package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama/v3/server/evr"

	"github.com/muesli/reflow/wordwrap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	HMDSerialOverrideUrlParam             = "hmdserial"
	DisplayNameOverrideUrlParam           = "displayname"
	UserPasswordUrlParam                  = "password"
	DiscordIdURLParam                     = "discordid"
	EvrIDOverrideUrlParam                 = "evrid"
	BroadcasterEncryptionDisabledUrlParam = "disable_encryption"
	BroadcasterHMACDisabledUrlParam       = "disable_hmac"
	FeaturesURLParam                      = "features"
	RequiredFeaturesURLParam              = "required_features"

	EvrIDStorageIndex            = "EvrIDs_Index"
	GameClientSettingsStorageKey = "clientSettings"
	GamePlayerSettingsStorageKey = "playerSettings"
	DocumentStorageCollection    = "GameDocuments"
	GameProfileStorageCollection = "GameProfiles"
	GameProfileStorageKey        = "gameProfile"
	RemoteLogStorageCollection   = "RemoteLogs"
)

// errWithEvrIDFn prefixes an error with the EchoVR Id.
func errWithEvrIDFn(evrID evr.EvrID, format string, a ...interface{}) error {
	return fmt.Errorf("%s: %w", evrID.String(), fmt.Errorf(format, a...))
}

// msgFailedLoginFn sends a LoginFailure message to the client.
// The error message is word-wrapped to 60 characters, 4 lines long.
func msgFailedLoginFn(session *sessionWS, evrID evr.EvrID, err error) error {
	// Format the error message
	s := fmt.Sprintf("%s: %s", evrID.String(), err.Error())

	// Replace ": " with ":\n" for better readability
	s = strings.Replace(s, ": ", ":\n", 2)

	// Word wrap the error message
	errMessage := wordwrap.String(s, 60)

	// Send the messages
	if err := session.SendEVR(
		evr.NewLoginFailure(evrID, errMessage),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		// If there's an error, prefix it with the EchoVR Id
		return errWithEvrIDFn(evrID, "send LoginFailure failed: %w", err)
	}

	return nil
}

// TODO FIXME This could use some optimization, or at least some benchmarking.
// Since all of these messages for the login step happen predictably, it might be worth preloading the user's profile.

// loginRequest handles the login request from the client.
func (p *EvrPipeline) loginRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.LoginRequest)

	// Start a timer to add to the metrics
	timer := time.Now()
	defer func() { p.metrics.CustomTimer("login", nil, time.Since(timer)) }()

	// Validate the user identifier
	if !request.EvrID.Valid() {
		return msgFailedLoginFn(session, request.EvrID, status.Error(codes.InvalidArgument, "invalid EVR ID"))
	}

	payload := request.LoginData

	// Check for an HMD serial override
	hmdsn, ok := ctx.Value(ctxHMDSerialOverrideKey{}).(string)
	if !ok {
		hmdsn = payload.HmdSerialNumber
	}

	// Construct the device auth token from the login payload
	deviceId := NewDeviceAuth(payload.AppId, request.EvrID, hmdsn, session.clientIP)

	// Authenticate the connection
	gameSettings, err := p.processLogin(ctx, logger, session, request.EvrID, deviceId, payload)
	if err != nil {
		st := status.Convert(err)
		return msgFailedLoginFn(session, request.EvrID, errors.New(st.Message()))
	}

	// Let the client know that the login was successful.
	// Send the login success message and the login settings.
	return session.SendEVR(
		evr.NewLoginSuccess(session.id, request.EvrID),
		evr.NewSTcpConnectionUnrequireEvent(),
		gameSettings,
	)
}

