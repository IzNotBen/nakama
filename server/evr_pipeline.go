package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/heroiclabs/nakama/v3/social"

	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

var GlobalConfig = &struct {
	sync.RWMutex
	RejectMatchmaking bool
}{
	RejectMatchmaking: true,
}

type EvrPipeline struct {
	sync.RWMutex
	ctx context.Context

	node              string
	broadcasterUserID string // The userID used for broadcaster connections
	externalIP        net.IP // Server's external IP for external connections
	localIP           net.IP // Server's local IP for external connections

	logger               *zap.Logger
	db                   *sql.DB
	config               Config
	version              string
	socialClient         *social.Client
	storageIndex         StorageIndex
	leaderboardCache     LeaderboardCache
	leaderboardRankCache LeaderboardRankCache
	sessionCache         SessionCache
	apiServer            *ApiServer
	sessionRegistry      SessionRegistry
	statusRegistry       StatusRegistry
	matchRegistry        MatchRegistry
	tracker              Tracker
	router               MessageRouter
	streamManager        StreamManager
	metrics              Metrics
	runtime              *Runtime
	runtimeModule        *RuntimeGoNakamaModule
	runtimeLogger        runtime.Logger

	matchmakingRegistry *MatchmakingRegistry
	profileRegistry     *ProfileRegistry
	discordRegistry     DiscordRegistry
	broadcasterRegistry *BroadcasterRegistry
	appBot              *DiscordAppBot

	matchBySessionID    *MapOf[string, MatchID] // sessionID -> matchID
	loginSessionByEvrID *MapOf[string, *sessionWS]
	matchByEvrID        *MapOf[string, MatchID]     // full match string by evrId token
	backfillQueue       *MapOf[string, *sync.Mutex] // A queue of backfills to avoid double backfill
	placeholderEmail    string
	linkDeviceURL       string
}

type ctxDiscordBotTokenKey struct{}

