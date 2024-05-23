package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"

	"github.com/muesli/reflow/wordwrap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	HMDSerialOverrideURLParam   = "hmdserial"
	DisplayNameOverrideURLParam = "displayname"
	UserPasswordURLParam        = "password"
	DiscordIDURLParam           = "discordid"
	EvrIdOverrideURLParam       = "evrid"
	FlagsURLParam               = "flags"
	GuildsURLParam              = "guilds"
	TagsURLParam                = "tags"
	FeaturesURLParam            = "features"
	RequiredFeaturesURLParam    = "required_features"

	EvrIDStorageIndex                    = "EvrIDs_Index"
	GameClientSettingsStorageKey         = "clientSettings"
	GamePlayerSettingsStorageKey         = "playerSettings"
	DocumentStorageCollection            = "GameDocuments"
	GameProfileStorageCollection         = "GameProfiles"
	SessionStatisticsStorageCollection   = "SessionStatistics"
	GameProfileStorageKey                = "gameProfile"
	ServerProfileUpdateStorageCollection = "ServerProfileUpdates"
	GameStatisticsStorageCollection      = "GameStatistics"

	RemoteLogStorageCollection = "RemoteLogs"
)

// msgFailedLoginFn sends a LoginFailure message to the client.
// The error message is word-wrapped to 60 characters, 4 lines long.
func msgFailedLoginFn(session *sessionWS, evrId evr.EvrId, err error) error {
	// Format the error message
	s := fmt.Sprintf("%s: %s", evrId.Token(), err.Error())

	// Replace ": " with ":\n" for better readability
	s = strings.Replace(s, ": ", ":\n", 2)

	// Word wrap the error message
	errMessage := wordwrap.String(s, 60)

	// Send the messages
	if err := session.SendEvr(
		evr.NewLoginFailure(evrId, errMessage),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		// If there's an error, prefix it with the EchoVR Id
		return fmt.Errorf("%s: %w", evrId.Token(), fmt.Errorf("send LoginFailure failed: %w", err))
	}

	return nil
}

func checkGroupMembershipByName(ctx context.Context, nk runtime.NakamaModule, userID uuid.UUID, groupName, langtag string) (bool, error) {

	groups, _, err := nk.UserGroupsList(ctx, userID.String(), 100, nil, "")
	if err != nil {
		return false, fmt.Errorf("error getting user groups: %w", err)
	}
	for _, g := range groups {
		if g.Group.LangTag != langtag && g.Group.Name == groupName {
			return true, nil
		}
	}
	return false, nil
}

// loginRequest handles the login request from the client.
func (p *EvrPipeline) loginRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(evr.LoginRequest)

	logger = logger.With(zap.String("evr_id", request.GetEvrID().Token()))

	// Start a timer to add to the metrics
	timer := time.Now()
	defer func() { p.metrics.CustomTimer("login", nil, time.Since(timer)) }()

	// Validate the user identifier
	if !request.GetEvrID().Valid() {
		return msgFailedLoginFn(session, request.GetEvrID(), status.Error(codes.InvalidArgument, "invalid EVR ID"))
	}

	payload := request.GetLoginProfile()

	// Construct the device auth token from the login payload
	deviceId := DeviceId{
		AppId:           payload.GetAppID(),
		EvrId:           request.GetEvrID(),
		HmdSerialNumber: payload.GetHMDSerialNumber(),
	}

	// Providing a discord ID and password avoids the need to link the device to the account.
	// Server Hosts use this method to authenticate.

	// Get the email and password from the basic auth.
	discordAuthID, userPassword, ok := parseBasicAuth(ctx.Value(ctxBasicAuthKey{}).(string))
	if !ok {
		return ErrNoAuthProvided
	}

	// Authenticate the connection
	loginSettings, err := p.processLogin(ctx, logger, session, request.GetEvrID(), deviceId, discordAuthID, userPassword, payload)
	if err != nil {
		st := status.Convert(err)
		return msgFailedLoginFn(session, request.GetEvrID(), errors.New(st.Message()))
	}

	// Let the client know that the login was successful.
	// Send the login success message and the login settings.
	return session.SendEvr(
		evr.NewLoginSuccess(session.id, request.GetEvrID()),
		evr.NewSTcpConnectionUnrequireEvent(),
		evr.NewSNSLoginSettings(loginSettings),
	)
}