// processLogin handles the authentication of the login connection.
func (p *EvrPipeline) processLogin(ctx context.Context, logger *zap.Logger, session *sessionWS, evrID evr.EvrID, deviceId *DeviceAuth, loginProfile evr.LoginProfile) (settings *evr.GameSettings, err error) {

	var account *api.Account
	if session.userID == uuid.Nil {
		if account, err = p.authenticateAccount(ctx, logger, session, deviceId, loginProfile); err != nil {
			return settings, err
		}
	} else {
		if account, err = GetAccount(ctx, session.logger, session.pipeline.db, session.statusRegistry, session.userID); err != nil {
			return settings, status.Error(codes.Internal, fmt.Sprintf("failed to get account: %s", err))
		}
	}

	if account == nil {
		return settings, fmt.Errorf("account not found")
	}
	user := account.GetUser()
	userID := user.GetId()
	uid := uuid.FromStringOrNil(user.GetId())

	updateCtx, cancel := context.WithTimeout(ctx, time.Second*3)
	go func() {
		defer cancel()
		err = p.discordRegistry.UpdateAllGuildGroupsForUser(ctx, NewRuntimeGoLogger(logger), uid)
		if err != nil {
			logger.Warn("Failed to update guild groups", zap.Error(err))
		}
	}()

	// Wait for the context to be done, or the timeout
	<-updateCtx.Done()

	// Get the user's metadata
	var metadata AccountUserMetadata
	if err := json.Unmarshal([]byte(account.User.GetMetadata()), &metadata); err != nil {
		return settings, fmt.Errorf("failed to unmarshal account metadata: %w", err)
	}

	// Check that this EVR-ID is only used by this userID
	otherLogins, err := p.checkEvrIDOwner(ctx, evrID)
	if err != nil {
		return settings, fmt.Errorf("failed to check EVR-ID owner: %w", err)
	}

	if len(otherLogins) > 0 {
		// Check if the user is the owner of the EVR-ID
		if otherLogins[0].UserID != uuid.FromStringOrNil(userID) {
			session.logger.Warn("EVR-ID is already in use", zap.String("evrID", evrID.String()), zap.String("userId", userID))
		}
	}

	// If user ID is not empty, write out the login payload to storage.
	if userID != "" {
		if err := writeAuditObjects(ctx, session, userID, evrID.String(), loginProfile); err != nil {
			session.logger.Warn("Failed to write audit objects", zap.Error(err))
		}
	}

	flags := 0
	if loginProfile.SystemInfo.HeadsetType == "No VR" {
		flags |= FlagNoVR
	}

	for name, flag := range groupFlagMap {
		if ok, err := checkGroupMembershipByName(ctx, p.runtimeModule, uid.String(), name, SystemGroupLangTag); err != nil {
			return settings, fmt.Errorf("failed to check group membership: %w", err)
		} else if ok {
			flags |= flag
		}
	}

	config, err := LoadMatchmakingSettings(ctx, p.runtimeModule, userId)
	if err != nil {
		logger.Warn("Failed to load matchmaking config", zap.Error(err))
	}
	verbose := config.Verbose

	// Get the GroupID from the user's metadata
	groupID := metadata.GetActiveGroupID()
	// Validate that the user is in the group

	// Get a list of the user's guild memberships and set to the largest one
	memberships, err := p.discordRegistry.GetGuildGroupMemberships(ctx, uid, nil)
	if err != nil {
		return settings, fmt.Errorf("failed to get guild groups: %w", err)
	}
	if len(memberships) == 0 {
		return settings, fmt.Errorf("user is not in any guild groups")
	}

	// Sort the groups by the edgecount
	sort.SliceStable(memberships, func(i, j int) bool {
		if memberships[i].GuildGroup.ID() == groupID {
			return true
		}
		return memberships[i].GuildGroup.Size() > memberships[j].GuildGroup.Size()
	})

	groupID = memberships[0].GuildGroup.ID()

	verbose := metadata.Verbose
	// Initialize the full session
	if err := session.LoginSession(userID, user.GetUsername(), evrID, deviceId, memberships, flags, verbose); err != nil {
		return settings, fmt.Errorf("failed to login: %w", err)
	}
	ctx = session.Context()

	// Load the user's profile
	profile, err := p.profileRegistry.GetSessionProfile(ctx, session, loginProfile, evrID)
	if err != nil {
		session.logger.Error("failed to load game profiles", zap.Error(err))
		return evr.NewDefaultGameSettings(), fmt.Errorf("failed to load game profiles")
	}

	displayName, err := SetDisplayNameByChannelBySession(ctx, p.runtimeModule, logger, p.discordRegistry, session, groupID.String())
	if err != nil {
		return settings, fmt.Errorf("failed to set display name: %w", err)
	}
	// Get the display name currently set on the account.
	profile.SetChannel(evr.GUID(groupID))
	profile.UpdateDisplayName(displayName)
	p.profileRegistry.Store(session.userID, profile)

	// TODO Add the settings to the user profile
	settings = evr.NewDefaultGameSettings()
	return settings, nil

}