func NewEvrPipeline(logger *zap.Logger, startupLogger *zap.Logger, db *sql.DB, protojsonMarshaler *protojson.MarshalOptions, protojsonUnmarshaler *protojson.UnmarshalOptions, config Config, version string, socialClient *social.Client, storageIndex StorageIndex, leaderboardScheduler LeaderboardScheduler, leaderboardCache LeaderboardCache, leaderboardRankCache LeaderboardRankCache, sessionRegistry SessionRegistry, sessionCache SessionCache, statusRegistry StatusRegistry, matchRegistry MatchRegistry, matchmaker Matchmaker, tracker Tracker, router MessageRouter, streamManager StreamManager, metrics Metrics, pipeline *Pipeline, runtime *Runtime) *EvrPipeline {
	// The Evr pipeline is going to be a bit "different".
	// It's going to get access to most components, because
	// of the way EVR works, it's going to need to be able
	// to access the API server, and the runtime.
	// TODO find a cleaner way to do this

	// Add the bot token to the context

	vars := config.GetRuntime().Environment

	ctx := context.WithValue(context.Background(), ctxDiscordBotTokenKey{}, vars["DISCORD_BOT_TOKEN"])
	ctx = context.WithValue(ctx, ctxNodeKey{}, config.GetName())
	nk := NewRuntimeGoNakamaModule(logger, db, protojsonMarshaler, config, socialClient, leaderboardCache, leaderboardRankCache, leaderboardScheduler, sessionRegistry, sessionCache, statusRegistry, matchRegistry, tracker, metrics, streamManager, router, storageIndex)

	// TODO Add a symbol cache that gets populated and stored back occasionally

	runtimeLogger := NewRuntimeGoLogger(logger)

	botToken, ok := ctx.Value(ctxDiscordBotTokenKey{}).(string)
	if !ok {
		panic("Bot token is not set in context.")
	}

	var dg *discordgo.Session
	var err error
	if botToken != "" {
		dg, err = discordgo.New("Bot " + botToken)
		if err != nil {
			logger.Error("Unable to create bot")
		}
		dg.StateEnabled = true
	}

	logChannelID := vars["DISCORD_LOG_CHANNEL_ID"]

	vrmlBadgeLogChannelID := vars["VRML_BADGE_LOG_CHANNEL_ID"]

	discordRegistry := NewLocalDiscordRegistry(ctx, nk, runtimeLogger, metrics, config, pipeline, dg, logChannelID)
	profileRegistry := NewProfileRegistry(nk, db, runtimeLogger, discordRegistry)

	appBot := NewDiscordAppBot(nk, runtimeLogger, metrics, pipeline, config, discordRegistry, profileRegistry, dg, vrmlBadgeLogChannelID)

	if disable, ok := vars["DISABLE_DISCORD_RESPONSE_HANDLERS"]; ok && disable == "true" {
		logger.Info("Discord bot is disabled")
	} else {
		if err := appBot.InitializeDiscordBot(); err != nil {
			logger.Error("Failed to initialize app bot", zap.Error(err))
		}
	}

	if err = appBot.dg.Open(); err != nil {
		logger.Error("Failed to open discord bot connection: %w", zap.Error(err))
	}

	// Every 5 minutes, store the cache.

	localIP, err := DetermineLocalIPAddress()
	if err != nil {
		logger.Fatal("Failed to determine local IP address", zap.Error(err))
	}

	// loop until teh external IP is set
	externalIP, err := DetermineExternalIPAddress()
	if err != nil {
		logger.Fatal("Failed to determine external IP address", zap.Error(err))
	}

	broadcasterUserID, _, _, err := nk.AuthenticateCustom(ctx, "000000000000000000", "broadcasthost", true)
	if err != nil {
		logger.Fatal("Failed to authenticate broadcaster", zap.Error(err))
	}
	broadcasterRegistry := NewBroadcasterRegistry(logger, metrics)

	evrPipeline := &EvrPipeline{
		ctx:                  ctx,
		node:                 config.GetName(),
		logger:               logger,
		db:                   db,
		config:               config,
		version:              version,
		socialClient:         socialClient,
		leaderboardCache:     leaderboardCache,
		leaderboardRankCache: leaderboardRankCache,
		storageIndex:         storageIndex,
		sessionCache:         sessionCache,
		sessionRegistry:      sessionRegistry,
		statusRegistry:       statusRegistry,
		matchRegistry:        matchRegistry,
		tracker:              tracker,
		router:               router,
		streamManager:        streamManager,
		metrics:              metrics,
		runtime:              runtime,
		runtimeModule:        nk,
		runtimeLogger:        runtimeLogger,

		discordRegistry:   discordRegistry,
		appBot:            appBot,
		localIP:           localIP,
		externalIP:        externalIP,
		broadcasterUserID: broadcasterUserID,

		profileRegistry: profileRegistry,

		broadcasterRegistry: broadcasterRegistry,
		matchBySessionID:    &MapOf[string, MatchID]{},
		loginSessionByEvrID: &MapOf[string, *sessionWS]{},
		matchByEvrID:        &MapOf[string, MatchID]{},
		backfillQueue:       &MapOf[string, *sync.Mutex]{},

		placeholderEmail: config.GetRuntime().Environment["PLACEHOLDER_EMAIL_DOMAIN"],
		linkDeviceURL:    config.GetRuntime().Environment["LINK_DEVICE_URL"],
	}

	go func() {
		err = clearLinkTickets(ctx, logger, nk)
		if err != nil {
			logger.Error("Failed to clear link tickets", zap.Error(err))
		}
	}()

	// Create a timer to periodically clear the queues and caches
	go func() {
		interval := 3 * time.Minute
		ticker := time.NewTicker(interval)
		for {
			select {
			case <-evrPipeline.ctx.Done():
				ticker.Stop()
				return

			case <-ticker.C:
				evrPipeline.expireCaches(evrPipeline.ctx)
			}
		}
	}()

	return evrPipeline
}

func (p *EvrPipeline) expireCaches(ctx context.Context) {
	p.backfillQueue.Range(func(key string, value *sync.Mutex) bool {
		if value.TryLock() {
			p.backfillQueue.Delete(key)
			value.Unlock()
		}
		return true
	})

	p.loginSessionByEvrID.Range(func(key string, value *sessionWS) bool {
		if p.sessionRegistry.Get(value.ID()) == nil {
			p.logger.Debug("Housekeeping: Session not found for evrID", zap.String("evrID", key))
			p.loginSessionByEvrID.Delete(key)
		}
		return true
	})

	p.matchBySessionID.Range(func(key string, value MatchID) bool {
		if p.sessionRegistry.Get(uuid.FromStringOrNil(key)) == nil {
			p.logger.Debug("Housekeeping: Session not found for matchID", zap.String("matchID", value.String()))
			p.matchBySessionID.Delete(key)
		}
		return true
	})

	p.matchByEvrID.Range(func(key string, value MatchID) bool {
		if match, _, _ := p.matchRegistry.GetMatch(ctx, value.String()); match == nil {
			p.logger.Debug("Housekeeping: Match not found for evrID", zap.String("evrID", key), zap.String("matchID", value.String()))
			p.matchByEvrID.Delete(key)
		}
		return true
	})
}

