package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
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

	config, err := LoadMatchmakingSettings(ctx, p.nk, session.UserID().String())
	if err != nil {
		return true, LobbySessionError(req, evr.LobbySessionFailures.InternalError, "unable to load matchmaking settings: "+err.Error())
	}

	if config.GroupID == "" {
		// just use the session ID
		config.GroupID = session.ID().String()
	}

	party, err := JoinOrCreatePartyGroup(session.(*sessionWS), config.GroupID)
	if err != nil {
		return true, LobbySessionError(req, evr.LobbySessionFailures.InternalError, "unable to join or create party group: "+err.Error())
	}

	// TODO need to get correct displayname for this group
	memberships := ctx.Value(ctxMembershipsKey{}).([]GuildGroupMembership)

	groupID := uuid.Nil
	// If the player is currently in a social lobby, then set the group ID to the same group ID
	if req.GetCurrentLobbyID() != uuid.Nil {
		label, err := MatchLabelByID(ctx, p.nk, MatchID{uuid: req.GetCurrentLobbyID(), node: p.node})
		if err != nil {
			return true, LobbySessionError(req, evr.LobbySessionFailures.InternalError, "unable to get current lobby label: "+err.Error())
		}
		if label.GroupID != nil && (label.Mode == evr.ModeSocialPublic || label.Mode == evr.ModeSocialPrivate) {
			groupID = *label.GroupID
		}
	}

	// Make sure the user has the permissions to matchmake in this group
	membership := memberships[0]
	for _, m := range memberships {
		if membership.ID() == groupID && membership.isMember {
			membership = m
			break
		}
	}

	req.SetGroupID(membership.ID())

	discordID, err := GetDiscordIDByUserID(ctx, p.db, session.UserID().String())
	if err != nil {
		logger.Error("Failed to get discord id", zap.Error(err))
	}

	presence := NewEntrantPresence(session.ID(), req.GetLoginSessionID(), session.UserID(), req.GetEvrID(), party.ID(), session.Username(), membership.DisplayName.String(), discordID, session.ClientIP(), session.ClientPort(), p.node, req.GetRoleAlignment())

	switch m := in.(type) {
	case *evr.LobbyFindSessionRequest:
		err = p.lobbySessionFind(logger, session, m, party, presence)
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

func (p *EVRPipeline) lobbySessionFind(logger *zap.Logger, session EVRSession, in *evr.LobbyFindSessionRequest, party *PartyGroup, presence EntrantPresence) error {
	ctx := session.Context()

	// Find out what match the leader is in.
	leader := party.GetLeader()
	// If this player is the leader, then reserve spots in the current match for the party.
	if leader.GetSessionId() == session.ID().String() {
		if party.Size() > 1 {
			partyPresences := party.GetPresences()

			// If the player is the leader, then reserve spots in the current match for the party.
			label, err := MatchLabelByID(ctx, p.nk, MatchID{uuid: in.GetCurrentLobbyID(), node: p.node})
			if err != nil {
				return fmt.Errorf("failed to get current lobby label: %w", err)
			}

			// Is the whole party in the match with leader?
			headcount := 0
			for _, p := range label.Players {
				if p.PartyID == party.ID().String() {
					headcount++
				}
			}
			neededSlots := party.Size() - headcount
			// Check how many reservations the match has for the remaining party.
			reservedSlots := 0
			for _, p := range label.Reservations {
				if p.PartyID == party.ID().String() {
					reservedSlots++
				}
			}

			query := ""
			if reservedSlots < neededSlots || label.GetAvailablePlayerSlots() < neededSlots {
				// Move to a new social lobby, this one does not have enough slots.
				query = fmt.Sprintf("+label.open:T +label.mode:social_2.0 -label.id:%s +label.group_id:%s +label.players.party_id:%s^100", in.GetCurrentLobbyID(), in.GetGroupID().String(), party.ID().String())
			} else {
				// Start real matchmaking
				query = fmt.Sprintf("+label.open:T +label.mode:%s -label.id:%s +label.group_id:%s +label.players.party_id:%s^100", in.GetMode().String(), in.GetCurrentLobbyID(), in.GetGroupID().String(), party.ID().String())
			}

			for {
				// Cycle until a match is found
				creatTicker := time.NewTicker(time.Second * 5)
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled")
				case <-time.After(time.Second):
				}
				minSize := 0
				maxSize := LobbyMaxSize(in.GetMode()) - party.Size()
				limit := 30
				matches, err := MatchList(ctx, p.matchRegistry, limit, minSize, maxSize, query)
				if err != nil {
					return fmt.Errorf("failed to list matches: %w", err)
				}
				for _, match := range matches {
					if err := LobbySessionJoinAttempt(ctx, logger, MatchIDFromStringOrNil(match.MatchId), p.sessionRegistry, p.matchRegistry, p.tracker, label, &presence, partyPresences); err != nil {
						logger.Warn("Failed to join match", zap.String("mid", match.MatchId), zap.Error(err))
					}
				}
				// If no matches were found, and the ticket is up, then create a new match.
				select {
				case <-creatTicker.C:
					// Creat ea new match
					if matchID, err := p.CreateSession(ctx, session.(*sessionWS), in.Mode); err != nil {
						logger.Error("Failed to create match", zap.Error(err))
					} else {
						if err := LobbySessionJoinAttempt(ctx, logger, matchID, p.sessionRegistry, p.matchRegistry, p.tracker, label, &presence, partyPresences); err != nil {
							logger.Warn("Failed to join match", zap.String("mid", matchID.String()), zap.Error(err))
						}
					}
				default:
				}
			}
		}
	}

	leaderMatch, _, err := GetMatchBySessionID(p.nk, uuid.FromStringOrNil(leader.GetSessionId()))
	if err != nil {
		return fmt.Errorf("failed to get leader match: %w", err)
	}
	// If the leader is in a match, then immediately try to join the match
	if !leaderMatch.IsNil() {
		label, err := MatchLabelByID(ctx, p.nk, leaderMatch)
		if err != nil {
			return fmt.Errorf("failed to get leader match label: %w", err)
		}
		if err := LobbySessionJoinAttempt(ctx, logger, leaderMatch, p.sessionRegistry, p.matchRegistry, p.tracker, label, &presence, nil); err != nil {
			logger.Warn("Failed to join leader match", zap.String("leader_session_id", leader.GetSessionId()), zap.String("mid", leaderMatch.UUID().String()), zap.Error(err))
		}
	}
	// If the player is not in a social lobby, then join a social lobby that has enough space for the entire party.

	query := fmt.Sprintf("+label.open:T +label.mode:%s -label.id:%s +label.group_id:%s +label.players.party_id:%s^100", in.GetMode().String(), in.GetCurrentLobbyID(), in.GetGroupID().String(), party.ID().String())

	minSize := 1
	maxSize := LobbyMaxSize(in.GetMode())
	limit := 20
	matches, err := MatchList(ctx, p.matchRegistry, limit, minSize, maxSize, query)
	if err != nil {
		return fmt.Errorf("failed to list matches: %w", err)
	}

	if len(matches) == 0 {
		logger.Info("No matches found")
		return fmt.Errorf("no matches found")
	}

	for _, match := range matches {
		label := EvrMatchState{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), &label); err != nil {
			continue
		}
		if label.GetAvailablePlayerSlots() > 0 {
			// Found a match with available slots.
			return LobbySessionJoinAttempt(ctx, logger, label.ID, p.sessionRegistry, p.matchRegistry, p.tracker, &label, &presence, nil)
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

func LobbySessionJoinAttempt(ctx context.Context, logger *zap.Logger, matchID MatchID, sessionRegistry SessionRegistry, matchRegistry MatchRegistry, tracker Tracker, label *EvrMatchState, presence *EntrantPresence, partyPresences []rtapi.UserPresence) (err error) {

	label, presence, _, err = EVRMatchJoinAttempt(ctx, logger, matchID, sessionRegistry, matchRegistry, tracker, *presence, partyPresences)
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

func (p *EVRPipeline) CreateSession(ctx context.Context, session *sessionWS, mode evr.Symbol) (matchID MatchID, err error) {
	ml := &EvrMatchState{
		Mode:      mode,
		SpawnedBy: session.UserID().String(),
	}
	ml.MaxSize = MatchMaxSize

	memberships := ctx.Value(ctxMembershipsKey{}).([]GuildGroupMembership)
	groupIDs := make([]string, 0, len(memberships))
	for _, m := range memberships {
		groupIDs = append(groupIDs, m.ID().String())
	}

	query := fmt.Sprintf("+label.open:T +label.lobby_type:unassigned +label.broadcaster.group_ids:/(%s)/", strings.Join(groupIDs, "|"))

	minSize := 1
	maxSize := LobbyMaxSize(MatchMaxSize)
	limit := 60
	matches, err := MatchList(ctx, p.matchRegistry, limit, minSize, maxSize, query)
	if err != nil {
		return MatchID{}, fmt.Errorf("failed to list matches: %w", err)
	}

	if len(matches) == 0 {
		return MatchID{}, ErrMatchmakingNoAvailableServers
	}

	matchID = MatchIDFromStringOrNil(matches[0].MatchId)

	// Prepare the match
	_, err = SignalMatch(ctx, p.matchRegistry, matchID, SignalPrepareSession, ml)
	if err != nil {
		return MatchID{}, ErrMatchmakingUnknownError
	}

	// Return the prepared session
	return matchID, nil
}
