package server

import (
	"context"
	"database/sql"
	"encoding/json"
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
		// Disconnect the client after 30 seconds. If the client is immediately disconnected, the client will replace the message with a disconnect message.
		// This is to prevent the client from being disconnected immediately after the message is sent.
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

// Process outgoing protobuf envelopes and translate them to Evr messages
func (p *EVRPipeline) ProcessOutgoing(logger *zap.Logger, session *sessionWS, in *rtapi.Envelope) ([]evr.Message, error) {
	// TODO FIXME Catch the match rejection message and translate it to an evr message
	// TODO FIXME Catch the match leave message and translate it to an evr message

	switch in.Message.(type) {
	case *rtapi.Envelope_StreamData:
		// Validate the stream data is EVR data.
		return nil, session.SendBytes([]byte(in.GetStreamData().GetData()), true)
	case *rtapi.Envelope_MatchData:
		if in.GetMatchData().GetOpCode() == OpCodeEVRPacketData {
			return nil, session.SendBytes(in.GetMatchData().GetData(), true)
		}
	}

	verbose, ok := session.Context().Value(ctxVerboseKey{}).(bool)
	if !ok {
		verbose = false
	}

	// DM the user on discord
	if !strings.HasPrefix(session.Username(), "broadcaster:") && verbose {
		content := ""
		switch in.Message.(type) {
		case *rtapi.Envelope_StatusPresenceEvent, *rtapi.Envelope_MatchPresenceEvent, *rtapi.Envelope_StreamPresenceEvent:
		case *rtapi.Envelope_Party:
			discordIDs := make([]string, 0)
			leader := in.GetParty().GetLeader()
			userIDs := make([]string, 0)

			// Put leader first
			if leader != nil {
				userIDs = append(userIDs, leader.GetUserId())
			}
			for _, m := range in.GetParty().GetPresences() {
				if m.GetUserId() == leader.GetUserId() {
					continue
				}
				userIDs = append(userIDs, m.GetUserId())
			}

			for _, userID := range userIDs {
				if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, userID); err != nil {
					logger.Warn("Failed to get discord ID", zap.Error(err))
					discordIDs = append(discordIDs, userID)
				} else {
					discordIDs = append(discordIDs, fmt.Sprintf("<@%s>", discordID))
				}
			}

			content = fmt.Sprintf("Active party: %s", strings.Join(discordIDs, ", "))
		case *rtapi.Envelope_PartyLeader:
			if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, in.GetPartyLeader().GetPresence().GetUserId()); err != nil {
				logger.Warn("Failed to get discord ID", zap.Error(err))
				content = fmt.Sprintf("Party leader: %s", in.GetPartyLeader().GetPresence().GetUsername())
			} else {
				content = fmt.Sprintf("New party leader: <@%s>", discordID)
			}

		case *rtapi.Envelope_PartyJoinRequest:

		case *rtapi.Envelope_PartyPresenceEvent:
			event := in.GetPartyPresenceEvent()
			joins := make([]string, 0)

			for _, join := range event.GetJoins() {
				if join.GetUserId() != session.UserID().String() {
					if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, join.GetUserId()); err != nil {
						logger.Warn("Failed to get discord ID", zap.Error(err))
						joins = append(joins, join.GetUsername())
					} else {
						joins = append(joins, fmt.Sprintf("<@%s>", discordID))
					}
				}
			}
			leaves := make([]string, 0)
			for _, leave := range event.GetLeaves() {
				if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, leave.GetUserId()); err != nil {
					logger.Warn("Failed to get discord ID", zap.Error(err))
					leaves = append(leaves, leave.GetUsername())
				} else {
					leaves = append(leaves, fmt.Sprintf("<@%s>", discordID))
				}
			}

			if len(joins) > 0 {
				content += fmt.Sprintf("Party join: %s\n", strings.Join(joins, ", "))
			}
			if len(leaves) > 0 {
				content += fmt.Sprintf("Party leave: %s\n", strings.Join(leaves, ", "))
			}

		default:
			if data, err := json.MarshalIndent(in.GetMessage(), "", "  "); err != nil {
				logger.Error("Failed to marshal message", zap.Error(err))
			} else if len(data) > 2000 {
				content = "Message too long to display"
			} else if len(data) > 0 {
				content = string("```json\n" + string(data) + "\n```")
			}
		}

		if content != "" {
			if dg := p.discordRegistry.GetBot(); dg == nil {
				// No discord bot
			} else if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, session.UserID().String()); err != nil {
				logger.Warn("Failed to get discord ID", zap.Error(err))
			} else if channel, err := dg.UserChannelCreate(discordID); err != nil {
				logger.Warn("Failed to create DM channel", zap.Error(err))
			} else if _, err = dg.ChannelMessageSend(channel.ID, content); err != nil {
				logger.Warn("Failed to send message to user", zap.Error(err))
			}
		}
	}
	return nil, nil
}