// processLogin handles the authentication of the login connection.
func (p *EvrPipeline) processLogin(ctx context.Context, logger *zap.Logger, session *sessionWS, evrID evr.EvrId, deviceId DeviceId, discordId string, userPassword string, loginProfile evr.LoginProfile) (settings evr.GameClientSettings, err error) {
	settings = evr.DefaultGameClientSettings

	// Authenticate the account.
	account, err := p.authenticateAccount(ctx, logger, session, deviceId, discordId, userPassword, loginProfile)
	account, err := p.authenticateAccount(ctx, logger, session, deviceId, discordId, userPassword, loginProfile)
	if err != nil {
		return settings, err
	}
	user := account.GetUser()
	userID := user.GetId()

	logger = logger.With(zap.String("uid", userID), zap.String("username", user.GetUsername()))

	// Check that this EVR-ID is only used by this userID
	otherLogins, err := p.checkEvrIDOwner(ctx, evrID)
	if err != nil {
		return settings, fmt.Errorf("failed to check EVR-ID owner: %w", err)
	}

	if len(otherLogins) > 0 {
		ownerID := otherLogins[0].UserID.String()
		// Check if the user is the owner of the EVR-ID
		if otherLogins[0].UserID.String() != userID {
			logger.Warn("EVR-ID is already in use", zap.String("owner_uid", ownerID))
		}
	}

	// If user ID is not empty, write out the login payload to storage.
	if userID != "" {
		if err := writeAuditObjects(ctx, session, userID, evrID.Token(), loginProfile); err != nil {
			logger.Warn("Failed to write audit objects", zap.Error(err))
		}
	}

	// Check the user's group memberships and update the session flags
	flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags)
	if !ok {
		flags = SessionFlags{}
	}
	flags.IsNoVR = loginProfile.GetHeadsetType() == "No VR"

	groupFlags := map[string]*bool{
		GroupGlobalDevelopers: &flags.IsDeveloper,
		GroupGlobalModerators: &flags.IsModerator,
		GroupGlobalTesters:    &flags.IsTester,
		GroupGlobalBots:       &flags.MultiSession,
	}

	uid := uuid.FromStringOrNil(userID)

	for name, flag := range groupFlags {
		if ok, err := checkGroupMembershipByName(ctx, p.runtimeModule, uid, name, "system"); err != nil {
			return settings, fmt.Errorf("failed to check group membership: %w", err)
		} else if ok {
			*flag = true
		}
	}

	// Get the GroupID from the user's metadata
	groupID := metadata.GetActiveGroupID()
	// Validate that the user is in the group
	if groupID == uuid.Nil {
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
			return memberships[i].GuildGroup.Size() > memberships[j].GuildGroup.Size()
		})
		groupID = memberships[0].GuildGroup.ID()
	}

	// Parse the URL params to "turn off" any unwanted group effects
	params := ctx.Value(ctxURLParamsKey{}).(map[string][]string)
	flags.Parse(params)

	userUUID := uuid.FromStringOrNil(userID)
	// Update the account data to match discord
	if err := p.discordRegistry.SyncronizeDiscordToNakamaAccount(ctx, userUUID); err != nil {
		logger.Warn("Failed to synchronize account with Discord", zap.Error(err))
	}

	// Reload the account to get the updated data
	account, err = GetAccount(ctx, logger, session.pipeline.db, session.statusRegistry, userUUID)
	if err != nil {
		return settings, fmt.Errorf("failed to get account: %w", err)
	}

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
		if !slices.ContainsFunc(memberships, func(m ChannelMember) bool {
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

	// If there are zero non-suspended channels, then this user can only join privates (but not create them).

	lobbyVersion := strconv.FormatUint(loginProfile.GetLobbyVersion(), 16)

	// Initialize the full session
	ctx, err = session.LoginSession(userID, account.GetUser().GetUsername(), evrID, deviceId, groupID, flags, lobbyVersion, memberships)
	if err != nil {
		return settings, fmt.Errorf("failed to login: %w", err)
	}

	// Create a goroutine to clear the session info when the login session is closed.
	go func() {
		p.loginSessionByEvrID.Store(evrID.Token(), session)
		<-session.Context().Done()
		p.loginSessionByEvrID.Delete(evrId.String())
	}()

	// (Pre-)load the user's profile
	profile, err := p.profileRegistry.GetSessionProfile(ctx, session, loginProfile, evrID, memberships)
	if err != nil {
		session.logger.Error("failed to load game profiles", zap.Error(err))
		return evr.DefaultGameClientSettings, fmt.Errorf("failed to load game profiles")
	}

	// Enable extra logging for NoVR users (broadcasters, etc.)
	if flags.IsNoVR {
		settings.RemoteLogWarnings = false
		settings.RemoteLogErrors = false
		settings.RemoteLogRichPresence = false
		settings.RemoteLogSocial = false
		settings.RemoteLogMetrics = true
	}

	return settings, nil

}

type EvrIDHistory struct {
	Created time.Time
	Updated time.Time
	UserID  uuid.UUID
}

func (p *EvrPipeline) checkEvrIDOwner(ctx context.Context, evrId evr.EvrId) ([]EvrIDHistory, error) {

	// Check the storage index for matching evrIDs
	objectIds, err := p.storageIndex.List(ctx, uuid.Nil, EvrIDStorageIndex, fmt.Sprintf("+value.server.xplatformid:%s", evrId.String()), 1)
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
func (p *EvrPipeline) authenticateAccount(ctx context.Context, logger *zap.Logger, session *sessionWS, deviceId DeviceId, discordId string, userPassword string, payload evr.LoginProfile) (*api.Account, error) {
	var err error
	var userId string
	var account *api.Account

	// Discord Authentication
	if discordId != "" {

		logger = logger.With(zap.String("discord_id", discordId))

		if userPassword == "" {
			return nil, status.Error(codes.InvalidArgument, "password required")
		}

		uid, err := p.discordRegistry.GetUserIdByDiscordId(ctx, discordId, false)
		if err == nil {
			userId = uid.String()
			// Authenticate the password.
			userId, _, _, err = AuthenticateEmail(ctx, logger, session.pipeline.db, userId+"@"+p.placeholderEmail, userPassword, "", false)
			if err == nil {
				// Complete. Return account.
				return GetAccount(ctx, logger, session.pipeline.db, session.statusRegistry, uuid.FromStringOrNil(userId))
			} else if status.Code(err) != codes.NotFound {
				// Possibly banned or other error.
				return account, err
			}
		}
		// TODO FIXME return early if the discordId is non-existant.
		// Account requires discord linking, clear the discordId.
		discordId = ""
	}

	// Device Authentication
	logger = logger.With(zap.String("device_id", deviceId.Token()))
	userId, _, _, err = AuthenticateDevice(ctx, logger, session.pipeline.db, deviceId.Token(), "", false)
	if err != nil && status.Code(err) != codes.NotFound {
		// Possibly banned or other error.
		return account, err
	} else if err == nil {

		// The account was found.
		account, err = GetAccount(ctx, logger, session.pipeline.db, session.statusRegistry, uuid.FromStringOrNil(userId))
		if err != nil {
			return account, status.Error(codes.Internal, fmt.Sprintf("failed to get account: %s", err))
		}

		if account.GetCustomId() == "" {

			// Account requires discord linking.
			userId = ""

		} else if account.GetEmail() != "" {
			// The account has a password, authenticate the password.
			_, _, _, err := AuthenticateEmail(ctx, logger, session.pipeline.db, account.Email, userPassword, "", false)
			return account, err

		} else if userPassword != "" {

			// The user provided a password and the account has no password.
			err = LinkEmail(ctx, logger, session.pipeline.db, uuid.FromStringOrNil(userId), account.User.Id+"@"+p.placeholderEmail, userPassword)
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
	linkTicket, err := p.linkTicket(session, deviceId, payload)
	if err != nil {
		return account, status.Error(codes.Internal, fmt.Errorf("error creating link ticket: %w", err).Error())
	}
	msg := fmt.Sprintf("\nEnter this code:\n  \n>>> %s <<<\nusing '/link-headset %s' in the Echo VR Lounge Discord.", linkTicket.Code, linkTicket.Code)
	return account, errors.New(msg)
}

func writeAuditObjects(ctx context.Context, session *sessionWS, userId string, evrIdToken string, payload evr.LoginProfile) error {
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
				Key:             evrIdToken,
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
	if err := session.SendEvr(
		evr.NewSNSChannelInfoResponse(resource),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return fmt.Errorf("failed to send ChannelInfoResponse: %w", err)
	}
	return nil
}

func (p *EvrPipeline) buildChannelInfo(ctx context.Context, logger *zap.Logger) (*evr.ChannelInfoResource, error) {
	resource := evr.NewChannelInfoResource()

	// Get the channels from the context
	channels, ok := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	if !ok {
		return nil, fmt.Errorf("channels not found in context")
	}

	if len(channels) == 0 {
		// TODO FIXME Handle a user that doesn't have access to any guild groups
		return nil, fmt.Errorf("user is not in any guild groups")
	}

	// Limit to 4 results
	if len(channels) > 4 {
		channels = channels[:4]
	}

	// Overwrite the existing channel info
	for i, g := range channels {
		// Get the group metadata
		group, md, err := p.discordRegistry.GetGuildGroupMetadata(ctx, g.ChannelID.String())
		if err != nil {
			return nil, fmt.Errorf("error getting guild group metadata: %w", err)
		}

		resource.Groups[i] = evr.ChannelGroup{
			ChannelUuid:  strings.ToUpper(g.ChannelID.String()),
			Name:         group.GetName(),
			Description:  group.GetDescription(),
			Rules:        group.GetName() + "\n" + md.RulesText,
			RulesVersion: 1,
			Link:         fmt.Sprintf("https://discord.gg/channel/%s", md.GuildID),
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
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		logger.Error("evrId not found in context")
		// Get it from the request
		evrID = request.EvrId
	}

	profile, found := p.profileRegistry.Load(session.userID, evrID)
	if !found {
		return session.SendEvr(evr.NewLoggedInUserProfileFailure(request.EvrId, 400, "failed to load game profiles"))
	}

	return session.SendEvr(evr.NewLoggedInUserProfileSuccess(evrID, profile.Client, profile.Server))
}

// updateClientProfileRequest handles the update client profile request from the client.
func (p *EvrPipeline) updateClientProfileRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.UpdateClientProfile)

	// Ignore the EvrID in the request and use what was authenticated with
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return fmt.Errorf("evrId not found in context")
	}
	// Warn if there is a mismatch
	if evrID != request.EvrID {
		logger.Warn("EvrID mismatch", zap.String("evrId", evrID.Token()), zap.String("requestEvrId", request.EvrID.Token()))
	}

	// Set the EVR ID from the context
	request.Profile.EvrID = evrID

	if _, err := p.profileRegistry.UpdateClientProfile(ctx, logger, session, request.Profile); err != nil {

		if err := session.SendEvr(evr.NewUpdateProfileFailure(evrID, 400, err.Error())); err != nil {
			return fmt.Errorf("send UpdateProfileFailure: %w", err)
		}
		return fmt.Errorf("UpdateProfile: %w", err)
	}

	// Send the profile update to the client
	if err := session.SendEvr(
		evr.NewSNSUpdateProfileSuccess(&evrID),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		logger.Warn("Failed to send UpdateProfileSuccess", zap.Error(err))
	}

	return nil
}

func (p *EvrPipeline) remoteLogSetv3(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.RemoteLogSet)

	if session.userID == uuid.Nil {
		return fmt.Errorf("session is not authenticated")
	}

	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		logger.Debug("evrId not found in context")
	}

	for _, logMessage := range request.Logs {
		// Unmarshal the top-level to check the message type.

		entry := map[string]interface{}{}
		logBytes := []byte(logMessage)
		if err := json.Unmarshal(logBytes, &entry); err != nil {
			if logger.Core().Enabled(zap.DebugLevel) {
				entry["message"] = "string"
				entry["data"] = logMessage
			}
		}

		s, ok := entry["message"]
		if !ok {
			logger.Warn("RemoteLogSet: missing message property", zap.Any("entry", entry))
		}

		messagetype, ok := s.(string)
		if !ok {
			logger.Debug("RemoteLogSet: message property is not a string", zap.Any("entry", entry))
			continue
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
				data, err := json.Marshal(entry)
				if err != nil {
					logger.Error("Failed to marshal remote log", zap.Error(err))
					continue
				}

				ops := StorageOpWrites{
					{
						OwnerID: session.userID.String(),
						Object: &api.WriteStorageObject{
							Collection:      RemoteLogStorageCollection,
							Key:             messagetype,
							Value:           string(data),
							PermissionRead:  &wrapperspb.Int32Value{Value: int32(1)},
							PermissionWrite: &wrapperspb.Int32Value{Value: int32(0)},
							Version:         "",
						},
					},
				}
				_, _, err = StorageWriteObjects(ctx, logger, session.pipeline.db, session.metrics, session.storageIndex, true, ops)
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
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return fmt.Errorf("evrId not found in context")
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

	session.SendEvr(
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

	// Find online user with EvrId of request.OtherEvrId
	otherSession, found := p.loginSessionByEvrID.Load(request.OtherEvrID.Token())
	if !found {
		return fmt.Errorf("failure to find user by EvrID: %s", request.OtherEvrID.Token())
	}

	msg := evr.NewGenericMessageNotify(request.MessageType, request.Session, request.RoomID, request.PartyData)

	if err := otherSession.SendEvr(msg); err != nil {
		return fmt.Errorf("failed to send generic message: %w", err)
	}

	if err := session.SendEvr(msg); err != nil {
		return fmt.Errorf("failed to send generic message success: %w", err)
	}

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
	// Always send the same response

	if session.userID == uuid.Nil {
		logger.Warn("UserServerProfileUpdateRequest: user not logged in")
		return nil
	}

	// Always send the success message.
	defer func() {
		if err := session.SendEvr(evr.NewUserServerProfileUpdateSuccess(request.EvrID)); err != nil {
			logger.Warn("Failed to send UserServerProfileUpdateSuccess", zap.Error(err))
		}
	}()
	// Get the target user's match
	matchID, ok := p.matchByEvrID.Load(request.EvrID.String())
	if !ok {
		logger.Warn("UserServerProfileUpdateRequest: user not in a match")
		return nil
	}
	logger = logger.With(zap.String("matchID", matchID.String()))

	// Check if the requester is a broadcaster in that match
	_, _, statejson, err := p.matchRegistry.GetState(ctx, matchID.uuid, matchID.node)
	if err != nil {
		logger.Warn("UserServerProfileUpdateRequest: failed to get match", zap.Error(err))
		return nil
	}

	// Check the label for the broadcaster
	state := &MatchLabel{}
	if err := json.Unmarshal([]byte(statejson), state); err != nil {
		logger.Warn("UserServerProfileUpdateRequest: failed to unmarshal match state", zap.Error(err))
		return nil
	}

	if state.Broadcaster.UserID != session.userID {
		logger.Warn("UserServerProfileUpdateRequest: user not broadcaster for match")
		return nil
	}

	// Get the user id for the target
	targetSession, found := p.loginSessionByEvrID.Load(request.EvrID.String())
	if !found {
		logger.Warn("UserServerProfileUpdateRequest: user not found", zap.String("evrId", request.EvrID.Token()))
		return nil
	}
	userID := targetSession.userID

	// Get the profile
	profile, found := p.profileRegistry.Load(userID, request.EvrID)
	if !found {
		return fmt.Errorf("failed to load game profiles")
	}

	update := request.Payload

	// Get the user's profile
	profile, found = p.profileRegistry.Load(userID, request.EvrID)
	if !found {
		return fmt.Errorf("failed to load game profiles")
	}

	if err := profile.UpdateStats(update); err != nil {
		return fmt.Errorf("failed to update server profile: %w", err)
	}
	return nil
}

func (p *EvrPipeline) otherUserProfileRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.OtherUserProfileRequest)

	// Lookup the the profile
	data, found := p.profileRegistry.GetServerProfileByEvrID(request.EvrId)
	if !found {
		return fmt.Errorf("failed to find profile for %s", request.EvrId.Token())
	}

	// Construct the response
	response := &evr.OtherUserProfileSuccess{
		EvrId:             request.EvrId,
		ServerProfileJSON: data,
	}

	// Send the profile to the client
	if err := session.SendEvr(response); err != nil {
		return fmt.Errorf("failed to send OtherUserProfileSuccess: %w", err)
	}
	return nil
}