type EvrIDHistory struct {
	Created time.Time
	Updated time.Time
	UserID  uuid.UUID
}

func (p *EvrPipeline) checkEvrIDOwner(ctx context.Context, evrID evr.EvrID) ([]EvrIDHistory, error) {

	// Check the storage index for matching evrIDs
	objectIds, err := p.storageIndex.List(ctx, uuid.Nil, EvrIDStorageIndex, fmt.Sprintf("+value.server.xplatformid:%s", evrID.String()), 1)
	if err != nil {
		return nil, fmt.Errorf("failed to list evrIDs: %w", err)
	}

	history := make([]EvrIDHistory, len(objectIds.Objects))
	for i, obj := range objectIds.Objects {
		history[i] = EvrIDHistory{
			Updated: obj.GetUpdateTime().AsTime(),
			Created: obj.GetCreateTime().AsTime(),
			UserID:  uuid.FromStringOrNil(obj.UserId),
		}
	}

	// sort history by updated time descending
	sort.Slice(history, func(i, j int) bool {
		return history[i].Updated.After(history[j].Updated)
	})

	return history, nil
}
func (p *EvrPipeline) authenticateAccount(ctx context.Context, logger *zap.Logger, session *sessionWS, deviceId *DeviceAuth, payload evr.LoginProfile) (*api.Account, error) {
	var err error
	var userId string
	var account *api.Account

	// Check for a password in the context
	userPassword := ctx.Value(ctxAuthPasswordKey{}).(string)

	// Authenticate with the device ID.
	userId, _, _, err = AuthenticateDevice(ctx, session.logger, session.pipeline.db, deviceId.Token(), "", false)
	if err != nil && status.Code(err) == codes.NotFound {
		// Try to authenticate the device with a wildcard address.
		userId, _, _, err = AuthenticateDevice(ctx, session.logger, session.pipeline.db, deviceId.WildcardToken(), "", false)
	}
	if err != nil && status.Code(err) != codes.NotFound {
		// Possibly banned or other error.
		return account, err
	} else if err == nil {

		// The account was found.
		account, err = GetAccount(ctx, session.logger, session.pipeline.db, session.statusRegistry, uuid.FromStringOrNil(userId))
		if err != nil {
			return account, status.Error(codes.Internal, fmt.Sprintf("failed to get account: %s", err))
		}

		if account.GetCustomId() == "" {

			// Account requires discord linking.
			userId = ""

		} else if account.GetEmail() != "" {

			// The account has a password, authenticate the password.
			_, _, _, err := AuthenticateEmail(ctx, session.logger, session.pipeline.db, account.Email, userPassword, "", false)
			return account, err

		} else if userPassword != "" {

			// The user provided a password and the account has no password.
			err = LinkEmail(ctx, session.logger, session.pipeline.db, uuid.FromStringOrNil(userId), account.User.Id+"@"+p.placeholderEmail, userPassword)
			if err != nil {
				return account, status.Error(codes.Internal, fmt.Errorf("error linking email: %w", err).Error())
			}

			return account, nil
		} else if status.Code(err) != codes.NotFound {
			// Possibly banned or other error.
			return account, err
		}
	}

	// Account requires discord linking.
	linkTicket, err := p.linkTicket(session, logger, deviceId, &payload)
	if err != nil {
		return account, status.Error(codes.Internal, fmt.Errorf("error creating link ticket: %w", err).Error())
	}
	return account, fmt.Errorf("\nEnter this code:\n  \n>>> %s <<<\nusing '/link-headset %s' in the Echo VR Lounge Discord.", linkTicket.Code, linkTicket.Code)
}

