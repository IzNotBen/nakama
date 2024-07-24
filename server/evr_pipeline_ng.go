package server

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
)

type EVRSession interface {
	Session
	SendEVR(message ...evr.Message) error
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

	beforeRtFunctions map[string]EVRBeforeFunction
	afterRtFunctions  map[string]EVRAfterFunction
}

func NewEVRPipelineNG(config Config, nk runtime.NakamaModule, db *sql.DB, matchRegistry MatchRegistry, sessionRegistry SessionRegistry, tracker Tracker, metrics Metrics) *EVRPipeline {
	return &EVRPipeline{
		config:          config,
		matchRegistry:   matchRegistry,
		sessionRegistry: sessionRegistry,
		tracker:         tracker,
		metrics:         metrics,

		db: db,
		nk: nk,

		node: config.GetName(),

		beforeRtFunctions: make(map[string]EVRBeforeFunction),
		afterRtFunctions:  make(map[string]EVRAfterFunction),
	}
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

	var messageName, messageNameID string

	switch in.(type) {
	default:
		messageName = fmt.Sprintf("%T", in)
		messageNameID = strings.ToLower(messageName)

		if fn := p.BeforeRt(messageNameID); fn != nil {
			hookResult, hookErr := fn(session.Context(), logger, session.UserID().String(), session.Username(), session.Vars(), session.Expiry(), session.ID().String(), session.ClientIP(), session.ClientPort(), session.Lang(), in)

			if hookErr != nil {
				logger.Error("Error in before hook.", zap.String("resource", messageName), zap.Error(hookErr))
				return true
			} else if hookResult == nil {
				// If result is nil, requested resource is disabled. Sessions calling disabled resources will be closed.
				logger.Warn("Intercepted a disabled resource.", zap.String("resource", messageName))
				return false
			}

			in = hookResult
		}
	}

	success, out := pipelineFn(logger, session, in)

	if success && messageName != "" {
		// Unsuccessful operations do not trigger after hooks.
		if fn := p.AfterRt(messageNameID); fn != nil {
			_ = fn(session.Context(), logger, session.UserID().String(), session.Username(), session.Vars(), session.Expiry(), session.ID().String(), session.ClientIP(), session.ClientPort(), session.Lang(), out, in)
		}
	}

	return true
}

func (p *EVRPipeline) BeforeRt(name string) EVRBeforeFunction {
	return p.beforeRtFunctions[name]
}

func (p *EVRPipeline) AfterRt(name string) EVRAfterFunction {
	return p.afterRtFunctions[name]
}
