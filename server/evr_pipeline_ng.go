package server

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

type EVRSession interface {
	Session
	SendEVR(message ...evr.Message) error
	ValidateSession(loginSessionID uuid.UUID, evrID evr.EvrID) error
}

type (
	EVRBeforeFunction func(ctx context.Context, logger *zap.Logger, userID, username string, vars map[string]string, expiry int64, sessionID, clientIP, clientPort, lang string, in evr.Message) (evr.Message, error)
	EVRAfterFunction  func(ctx context.Context, logger *zap.Logger, userID, username string, vars map[string]string, expiry int64, sessionID, clientIP, clientPort, lang string, out, in evr.Message) error
)

type EVRPipeline struct {
	config          Config
	db              *sql.DB
	matchRegistry   MatchRegistry
	sessionRegistry SessionRegistry
	tracker         Tracker
	metrics         Metrics

	nk   runtime.NakamaModule
	node string

	discordRegistry DiscordRegistry

	beforeRtFunctions map[string]EVRBeforeFunction
	afterRtFunctions  map[string]EVRAfterFunction
}

func NewEVRPipelineNG(config Config, nk runtime.NakamaModule, db *sql.DB, matchRegistry MatchRegistry, sessionRegistry SessionRegistry, tracker Tracker, metrics Metrics, discordRegistry DiscordRegistry) *EVRPipeline {

	beforeFunctions := map[string]EVRBeforeFunction{
		"sessionrequest": beforeSessionRequestHook,
	}
	afterFunctions := map[string]EVRAfterFunction{}

	pipeline := EVRPipeline{
		config:          config,
		matchRegistry:   matchRegistry,
		sessionRegistry: sessionRegistry,
		tracker:         tracker,
		metrics:         metrics,

		db: db,
		nk: nk,

		node: config.GetName(),

		beforeRtFunctions: beforeFunctions,
		afterRtFunctions:  afterFunctions,
	}

	return &pipeline

}

func (p *EVRPipeline) ProcessRequest(logger *zap.Logger, session EVRSession, in evr.Message) bool {
	if logger.Core().Enabled(zap.DebugLevel) { // remove extra heavy reflection processing
		logger.Debug(fmt.Sprintf("Received %T message", in), zap.Any("message", in))
	}

	if in == nil {
		logger.Error("Missing message.")
		return false
	}

	var pipelineFn func(*zap.Logger, EVRSession, evr.Message) (bool, evr.Message)

	switch in.(type) {
	// Match service
	case SessionRequest:
		pipelineFn = p.lobbySession
		/*
			case *evr.LobbyMatchmakerStatusRequest:
				pipelineFn = p.lobbyMatchmakerStatusRequest
			case *evr.LobbyPingResponse:
				pipelineFn = p.lobbyPingResponse
			case *evr.LobbyPlayerSessionsRequest:
				pipelineFn = p.lobbyPlayerSessionsRequest
			case *evr.LobbyPendingSessionCancel:
				pipelineFn = p.lobbyPendingSessionCancel
		*/
	}

	// Validate Session will "authenticate" this connection against the login session.
	if idmessage, ok := in.(evr.IdentifyingMessage); ok {
		if err := session.ValidateSession(idmessage.GetLoginSessionID(), idmessage.GetEvrID()); err != nil {
			logger.Error("Invalid session, disconnecting client.", zap.Error(err))
			return false // Disconnect the client if the session is invalid.
		}
	}

	success, out := pipelineFn(logger, session, in)

	if out != nil {
		if err := session.SendEVR(out); err != nil {
			logger.Error("Failed to send message.", zap.Error(err))
		}
	}
	success = false

	if !success {
		go func() {
			logger.Error("Disconnecting client after failed message processing.")
			// Lock the close for 30 seconds to prevent the client from being disconnected immediately after the message is sent.
			session.CloseLock()
			defer session.CloseUnlock()
			<-time.After(30 * time.Second)
		}()
	}

	return success
}

func (p *EVRPipeline) ProcessOutgoing(logger *zap.Logger, session *sessionWS, in *rtapi.Envelope) ([]evr.Message, error) {

	switch in.Message.(type) {
	case *rtapi.Envelope_StreamData:
		// Validate the stream data is EVR data.
		return nil, session.SendBytes([]byte(in.GetStreamData().GetData()), true)
	case *rtapi.Envelope_MatchData:
		if in.GetMatchData().GetOpCode() == OpCodeEVRPacketData {
			return nil, session.SendBytes(in.GetMatchData().GetData(), true)
		}
	}

	// Relay the message to Discord if the user is not a broadcaster, and has verbose set.
	if session.Context().Value(ctxVerboseKey{}).(bool) && !strings.HasPrefix(session.Username(), "broadcaster:") {
		return p.DiscordRTAPIRelay(logger, session, p.discordRegistry.GetBot(), in)
	}

	return nil, nil
}