func writeAuditObjects(ctx context.Context, session *sessionWS, userId string, evrIDToken string, payload evr.LoginProfile) error {
	// Write logging/auditing storage objects.
	loginPayloadJson, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshalling login payload: %w", err)
	}
	data := string(loginPayloadJson)
	perm := &wrapperspb.Int32Value{Value: int32(0)}
	ops := StorageOpWrites{
		{
			OwnerID: userId,
			Object: &api.WriteStorageObject{
				Collection:      EvrLoginStorageCollection,
				Key:             evrIDToken,
				Value:           data,
				PermissionRead:  perm,
				PermissionWrite: perm,
				Version:         "",
			},
		},
		{
			OwnerID: userId,
			Object: &api.WriteStorageObject{
				Collection:      ClientAddrStorageCollection,
				Key:             session.clientIP,
				Value:           string(loginPayloadJson),
				PermissionRead:  perm,
				PermissionWrite: perm,
				Version:         "",
			},
		},
	}
	_, _, err = StorageWriteObjects(ctx, session.logger, session.pipeline.db, session.metrics, session.storageIndex, true, ops)
	if err != nil {
		return fmt.Errorf("failed to write objects: %w", err)
	}
	return nil
}

func (p *EvrPipeline) channelInfoRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	_ = in.(*evr.ChannelInfoRequest)

	resource, err := p.buildChannelInfo(ctx, logger)
	if err != nil {
		logger.Warn("Error building channel info", zap.Error(err))
	}

	if resource == nil {
		resource = evr.NewChannelInfoResource()
	}

	// send the document to the client
	if err := session.SendEVR(
		evr.NewSNSChannelInfoResponse(resource),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return fmt.Errorf("failed to send ChannelInfoResponse: %w", err)
	}
	return nil
}

func (p *EvrPipeline) buildChannelInfo(ctx context.Context, logger *zap.Logger) (*evr.ChannelInfoResource, error) {
	resource := evr.NewChannelInfoResource()

	memberships, ok := ctx.Value(ctxMembershipsKey{}).([]GuildGroupMembership)
	if !ok {
		return nil, fmt.Errorf("guild memberships not found in context")
	}

	group, md, err := GetGuildGroupMetadata(ctx, p.runtimeModule, memberships[0].ID().String())
	if err != nil {
		return nil, fmt.Errorf("failed to get guild group metadata: %w", err)
	}

	resource.Groups = make([]evr.ChannelGroup, 4)
	for i := range resource.Groups {
		resource.Groups[i] = evr.ChannelGroup{
			ChannelUuid:  strings.ToUpper(group.GetId()),
			Name:         group.GetName(),
			Description:  group.GetDescription(),
			Rules:        group.GetName() + "\n" + md.RulesText,
			RulesVersion: 1,
			Link:         fmt.Sprintf("https://discord.gg/channel/%s", group.GetName()),
			Priority:     uint64(i),
			RAD:          true,
		}
	}

	return resource, nil
}

func (p *EvrPipeline) loggedInUserProfileRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) (err error) {
	request := in.(*evr.LoggedInUserProfileRequest)
	// Start a timer to add to the metrics
	timer := time.Now()
	defer func() { p.metrics.CustomTimer("loggedInUserProfileRequest", nil, time.Since(timer)) }()

	// Ignore the request and use what was authenticated with
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	if !ok {
		logger.Error("evrID not found in context")
		// Get it from the request
		evrID = request.EvrID
	}

	profile, found := p.profileRegistry.Load(session.userID, evrID)
	if !found {
		return session.SendEVR(evr.NewLoggedInUserProfileFailure(request.EvrID, 400, "failed to load game profiles"))
	}

	return session.SendEVR(evr.NewLoggedInUserProfileSuccess(evrID, profile.Client, profile.Server))
}

func (p *EvrPipeline) updateClientProfileRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.UpdateClientProfile)
	// Ignore the EvrID in the request and use what was authenticated with
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	if !ok {
		return fmt.Errorf("evrID not found in context")
	}
	// Set the EVR ID from the context
	request.ClientProfile.EvrID = evrID

	if _, err := p.profileRegistry.UpdateClientProfile(ctx, logger, session, request.ClientProfile); err != nil {
		code := 400
		if err := session.SendEVR(evr.NewUpdateProfileFailure(evrID, uint64(code), err.Error())); err != nil {
			return fmt.Errorf("send UpdateProfileFailure: %w", err)
		}
		return fmt.Errorf("UpdateProfile: %w", err)
	}

	// Send the profile update to the client
	if err := session.SendEVR(
		evr.NewSNSUpdateProfileSuccess(&evrID),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		logger.Warn("Failed to send UpdateProfileSuccess", zap.Error(err))
	}

	return nil
}

