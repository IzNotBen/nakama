package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func LobbyMaxSize(mode evr.Symbol) int {
	switch mode {
	case evr.ModeSocialPublic, evr.ModeSocialPrivate:
		return 12
	}
	return 15
}

type SessionRequest interface {
	evr.IdentifyingMessage
	GetMode() evr.Symbol
	GetLevel() evr.Symbol
	GetCurrentLobbyID() uuid.UUID
	GetRoleAlignment() int
	GetGroupID() uuid.UUID
	SetGroupID(uuid.UUID)
}

func LobbySessionError(req SessionRequest, code evr.LobbySessionErrorCode, message string) evr.Message {
	return evr.NewLobbySessionFailure(req.GetMode(), req.GetGroupID(), code, message).Version4()
}

func (p *EVRPipeline) lobbySession(logger *zap.Logger, session EVRSession, in evr.Message) (bool, evr.Message) {
	req := in.(SessionRequest)

	ctx := session.Context()

	// TODO Send ping

	// TODO need to join to party
	partyID := uuid.Nil

	// TODO need to get correct displayname for this group
	displayName := "a name"

	discordID, err := GetDiscordIDByUserID(ctx, p.db, session.UserID().String())
	if err != nil {
		logger.Error("Failed to get discord id", zap.Error(err))
	}

	roleAlignment := req.GetRoleAlignment()

	presence := NewEntrantPresence(session.ID(), req.GetLoginSessionID(), session.UserID(), req.GetEvrID(), partyID, session.Username(), displayName, discordID, session.ClientIP(), session.ClientPort(), p.node, roleAlignment)

	switch m := in.(type) {
	case *evr.LobbyFindSessionRequest:
		err = p.lobbySessionFind(logger, session, m, presence)
	case *evr.LobbyCreateSessionRequest:
		//return p.lobbySessionCreate(logger, session, m, groupID, presence)
	case *evr.LobbyJoinSessionRequest:
		//return p.lobbySessionJoin(logger, session, m, groupID, presence)
	}
	if err != nil {
		return true, LobbySessionError(req, evr.LobbySessionFailures.InternalError, err.Error())
	}
	return true, nil
}

func (p *EVRPipeline) lobbySessionFind(logger *zap.Logger, session EVRSession, in *evr.LobbyFindSessionRequest, presence EntrantPresence) error {

	ctx := session.Context()

	query := fmt.Sprintf("+label.open:T +label:mode:%s -label.id:%s +label.group_id:%s", in.GetMode().String(), in.GetCurrentLobbyID(), in.GetGroupID().String())

	minSize := 1
	maxSize := LobbyMaxSize(in.GetMode())
	limit := 20
	matches, err := MatchList(ctx, p.matchRegistry, limit, minSize, maxSize, query)
	if err != nil {
		logger.Error("Failed to list matches", zap.Error(err))
		return fmt.Errorf("failed to list matches: %w", err)
	}

	if len(matches) == 0 {
		logger.Info("No matches found")
		return fmt.Errorf("no matches found")
	}

	for _, match := range matches {
		label := EvrMatchState{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), &label); err != nil {
			logger.Error("Failed to unmarshal match label", zap.Error(err))
			continue
		}
		if label.GetAvailablePlayerSlots() > 0 {
			// Found a match with available slots.
			return LobbySessionJoinAttempt(ctx, logger, label.ID, p.sessionRegistry, p.matchRegistry, p.tracker, &label, &presence)
		}
	}
	return nil
}

// listMatches returns a list of matches
func MatchList(ctx context.Context, matchRegistry MatchRegistry, limit int, minSize, maxSize int, query string) ([]*api.Match, error) {
	authoritativeWrapper := &wrapperspb.BoolValue{Value: true}
	var labelWrapper *wrapperspb.StringValue
	var queryWrapper *wrapperspb.StringValue
	if query != "" {
		queryWrapper = &wrapperspb.StringValue{Value: query}
	}
	minSizeWrapper := &wrapperspb.Int32Value{Value: int32(minSize) + 1} // Add one for the game server

	maxSizeWrapper := &wrapperspb.Int32Value{Value: int32(maxSize) + 1} // Add one for the game server

	matches, _, err := matchRegistry.ListMatches(ctx, limit, authoritativeWrapper, labelWrapper, minSizeWrapper, maxSizeWrapper, queryWrapper, nil)
	return matches, err
}

func NewEntrantPresence(sessionID, loginSessionID, userID uuid.UUID, evrID evr.EvrID, partyID uuid.UUID, username, displayName, discordID string, clientIP, clientPort, node string, roleAlignment int) EntrantPresence {
	return EntrantPresence{
		Node:           node,
		UserID:         userID,
		SessionID:      sessionID,
		LoginSessionID: loginSessionID,
		Username:       username,
		DisplayName:    displayName,
		EvrID:          evrID,
		PartyID:        partyID,
		RoleAlignment:  roleAlignment,
		DiscordID:      discordID,
		ClientIP:       clientIP,
		ClientPort:     clientPort,
	}
}

func LobbySessionJoinAttempt(ctx context.Context, logger *zap.Logger, matchID MatchID, sessionRegistry SessionRegistry, matchRegistry MatchRegistry, tracker Tracker, label *EvrMatchState, presence *EntrantPresence) (err error) {
	label, presence, _, err = EVRMatchJoinAttempt(ctx, logger, matchID, sessionRegistry, matchRegistry, tracker, *presence)
	switch err {
	case nil:
	case ErrDuplicateJoin:
		return nil
	default:
		return fmt.Errorf("failed to join match: %w", err)
	}

	// Get the broadcasters session
	bsession := sessionRegistry.Get(uuid.FromStringOrNil(label.Broadcaster.SessionID)).(EVRSession)
	if bsession == nil {
		return fmt.Errorf("broadcaster session not found: %s", label.Broadcaster.SessionID)
	}

	msg := evr.NewLobbySessionSuccess(label.Mode, label.ID.UUID(), label.GetGroupID(), label.GetEndpoint(), int16(presence.RoleAlignment))
	messages := []evr.Message{msg.Version4(), msg.Version5(), evr.NewSTcpConnectionUnrequireEvent()}

	if err = bsession.SendEVR(messages...); err != nil {
		return fmt.Errorf("failed to send messages to broadcaster: %w", err)
	}
	session := sessionRegistry.Get(presence.SessionID).(EVRSession)
	if err = session.SendEVR(messages...); err != nil {
		err = fmt.Errorf("failed to send messages to player: %w", err)
	}

	return err
}