func clearLinkTickets(ctx context.Context, logger *zap.Logger, nk runtime.NakamaModule) error {
	// Remove existing linktickets
	cursor := ""
	ops := make([]*runtime.StorageDelete, 0)
	objs := make([]*api.StorageObject, 0)
	var err error
	for {
		objs, cursor, err = nk.StorageList(ctx, SystemUserID, SystemUserID, LinkTicketCollection, 2000, "")
		if err != nil {
			return err
		}
		for _, obj := range objs {
			ops = append(ops, &runtime.StorageDelete{
				Collection: LinkTicketCollection,
				Key:        obj.Key,
				UserID:     SystemUserID,
			})
		}
		if cursor == "" {
			break
		}
	}

	if len(ops) > 0 {
		if err := nk.StorageDelete(ctx, ops); err != nil {
			return err
		}
	}
	return nil
}

func (p *EvrPipeline) SetApiServer(apiServer *ApiServer) {
	p.apiServer = apiServer
}

func (p *EvrPipeline) Stop() {}

func (p *EvrPipeline) ProcessRequestEvr(logger *zap.Logger, session *sessionWS, in evr.Message) bool {
	logger = logger.With(zap.String("username", session.Username()))
	if in == nil {
		logger.Error("Received nil message, disconnecting client.")
		return false
	}

	var pipelineFn func(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error

	switch in.(type) {

	// Config service
	case *evr.ConfigRequest:
		pipelineFn = p.configRequest

		// Transaction (IAP) service
	case *evr.IAPReconcile:
		pipelineFn = p.reconcileIAP

		// Login Service
	case *evr.RemoteLogSet:
		pipelineFn = p.remoteLogSetv3
	case evr.LoginRequest:
		pipelineFn = p.loginRequest
	case *evr.DocumentRequest:
		pipelineFn = p.documentRequest
	case *evr.LoggedInUserProfileRequest:
		pipelineFn = p.loggedInUserProfileRequest
	case *evr.ChannelInfoRequest:
		pipelineFn = p.channelInfoRequest
	case *evr.UpdateClientProfile:
		pipelineFn = p.updateClientProfileRequest
	case *evr.OtherUserProfileRequest: // Broadcaster only via it's login connection
		pipelineFn = p.otherUserProfileRequest
	case *evr.UserServerProfileUpdateRequest: // Broadcaster only via it's login connection
		pipelineFn = p.userServerProfileUpdateRequest
	case *evr.GenericMessage:
		pipelineFn = p.genericMessage

	// Match service
	case *evr.LobbyFindSessionRequest:
		pipelineFn = p.lobbyFindSessionRequest
	case *evr.LobbyCreateSessionRequest:
		pipelineFn = p.lobbyCreateSessionRequest
	case *evr.LobbyJoinSessionRequest:
		pipelineFn = p.lobbyJoinSessionRequest
	case *evr.LobbyMatchmakerStatusRequest:
		pipelineFn = p.lobbyMatchmakerStatusRequest
	case *evr.LobbyPingResponse:
		pipelineFn = p.lobbyPingResponse
	case *evr.LobbyPlayerSessionsRequest:
		pipelineFn = p.relayMatchData
	case *evr.LobbyPendingSessionCancel:
		pipelineFn = p.lobbyPendingSessionCancel

	// ServerDB service
	case *evr.BroadcasterRegistrationRequest:

		pipelineFn = p.broadcasterRegistrationRequest
	case *evr.BroadcasterSessionEnded:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterPlayersAccept:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterSessionStarted:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterChallengeRequest:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterPlayersRejected:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterPlayerSessionsLocked:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterPlayerSessionsUnlocked:
		pipelineFn = p.relayMatchData
	case *evr.BroadcasterPlayerRemoved:
		pipelineFn = p.relayMatchData

	default:
		pipelineFn = func(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
			logger.Warn("Received unhandled message", zap.Any("message", in))
			return nil
		}
	}

	switch msg := in.(type) {
	case evr.LoginRequest, *evr.BroadcasterRegistrationRequest:
		// login requests and braodcasters registration requests are first step in the pipeline

	case *evr.ConfigRequest, *evr.IAPReconcile:
		// If the message is a config request or an IAP request, do not require authentication.

	case evr.IdentifyingMessage:
		if err := session.ValidateSession(msg.GetSessionID(), msg.GetEvrID()); err != nil {
			logger.Error("Invalid session", zap.Error(err))
			// Disconnect the client if the session is invalid.
			return false
		}

		// If the session is not authenticated, log the error and return.
		if session != nil && session.UserID() == uuid.Nil {
			logger.Debug("Session not authenticated. Attempting out of band authentication.")
			// As a work around to the serverdb connection getting lost and needing to reauthenticate
			err := p.attemptOutOfBandAuthentication(session)
			if err != nil {
				// If the session is now authenticated, continue processing the request.
				logger.Error("Failed to authenticate session with discordId and password", zap.Error(err))
				return false
			}
		}
	}

	ctx := session.Context()

	// Check matchmaking permission before processing the request

	switch msg := in.(type) {
	case evr.LobbySessionRequest:
		response := NewMatchmakingResult(session, logger, msg.GetMode(), msg.GetChannel())

		// Only bots may join multiple matches
		if flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags); ok {
			if !flags.MultiSession && !flags.IsDeveloper {
				EnforceSingleMatch(logger, session)
			}
		}

		// Get a specific channel
		memberships := ctx.Value(ctxChannelsKey{}).(Memberships)
		channel := msg.GetChannel()
		if channel == evr.GUID(uuid.Nil) {
			// Use the first channel
			membership := memberships.GetPrimary()
			if membership == nil {
				err := response.SendErrorToSession(fmt.Errorf("no available channels"))
				if err != nil {
					logger.Error("Failed to send error to session", zap.Error(err))
				}
				return false
			}
		}

		// Add the matchamking session metadata
		if err := session.MatchSession(); err != nil {
			err := response.SendErrorToSession(err)
			if err != nil {
				logger.Error("Failed to send error to session", zap.Error(err))
			}
			return false
		}

		for _, ch := range channels {
			if ch.ChannelID == channel && ch.isSuspended {
				err := response.SendErrorToSession(fmt.Errorf("channel %s is suspended", channel))
				if err != nil {
					logger.Error("Failed to send error to session", zap.Error(err))
				}
			}
		}
	}

	err := pipelineFn(session.Context(), logger, session, in)
	if err != nil {
		// Unwrap the error
		logger.Error("Pipeline error", zap.Error(err))
		// TODO: Handle errors and close the connection
	}
	// Keep the connection open, otherwise the client will display "service unavailable"
	return true
}