func (p *EvrPipeline) remoteLogSetv3(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.RemoteLogSet)

	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	if !ok {
		logger.Debug("evrID not found in context")

	}

	for _, l := range request.Logs {
		// Unmarshal the top-level to check the message type.

		entry := map[string]interface{}{}
		logBytes := []byte(l)
		if err := json.Unmarshal(logBytes, &entry); err != nil {
			if logger.Core().Enabled(zap.DebugLevel) {
				logger.Debug("Non-JSON log entry", zap.String("entry", string(logBytes)))
			}
		}

		s, ok := entry["message"]
		if !ok {
			logger.Warn("RemoteLogSet: missing message property", zap.Any("entry", entry))
		}

		messagetype, ok := s.(string)
		if !ok {
			logger.Debug("RemoteLogSet: message property is not a string", zap.Any("entry", entry))
		}

		switch strings.ToLower(messagetype) {
		case "ghost_user":
			// This is a ghost user message.
			ghostUser := &evr.RemoteLogGhostUser{}
			if err := json.Unmarshal(logBytes, ghostUser); err != nil {
				logger.Error("Failed to unmarshal ghost user", zap.Error(err))
				continue
			}
			_ = ghostUser
		case "game_settings":
			gameSettings := &evr.RemoteLogGameSettings{}
			if err := json.Unmarshal(logBytes, gameSettings); err != nil {
				logger.Error("Failed to unmarshal game settings", zap.Error(err))
				continue
			}

			// Store the game settings (this isn't really used)
			ops := StorageOpWrites{
				{
					OwnerID: session.userID.String(),
					Object: &api.WriteStorageObject{
						Collection:      RemoteLogStorageCollection,
						Key:             GamePlayerSettingsStorageKey,
						Value:           string(logBytes),
						PermissionRead:  &wrapperspb.Int32Value{Value: int32(1)},
						PermissionWrite: &wrapperspb.Int32Value{Value: int32(0)},
						Version:         "",
					},
				},
			}
			if _, _, err := StorageWriteObjects(ctx, logger, session.pipeline.db, session.metrics, session.storageIndex, true, ops); err != nil {
				logger.Error("Failed to write game settings", zap.Error(err))
				continue
			}

		case "session_started":
			// TODO let the match know the server loaded the session?
			sessionStarted := &evr.RemoteLogSessionStarted{}
			if err := json.Unmarshal(logBytes, sessionStarted); err != nil {
				logger.Error("Failed to unmarshal session started", zap.Error(err))
				continue
			}
		case "customization item preview":
			fallthrough
		case "customization item equip":
			fallthrough
		case "podium interaction":
			fallthrough
		case "interaction_event":
			// Avoid spamming the logs with interaction events.
			if !logger.Core().Enabled(zap.DebugLevel) {
				continue
			}
			event := &evr.RemoteLogInteractionEvent{}
			if err := json.Unmarshal(logBytes, &event); err != nil {
				logger.Error("Failed to unmarshal interaction event", zap.Error(err))
				continue
			}
		case "customization_metrics_payload":
			// Update the server profile with the equipped cosmetic item.
			c := &evr.RemoteLogCustomizationMetricsPayload{}
			if err := json.Unmarshal(logBytes, &c); err != nil {
				logger.Error("Failed to unmarshal customization metrics", zap.Error(err))
				continue
			}

			if c.EventType != "item_equipped" {
				continue
			}
			category, name, err := c.GetEquippedCustomization()
			if err != nil {
				logger.Error("Failed to get equipped customization", zap.Error(err))
				continue
			}
			if category == "" || name == "" {
				logger.Error("Equipped customization is empty")
			}
			profile, found := p.profileRegistry.Load(session.userID, evrID)
			if !found {
				return status.Errorf(codes.Internal, "Failed to get player's profile")
			}

			p.profileRegistry.UpdateEquippedItem(&profile, category, name)

			err = p.profileRegistry.Store(session.userID, profile)
			if err != nil {
				return status.Errorf(codes.Internal, "Failed to store player's profile")
			}

		default:
			if logger.Core().Enabled(zap.DebugLevel) {
				// Write the remoteLog to storage.
				ops := StorageOpWrites{
					{
						OwnerID: session.userID.String(),
						Object: &api.WriteStorageObject{
							Collection:      RemoteLogStorageCollection,
							Key:             messagetype,
							Value:           l,
							PermissionRead:  &wrapperspb.Int32Value{Value: int32(1)},
							PermissionWrite: &wrapperspb.Int32Value{Value: int32(0)},
							Version:         "",
						},
					},
				}
				_, _, err := StorageWriteObjects(ctx, logger, session.pipeline.db, session.metrics, session.storageIndex, true, ops)
				if err != nil {
					logger.Error("Failed to write remote log", zap.Error(err))
				}
			} else {
				//logger.Debug("Received unknown remote log", zap.Any("entry", entry))
			}
		}
	}

	return nil
}

func (p *EvrPipeline) documentRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.DocumentRequest)
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	if !ok {
		return fmt.Errorf("evrID not found in context")
	}

	var document evr.Document
	var err error
	switch request.Type {
	case "eula":

		if flags, ok := ctx.Value(ctxFlagsKey{}).(int); ok && flags&FlagNoVR != 0 {
			// Get the version of the EULA from the profile
			profile, found := p.profileRegistry.Load(session.userID, evrID)
			if !found {
				return fmt.Errorf("failed to load game profiles")
			}
			document = evr.NewEULADocument(int(profile.Client.LegalConsents.EulaVersion), int(profile.Client.LegalConsents.GameAdminVersion), request.Language, "https://github.com/EchoTools", "Blank EULA for NoVR clients.")

		} else {

			document, err = p.generateEULA(ctx, logger, request.Language)
			if err != nil {
				return fmt.Errorf("failed to get eula document: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown document: %s,%s", request.Language, request.Type)
	}

	session.SendEVR(
		evr.NewDocumentSuccess(document),
		evr.NewSTcpConnectionUnrequireEvent(),
	)
	return nil
}

func (p *EvrPipeline) generateEULA(ctx context.Context, logger *zap.Logger, language string) (evr.EULADocument, error) {
	// Retrieve the contents from storage
	key := fmt.Sprintf("eula,%s", language)
	document := evr.DefaultEULADocument(language)
	ts, err := p.StorageLoadOrStore(ctx, logger, uuid.Nil, DocumentStorageCollection, key, &document)
	if err != nil {
		return document, fmt.Errorf("failed to load or store EULA: %w", err)
	}

	msg := document.Text
	maxLineCount := 7
	maxLineLength := 28
	// Split the message by newlines

	// trim the final newline
	msg = strings.TrimRight(msg, "\n")

	// Limit the EULA to 7 lines, and add '...' to the end of any line that is too long.
	lines := strings.Split(msg, "\n")
	if len(lines) > maxLineCount {
		logger.Warn("EULA too long", zap.Int("lineCount", len(lines)))
		lines = lines[:maxLineCount]
		lines = append(lines, "...")
	}

	// Cut lines at 18 characters
	for i, line := range lines {
		if len(line) > maxLineLength {
			logger.Warn("EULA line too long", zap.String("line", line), zap.Int("length", len(line)))
			lines[i] = line[:maxLineLength-3] + "..."
		}
	}
	msg = strings.Join(lines, "\n") + "\n"

	document.Version = ts.Unix()
	document.VersionGameAdmin = ts.Unix()

	document.Text = msg
	return document, nil
}

// StorageLoadOrDefault loads an object from storage or store the given object if it doesn't exist.
func (p *EvrPipeline) StorageLoadOrStore(ctx context.Context, logger *zap.Logger, userID uuid.UUID, collection, key string, dst any) (time.Time, error) {
	ts := time.Now().UTC()
	objs, err := StorageReadObjects(ctx, logger, p.db, uuid.Nil, []*api.ReadStorageObjectId{
		{
			Collection: collection,
			Key:        key,
			UserId:     userID.String(),
		},
	})
	if err != nil {
		return ts, fmt.Errorf("SNSDocumentRequest: failed to read objects: %w", err)
	}

	if len(objs.Objects) > 0 {

		// unmarshal the document
		if err := json.Unmarshal([]byte(objs.Objects[0].Value), dst); err != nil {
			return ts, fmt.Errorf("error unmarshalling document %s: %w", key, err)
		}

		ts = objs.Objects[0].UpdateTime.AsTime()

	} else {

		// If the document doesn't exist, store the object
		jsonBytes, err := json.Marshal(dst)
		if err != nil {
			return ts, fmt.Errorf("error marshalling document: %w", err)
		}

		// write the document to storage
		ops := StorageOpWrites{
			{
				OwnerID: userID.String(),
				Object: &api.WriteStorageObject{
					Collection:      collection,
					Key:             key,
					Value:           string(jsonBytes),
					PermissionRead:  &wrapperspb.Int32Value{Value: int32(0)},
					PermissionWrite: &wrapperspb.Int32Value{Value: int32(0)},
				},
			},
		}
		if _, _, err = StorageWriteObjects(ctx, logger, p.db, p.metrics, p.storageIndex, false, ops); err != nil {
			return ts, fmt.Errorf("failed to write objects: %w", err)
		}
	}

	return ts, nil
}

func (p *EvrPipeline) genericMessage(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.GenericMessage)
	logger.Debug("Received generic message", zap.Any("message", request))

	/*

		msg := evr.NewGenericMessageNotify(request.MessageType, request.Session, request.RoomID, request.PartyData)

		if err := otherSession.SendEVR(msg); err != nil {
			return fmt.Errorf("failed to send generic message: %w", err)
		}

		if err := session.SendEVR(msg); err != nil {
			return fmt.Errorf("failed to send generic message success: %w", err)
		}

	*/
	return nil
}