// Process outgoing protobuf envelopes and translate them to Evr messages
func ProcessOutgoing(logger *zap.Logger, session *sessionWS, in *rtapi.Envelope) ([]evr.Message, error) {
	// TODO FIXME Catch the match rejection message and translate it to an evr message
	// TODO FIXME Catch the match leave message and translate it to an evr message
	p := session.evrPipeline

	pipelineFn := func(logger *zap.Logger, session *sessionWS, in *rtapi.Envelope) ([]evr.Message, error) {
		if logger.Core().Enabled(zap.DebugLevel) {
			logger.Debug(fmt.Sprintf("Unhandled protobuf message: %T", in.Message))
		}
		return nil, nil
	}

	switch in.Message.(type) {
	case *rtapi.Envelope_Error:
		envelope := in.GetError()
		logger.Error("Envelope_Error", zap.Int32("code", envelope.Code), zap.String("message", envelope.Message))

	case *rtapi.Envelope_MatchPresenceEvent:
		envelope := in.GetMatchPresenceEvent()
		userID := session.UserID().String()
		matchID := MatchIDFromStringOrNil(envelope.GetMatchId())

		for _, leave := range envelope.GetLeaves() {
			if leave.GetUserId() != userID {
				// This is not this user
				continue
			}
		}

		for _, join := range envelope.GetJoins() {
			if join.GetUserId() != userID {
				// This is not this user.
				continue
			}

			p.matchBySessionID.Store(session.ID().String(), matchID)

			if evrId, ok := session.Context().Value(ctxEvrIDKey{}).(evr.EvrId); ok {
				p.matchByEvrID.Store(evrId.Token(), matchID)
			}

			go func() {
				// Remove the match lookup entries when the session is closed.
				<-session.Context().Done()

				if evrId, ok := session.Context().Value(ctxEvrIDKey{}).(evr.EvrId); ok {
					p.matchByEvrID.Delete(evrId.Token())
				}

				p.matchBySessionID.Delete(session.ID().String())
			}()
		}

		pipelineFn = func(_ *zap.Logger, _ *sessionWS, _ *rtapi.Envelope) ([]evr.Message, error) {
			return nil, nil
		}
	default:
		// No translation needed.
	}

	if logger.Core().Enabled(zap.DebugLevel) && in.Message != nil {
		logger.Debug(fmt.Sprintf("outgoing protobuf message: %T", in.Message))
	}

	return pipelineFn(logger, session, in)
}