func generateSuspensionNotice(statuses []*SuspensionStatus) string {
	msgs := []string{
		"Current Suspensions:",
	}
	for _, s := range statuses {
		// The user is suspended from this channel.
		// Get the message from the suspension
		msgs = append(msgs, s.GuildName)
	}
	// Ensure that every line is padded to 40 characters on the right.
	for i, m := range msgs {
		msgs[i] = fmt.Sprintf("%-40s", m)
	}
	msgs = append(msgs, "\n\nContact the Guild's moderators for more information.")
	return strings.Join(msgs, "\n")
}

func (p *EvrPipeline) userServerProfileUpdateRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.UserServerProfileUpdateRequest)

	defer func() {
		if err := session.SendEVR(evr.NewUserServerProfileUpdateSuccess(request.EvrID)); err != nil {
			logger.Warn("Failed to send UserServerProfileUpdateSuccess", zap.Error(err))
		}
	}()

	matchID, err := NewMatchID(uuid.UUID(request.Payload.SessionID), p.node)
	if err != nil {
		return fmt.Errorf("failed to generate matchID: %w", err)
	}

	// Verify the player is a member of this match
	label, err := MatchLabelByID(ctx, p.runtimeModule, matchID)
	if err != nil {
		return fmt.Errorf("failed to get match label: %w", err)
	}

	userID := uuid.Nil
	username := ""
	for _, p := range label.Players {
		if p.EvrID == request.EvrID {
			userID = uuid.FromStringOrNil(p.UserID)
			username = p.Username
		}
	}
	if userID == uuid.Nil {
		return fmt.Errorf("failed to find player in match")
	}

	profile, found := p.profileRegistry.Load(userID, request.EvrID)
	if !found {
		return fmt.Errorf("failed to load game profiles")
	}

	serverProfile := profile.GetServer()

	matchType := evr.ToSymbol(request.Payload.MatchType).Token().String()
	_ = matchType
	for groupName, stats := range request.Payload.Update.StatsGroups {

		for statName, stat := range stats {
			record, err := p.leaderboardRegistry.Submission(ctx, userID.String(), request.EvrID.String(), username, request.Payload.SessionID.String(), groupName, statName, stat.Operand, stat.Value)
			if err != nil {
				logger.Warn("Failed to submit leaderboard", zap.Error(err))
			}
			if record != nil {
				matchStat := evr.MatchStatistic{
					Operand: stat.Operand,
				}
				if stat.Count != nil {
					matchStat.Count = stat.Count
				}
				if stat.IsFloat64() {
					// Combine the record score and the subscore as the decimal value
					matchStat.Value = float64(record.Score) + float64(record.Subscore)/10000
				} else {
					matchStat.Value = record.Score
				}

				// Update the profile
				_, ok := serverProfile.Statistics[groupName]
				if !ok {
					serverProfile.Statistics[groupName] = make(map[string]evr.MatchStatistic)
				}
				serverProfile.Statistics[groupName][statName] = matchStat
			}
		}
	}
	profile.SetServer(serverProfile)
	// Store the profile
	if err := p.profileRegistry.Store(userID, profile); err != nil {
		logger.Warn("UserServerProfileUpdateRequest: failed to store game profiles", zap.Error(err))
	}
	p.profileRegistry.Save(userID)

	return nil
}

func updateStats(profile *GameProfileData, stats evr.StatsUpdate) {
	serverProfile := profile.GetServer()

	for groupName, stats := range stats.StatsGroups {

		for statName, stat := range stats {

			matchStat := evr.MatchStatistic{
				Operand: stat.Operand,
			}
			if stat.Count != nil {
				matchStat.Count = stat.Count
				matchStat.Value = stat.Value
			}

			_, ok := serverProfile.Statistics[groupName]
			if !ok {
				serverProfile.Statistics[groupName] = make(map[string]evr.MatchStatistic)
			}
			serverProfile.Statistics[groupName][statName] = matchStat
		}
	}

	profile.SetServer(serverProfile)
}

func (p *EvrPipeline) otherUserProfileRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.OtherUserProfileRequest)

	// get this users match
	stream := PresenceStream{
		Mode:    StreamModeService,
		Subject: session.id,
		Label:   StreamLabelMatchService,
	}
	// Get the target user's match
	presences := session.tracker.ListByStream(stream, true, true)

	var matchID MatchID
	for _, presence := range presences {
		matchID = MatchIDFromStringOrNil(presence.GetStatus())
		if matchID.IsNil() {
			continue
		}
		break
	}

	var data string
	var found bool

	// This is probably a broadcaster connection. pull any profile for this EvrID from the cache.
	data, found = p.profileCache.GetByEvrID(request.EvrID)
	if !found {
		return fmt.Errorf("failed to find profile for `%s` in match `%s`", request.EvrID.String(), matchID.String())
	}

	// Construct the response
	response := &evr.OtherUserProfileSuccess{
		EvrID:             request.EvrID,
		ServerProfileJSON: []byte(data),
	}

	// Send the profile to the client
	if err := session.SendEVR(response); err != nil {
		return fmt.Errorf("failed to send OtherUserProfileSuccess: %w", err)
	}
	return nil
}

/*
func NewGroupMemberships() {

	// Synchronize the user's groups with the Discord Guilds/Roles
	memberships, err := p.discordRegistry.UpdateGuildGroupsForUser(ctx, userUUID, p.discordRegistry.GetBot().State.Guilds)
	if err != nil {
		logger.Warn("Failed to update guild groups", zap.Error(err))
	}
	if len(memberships) == 0 {
		return settings, fmt.Errorf("user is not in any guilds")
	}

	// Set channel selections based on the matchmaking URL param (if given)
	matchmakingChannels := make([]uuid.UUID, 0)
	for _, gid := range ctx.Value(ctxDiscordGuildIDsKey{}).([]string) {
		groupID, found := p.discordRegistry.Get(gid)
		if !found {
			continue
		}
		// Verify that this group is in the user's memberships
		if !slices.ContainsFunc(memberships, func(m GuildGroupMembership) bool {
			return m.ChannelID == uuid.FromStringOrNil(groupID)
		}) {
			continue
		}
		matchmakingChannels = append(matchmakingChannels, uuid.FromStringOrNil(groupID))
	}

	if len(matchmakingChannels) == 0 {
		// If no channels were found, use the user's memberships
		for _, m := range memberships {
			matchmakingChannels = append(matchmakingChannels, m.ChannelID)
		}
	}

	// Sort the channels by the matchmakingChannels order
	slices.SortStableFunc(memberships, func(a, b ChannelMember) int {
		// Put suspended channels at the bottom
		if a.isSuspended && !b.isSuspended {
			return 1
		}
		if !a.isSuspended && b.isSuspended {
			return -1
		}

		for _, c := range matchmakingChannels {
			if a.ChannelID == c {
				return -1
			}

			if b.ChannelID == c {
				return 1
			}
		}
		return 0
	})

	// Disable matchmaking for any channel that is not in the matchmakingChannels list
	// Or where the user is suspended.
	for i, m := range memberships {
		if !slices.Contains(matchmakingChannels, m.ChannelID) || m.isSuspended {
			memberships[i].ExcludeFromMatchmaking = true
		}
	}

	// Ensure that the user has at least one channel
	if len(memberships) == 0 {
		return settings, fmt.Errorf("user is not in any guilds")
	}
}

*/