// relayMatchData relays the data to the match by determining the match id from the session or user id.
func (p *EvrPipeline) relayMatchData(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	var matchID MatchID
	var found bool

	// Try to load the match ID from the session ID (this is how broadcasters can be found)
	matchID, found = p.matchBySessionID.Load(session.ID().String())
	if !found {
		// Try to load the match ID from the presence stream
		sessionIDs := session.tracker.ListLocalSessionIDByStream(PresenceStream{Mode: StreamModeEvr, Subject: session.id, Subcontext: StreamContextMatch})
		if len(sessionIDs) == 0 {
			return fmt.Errorf("no matchmaking session for user %s session: %s", session.UserID(), session.ID())
		}
		sessionID := sessionIDs[0]

		matchID, found = p.matchBySessionID.Load(sessionID.String())

		if !found {
			return fmt.Errorf("no match found for user %s session: %s", session.UserID(), session.ID())
		}
	}

	err := sendMatchData(p.matchRegistry, matchID, session, in)
	if err != nil {
		return fmt.Errorf("failed to send match data: %w", err)
	}

	return nil
}

// sendMatchData sends the data to the match.
func sendMatchData(matchRegistry MatchRegistry, matchID MatchID, session *sessionWS, in evr.Message) error {

	requestJson, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Set the OpCode to the symbol of the message.
	opCode := int64(evr.SymbolOf(in))
	// Send the data to the match.
	matchRegistry.SendData(matchID.uuid, matchID.Node(), session.UserID(), session.ID(), session.Username(), matchID.Node(), opCode, requestJson, true, time.Now().UTC().UnixNano()/int64(time.Millisecond))

	return nil
}

var ErrOutOfBandAuthFailure = errors.New("out of band authentication failed")
var ErrNoAuthProvided = errors.Join(ErrOutOfBandAuthFailure, errors.New("no authentication provided"))

// configRequest handles the config request.
func (p *EvrPipeline) attemptOutOfBandAuthentication(session *sessionWS) error {
	ctx := session.Context()
	// If the session is already authenticated, return.
	if session.UserID() != uuid.Nil {
		return nil
	}

	// Get the email and password from the basic auth.
	discordAuthID, userPassword, ok := parseBasicAuth(ctx.Value(ctxBasicAuthKey{}).(string))
	if !ok {
		return ErrNoAuthProvided
	}
	// Get the account for this discordID
	uid, err := p.discordRegistry.GetUserIdByDiscordId(ctx, discordAuthID, false)
	if err != nil {
		return errors.Join(ErrOutOfBandAuthFailure, fmt.Errorf("failed to get user id by discord id %s: %w", discordAuthID, err))
	}
	userID := uid.String()

	// The account was found.
	account, err := GetAccount(ctx, session.logger, session.pipeline.db, session.statusRegistry, uuid.FromStringOrNil(userID))
	if err != nil {
		return errors.Join(ErrOutOfBandAuthFailure, fmt.Errorf("failed to get account: %w", err))
	}

	// Do email authentication
	userID, username, _, err := AuthenticateEmail(ctx, session.logger, session.pipeline.db, account.Email, userPassword, "", false)
	if err != nil {
		return errors.Join(ErrOutOfBandAuthFailure, fmt.Errorf("failed to authenticate email: %w", err))
	}

	return session.BroadcasterSession(userID, "broadcaster:"+username)
}
