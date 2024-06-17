package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// lobbyMatchmakerStatusRequest is a message requesting the status of the matchmaker.
func (p *EvrPipeline) lobbyMatchmakerStatusRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	_ = in.(*evr.LobbyMatchmakerStatusRequest)

	// TODO Check if the matchmaking ticket is still open
	err := session.SendEvr(evr.NewLobbyMatchmakerStatusResponse())
	if err != nil {
		return fmt.Errorf("LobbyMatchmakerStatus: %v", err)
	}
	return nil
}

// authorizeMatchmaking checks if the user is allowed to join a public match or spawn a new match
func (p *EvrPipeline) authorizeMatchmaking(ctx context.Context, logger *zap.Logger, session *sessionWS, loginSessionID uuid.UUID, groupID uuid.UUID, requireMembership bool) error {

	if groupID == uuid.Nil {
		var ok bool
		groupID, ok = ctx.Value(ctxGroupIDKey{}).(uuid.UUID)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "Failed to get group ID from context")
		}
	}

	// Get the EvrID from the context
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "Failed to get EVR ID")
	}
	if !evrID.Valid() {
		return status.Errorf(codes.InvalidArgument, "Invalid EVR ID")
	}

	if session.userID == uuid.Nil {
		return status.Errorf(codes.Unauthenticated, "User not authenticated")
		return status.Error(codes.Unauthenticated, "User not authenticated")
	}

	// Only bots may join multiple matches
	if flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags); ok {
		if flags.SingleSession && !flags.IsDeveloper {
			EnforceSingleMatch(logger, session, evrID)
		}
	}

	if err := session.MatchSession(); err != nil {
		return fmt.Errorf("failed to get match session: %w", err)
	}

	if channel == uuid.Nil {
		logger.Warn("Channel is nil")
		return nil
	}

	// Check for suspensions on this channel.
	suspensions, err := p.checkSuspensionStatus(ctx, logger, session.UserID().String(), channel)

	if err != nil {
		return err
	}

	if len(suspensions) != 0 {
		msg := suspensions[0].Reason
		return errors.New("Suspended: " + msg)
	}

	return nil
}

func EnforceSingleMatch(logger *zap.Logger, session *sessionWS, evrID evr.EvrId) {
	// Disconnect this EVRID from other matches
	sessionIDs := session.tracker.ListLocalSessionIDByStream(PresenceStream{Mode: StreamModeEvr, Subject: evrID.UUID(), Subcontext: StreamContextMatch})
	for _, foundSessionID := range sessionIDs {
		if foundSessionID == session.id {
			// Allow the current session, only disconnect any older ones.
			continue
		}

			// Disconnect the older session.
			logger.Debug("Disconnecting older session from matchmaking", zap.String("other_sid", foundSessionID.String()))
			fs := p.sessionRegistry.Get(foundSessionID)
			if fs == nil {
				logger.Warn("Failed to find older session to disconnect", zap.String("other_sid", foundSessionID.String()))
				continue
			}
			// Send an error
			fs.Close("New session started", runtime.PresenceReasonDisconnect)
		}
	}
	// Track this session as a matchmaking session.
	s := session
	s.tracker.TrackMulti(s.ctx, s.id, []*TrackerOp{
		{
			Stream: PresenceStream{Mode: StreamModeEvr, Subject: session.id, Subcontext: svcMatchID},
			Meta:   PresenceMeta{Format: s.format, Hidden: true},
		},
		// By login sessionID and match service ID
		{
			Stream: PresenceStream{Mode: StreamModeEvr, Subject: loginSessionID, Subcontext: svcMatchID},
			Meta:   PresenceMeta{Format: s.format, Hidden: true},
		},
		// By EVRID and match service ID
		{
			Stream: PresenceStream{Mode: StreamModeEvr, Subject: evrID.UUID(), Subcontext: svcMatchID},
			Meta:   PresenceMeta{Format: s.format, Hidden: true},
		},
		// EVR packet data stream for the match session by Session ID and service ID
	}, s.userID)

	// Check if th user is a member of this guild
	guild, err := p.discordRegistry.GetGuildByGroupId(ctx, groupID.String())
	if err != nil || guild == nil {
		return status.Errorf(codes.Internal, "Failed to get guild: %v", err)
	}

	discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, session.userID)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to get discord id: %v", err)
	}

	// Check if the user is a member of this guild
	member, err := p.discordRegistry.GetGuildMember(ctx, guild.ID, discordID)
	if err != nil || member == nil || member.User == nil {
		if requireMembership {
			return status.Errorf(codes.PermissionDenied, "User is not a member of this guild")
		} else {
			return status.Errorf(codes.NotFound, "User is not a member of this guild")
		}
	}

	// Check if the guild has a membership role defined in the metadata
	if requireMembership {
		metadata, err := p.discordRegistry.GetGuildGroupMetadata(ctx, groupID.String())
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get guild metadata: %v", err)
		}
		if metadata.MemberRole != "" {
			// Check if the user has the membership role
			if !lo.Contains(member.Roles, metadata.MemberRole) {
				return status.Errorf(codes.PermissionDenied, "User does not have the required role to join this guild")
			}
		}
	}

	// Check for suspensions on this groupID.
	suspensions, err := p.checkSuspensionStatus(ctx, logger, session.UserID().String(), groupID)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to check suspension status: %v", err)
		// Disconnect the older session.
		logger.Debug("Disconnecting older session from matchmaking", zap.String("other_sid", foundSessionID.String()))
		foundSession := session.sessionRegistry.Get(foundSessionID)
		if foundSession == nil {
			logger.Warn("Failed to find older session to disconnect", zap.String("other_sid", foundSessionID.String()))
			continue
		}
		// Send an error
		foundSession.Close("New session started", runtime.PresenceReasonDisconnect)
	}
	if len(suspensions) != 0 {
		msg := suspensions[0].Reason

		return status.Errorf(codes.PermissionDenied, msg)
	}
	return nil
}



func (p *EvrPipeline) matchmakingLabelFromFindRequest(ctx context.Context, session *sessionWS, request *evr.LobbyFindSessionRequest) (*EvrMatchState, error) {

	// If the channel is nil, use the players profile channel
	groupID := request.Channel
	if groupID == uuid.Nil {
		var ok bool
		groupID, ok = ctx.Value(ctxGroupIDKey{}).(uuid.UUID)
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, "Failed to get group ID from context")
		}
	}

	_, guildPriority, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get guild priority list: %v", err)
	}

	if request.Mode == evr.ModeArenaPublicAI {
		request.Mode = evr.ModeCombatPublic
		request.Level = evr.LevelUnspecified
		request.TeamIndex = int16(evr.TeamUnassigned)
		request.SessionSettings = evr.SessionSettings{
			AppID: request.SessionSettings.AppID,
			Mode:  int64(evr.ModeCombatPublic),
		}
	}

	features := ctx.Value(ctxFeaturesKey{}).([]string)

	return &EvrMatchState{
		Channel: &groupID,

		MatchID: request.CurrentMatch, // The existing lobby/match that the player is in (if any)
		Mode:    request.Mode,
		Level:   request.Level,
		Open:    true,

		SessionSettings: &request.SessionSettings,
		Broadcaster: MatchBroadcaster{
			VersionLock: request.VersionLock,
			Channels:    guildPriority,
			Features:    features,
		},
	}
	if len(request.Entrants) > 0 {
		ml.TeamIndex = TeamIndex(request.Entrants[0].TeamIndex)
	}
	return ml, nil
}

// lobbyFindSessionRequest is a message requesting to find a public session to join.
func (p *EvrPipeline) lobbyFindSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) (err error) {
	request := in.(*evr.LobbyFindSessionRequest)
	// Prepare the response message
	response := NewMatchmakingResult(session, logger, request.Mode, request.Channel)

	metricsTags := map[string]string{
		"mode":         request.Mode.String(),
		"channel":      request.Channel.String(),
		"level":        request.Level.String(),
		"player_count": strconv.FormatInt(int64(len(request.Entrants)), 10),
	}

	p.metrics.CustomCounter("lobbyfindsession_active_count", metricsTags, 1)

	loginSessionID := request.LoginSessionID
	groups, priorities, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		logger.Warn("Failed to get guild priority list", zap.Error(err))
	}

	// Validate that the channel is in the user's guilds
	if request.Channel == uuid.Nil || !lo.Contains(groups, request.Channel) {
		if len(priorities) > 0 {
			// If the channel is nil, use the players primary channel
			request.Channel = priorities[0]
		} else if len(groups) > 0 {
			// If the player has no guilds, use the first guild
			request.Channel = groups[0]
		} else {
			// If the player has no guilds
			return response.SendErrorToSession(status.Errorf(codes.PermissionDenied, "No guilds available"))
		}
	}

	// Build the matchmaking label using the request parameters
	ml, err := p.matchmakingLabelFromFindRequest(ctx, session, request)
	if err != nil {
		return response.SendErrorToSession(err)
	}

	metricsTags := map[string]string{
		"mode":     request.Mode.String(),
		"channel":  ml.Channel.String(),
		"level":    request.Level.String(),
		"team_idx": strconv.FormatInt(int64(request.TeamIndex), 10),
	}
	p.metrics.CustomCounter("lobbyfindsession_active_count", metricsTags, 1)
	loginSessionID := request.LoginSessionID

	// Check for suspensions on this channel, if this is a request for a public match.
	if err := p.authorizeMatchmaking(ctx, logger, session, loginSessionID, *ml.Channel, true); err != nil {
		switch status.Code(err) {
		case codes.Internal:
			logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
		default:
			return response.SendErrorToSession(session, err)
		}
	if err := p.authorizeMatchmaking(ctx, logger, session, loginSessionID, *ml.Channel); err != nil {
		return response.SendErrorToSession(err)
	}

	ml.Broadcaster.Regions = []evr.Symbol{evr.DefaultRegion}

	// Wait for a graceperiod Unless this is a social lobby, wait for a grace period before starting the matchmaker
	matchmakingDelay := MatchJoinGracePeriod
	if ml.Mode == evr.ModeSocialPublic {
		matchmakingDelay = 0
	}
	select {
	case <-time.After(matchmakingDelay):
	case <-ctx.Done():
		// The context was cancelled before the grace period was over
		return nil
	}

	go func() {
		// Create the matchmaking session
		err = p.MatchFind(ctx, logger, session, ml)
		if err != nil {
			response.SendErrorToSession(err)
		}
	}()

	return nil
}

func (p *EvrPipeline) MatchSpectateStreamLoop(session *sessionWS, msession *MatchmakingSession, skipDelay bool, create bool) error {
	logger := msession.Logger
	ctx := msession.Context()
	p.metrics.CustomCounter("spectatestream_active_count", msession.metricsTags(), 1)
	// Create a ticker to spectate
	spectateInterval := time.Duration(10) * time.Second

	limit := 100
	minSize := 3
	maxSize := MatchMaxSize - 1
	query := fmt.Sprintf("+label.open:T +label.lobby_type:public +label.mode:%s +label.size:>=2", msession.Label.Mode.Token())
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// list existing matches
		matches, err := listMatches(ctx, p, limit, minSize, maxSize, query)
		if err != nil {
			return msession.Cancel(fmt.Errorf("failed to find spectate match: %w", err))
		}

		if len(matches) != 0 {
			// sort matches by population
			sort.SliceStable(matches, func(i, j int) bool {
				// Sort by newer matches
				return matches[i].Size > matches[j].Size
			})

			// Found a backfill match
			foundMatch := FoundMatch{
				MatchID:   matches[0].GetMatchId(),
				Query:     query,
				TeamIndex: TeamIndex(evr.TeamSpectator),
			}
			select {
			case <-ctx.Done():
				return nil
			case msession.MatchJoinCh <- foundMatch:
				p.metrics.CustomCounter("spectatestream_found_count", msession.metricsTags(), 1)
				logger.Debug("Spectating match", zap.String("mid", foundMatch.MatchID))
			case <-time.After(3 * time.Second):
				p.metrics.CustomCounter("spectatestream_join_timeout_count", msession.metricsTags(), 1)
				logger.Warn("Failed to spectate match", zap.String("mid", foundMatch.MatchID))
			}
		}
		<-time.After(spectateInterval)
	}
}

func (p *EvrPipeline) MatchBackfillLoop(session *sessionWS, msession *MatchmakingSession, skipDelay bool, create bool, minCount int) error {
	interval := p.config.GetMatchmaker().IntervalSec
	idealMatchIntervals := p.config.GetMatchmaker().RevThreshold
	logger := msession.Logger
	ctx := msession.Context()
	// Wait for at least 1 interval before starting to look for a backfill.
	// This gives the matchmaker a chance to find a full ideal match
	backfillDelay := time.Duration(interval*idealMatchIntervals) * time.Second
	if skipDelay {
		backfillDelay = 0 * time.Second
	}

	// Check for a backfill match on a regular basis
	backfilInterval := time.Duration(10) * time.Second

	// Create a ticker to check for backfill matches
	backfillTicker := time.NewTimer(backfilInterval)
	backfillDelayTimer := time.NewTimer(backfillDelay)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-backfillDelayTimer.C:
		case <-backfillTicker.C:
		}

		msession.UpdatePlayerCounts()

		foundMatch := FoundMatch{}
		// Backfill any existing matches
		label, query, err := p.Backfill(ctx, session, msession, minCount)
		if err != nil {
			return msession.Cancel(fmt.Errorf("failed to find backfill match: %w", err))
		}

		if label != nil {
			// Found a backfill match
			foundMatch = FoundMatch{
				MatchID:   label.MatchID.String(),
				Query:     query,
				TeamIndex: TeamIndex(evr.TeamUnassigned),
			}
		} else if create {
			create = false
			// Start the create loop too
			go p.MatchCreateLoop(session, msession, 5*time.Minute)
		}

		if foundMatch.MatchID == "" {
			// No match found
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		logger.Debug("Attempting to backfill match", zap.String("mid", foundMatch.MatchID))
		p.metrics.CustomCounter("match_backfill_found_count", msession.metricsTags(), 1)

		if msession.Party != nil && !msession.Party.ID.IsNil() {
			// Send all the members of the party (that have matching labels) to the match
			msession.Party.Lock()
			for _, presence := range msession.Party.members.presences {
				if presence == nil {
					continue
				}

				if ms, found := p.matchmakingRegistry.GetMatchingBySessionId(uuid.FromStringOrNil(presence.Presence.GetSessionId())); found {
					select {
					case <-ms.Ctx.Done():
						continue
					default:
					}
					// Send the match to the session
					ms.Lock()
					ms.MatchJoinCh <- foundMatch
					ms.Unlock()
					p.metrics.CustomCounter("match_backfill_party_member_count", msession.metricsTags(), 1)
				}
			}

			msession.Party.Unlock()
		} else {
			msession.MatchJoinCh <- foundMatch
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
			p.metrics.CustomCounter("match_backfill_join_timeout_count", msession.metricsTags(), 1)
			logger.Warn("Failed to backfill match", zap.String("mid", foundMatch.MatchID))
		}
		// Continue to loop until the context is done

	}
}

func (p *EvrPipeline) MatchCreateLoop(session *sessionWS, msession *MatchmakingSession, pruneDelay time.Duration) error {
	ctx := msession.Context()
	logger := msession.Logger
	// set a timeout
	//stageTimer := time.NewTimer(pruneDelay)
	p.metrics.CustomCounter("match_create_active_count", msession.metricsTags(), 1)
	for {

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		// Stage 1: Check if there is an available broadcaster
		matchID, err := p.MatchCreate(ctx, session, msession, msession.Label)

		switch status.Code(err) {

		case codes.OK:
			if matchID == "" {
				return msession.Cancel(fmt.Errorf("match is nil"))
			}
			foundMatch := FoundMatch{
				MatchID:   matchID,
				Query:     "",
				TeamIndex: TeamIndex(evr.TeamUnassigned),
			}
			select {
			case <-ctx.Done():
				return nil
			case msession.MatchJoinCh <- foundMatch:
				p.metrics.CustomCounter("match_create_join_active_count", msession.metricsTags(), 1)
				logger.Debug("Joining match", zap.String("mid", foundMatch.MatchID))
			case <-time.After(3 * time.Second):
				p.metrics.CustomCounter("make_create_create_join_timeout_count", msession.metricsTags(), 1)
				msession.Cancel(fmt.Errorf("failed to join match"))
			}
			// Keep trying until the context is done
		case codes.NotFound, codes.ResourceExhausted, codes.Unavailable:
			p.metrics.CustomCounter("create_unavailable_count", msession.metricsTags(), 1)
		default:
			return msession.Cancel(err)
		}
		<-time.After(10 * time.Second)
	}
}

func (p *EvrPipeline) MatchFind(parentCtx context.Context, logger *zap.Logger, session *sessionWS, ml *EvrMatchState) error {
	response := NewMatchmakingResult(session, logger, ml.Mode, *ml.Channel)
	if s, found := p.matchmakingRegistry.GetMatchingBySessionId(session.id); found {
		// Replace the session
		logger.Warn("Matchmaking session already exists", zap.Any("tickets", s.Tickets))
	}
	joinFn := func(matchID string, query string) error {
		err := p.JoinEvrMatch(parentCtx, logger, session, query, matchID, int(ml.TeamIndex))
		if err != nil {
			return response.SendErrorToSession(err)
		}
		return nil
	}
	errorFn := func(err error) error {
		return response.SendErrorToSession(err)
	}

	// Create a new matching session
	logger.Debug("Creating a new matchmaking session")

	timeout := 60 * time.Minute

	if ml.TeamIndex == TeamIndex(evr.TeamSpectator) {
		timeout = 12 * time.Hour
	}

	joinParty := true
	msession, err := p.matchmakingRegistry.Create(parentCtx, logger, session, ml, timeout, errorFn, joinFn, joinParty)
	if err != nil {
		logger.Error("Failed to create matchmaking session", zap.Error(err))
		return err
	}
	p.metrics.CustomCounter("match_find_active_count", msession.metricsTags(), 1)
	// Load the user's matchmaking config

	matchToken := msession.UserSettings.NextMatchToken.String()

	if matchToken != "" {
		logger.Debug("Attempting to join match from settings", zap.String("mid", matchToken))
		msession.UserSettings.NextMatchToken = ""
		err = p.matchmakingRegistry.StoreMatchmakingSettings(msession.Ctx, session.logger, msession.UserSettings, session.userID.String())
		if err != nil {
			logger.Error("Failed to save matchmaking config", zap.Error(err))
		}
		match, _, err := p.matchRegistry.GetMatch(msession.Ctx, matchToken)
		if err != nil {
			logger.Error("Failed to get match", zap.Error(err))
		} else {
			if match == nil {
				logger.Warn("Match not found", zap.String("mid", matchToken))
			} else {
				p.metrics.CustomCounter("match_next_join_count", map[string]string{}, 1)
				// Join the match
				msession.MatchJoinCh <- FoundMatch{
					MatchID:   matchToken,
					Query:     "",
					TeamIndex: TeamIndex(evr.TeamUnassigned),
				}
				<-time.After(3 * time.Second)
				select {
				case <-msession.Ctx.Done():
					return nil
				default:
				}
			}
		}
	}
	logger = msession.Logger

	skipBackfillDelay := false
	if ml.TeamIndex == TeamIndex(evr.TeamModerator) {
		skipBackfillDelay = true
		// Check that the user is a moderator for this channel, or globally
		guild, err := p.discordRegistry.GetGuildByGroupId(parentCtx, ml.Channel.String())
		if err != nil || guild == nil {
			logger.Warn("failed to get guild: %v", zap.Error(err))
			ml.TeamIndex = TeamIndex(evr.TeamSpectator)
		} else {
			discordID, err := p.discordRegistry.GetDiscordIdByUserId(parentCtx, session.userID)
			if err != nil {
				return fmt.Errorf("failed to get discord id: %v", err)
			}
			if ok, _, err := p.discordRegistry.isModerator(parentCtx, guild.ID, discordID); err != nil || !ok {
				ml.TeamIndex = TeamIndex(evr.TeamSpectator)
			}
		}
	}
	if ml.TeamIndex == TeamIndex(evr.TeamSpectator) {
		skipBackfillDelay = true
		if ml.Mode != evr.ModeArenaPublic && ml.Mode != evr.ModeCombatPublic {
			return fmt.Errorf("spectators are only allowed in arena and combat matches")
		}
		// Spectators don't matchmake, and they don't have a delay for backfill.
		// Spectators also don't time out.
		go p.MatchSpectateStreamLoop(session, msession, skipBackfillDelay, false)
		return nil
	}

	validModes := []evr.Symbol{
		evr.ModeArenaPublic,
		evr.ModeCombatPublic,
		evr.ModeSocialPublic,
	}

	if !lo.Contains(validModes, ml.Mode) {
		return fmt.Errorf("invalid mode for match find")
	}

	go func() {

		// This is in a go function because there are timers that need to be waited on
		// Get the user into a match first (the matchID will be nil)

		if msession.Party != nil && !msession.Party.ID.IsNil() && ml.MatchID != uuid.Nil && msession.Party.leader != nil {
			ph := msession.Party
			ph.RLock()
			leader := ph.leader.UserPresence
			ph.RUnlock()
			if leader == nil {
				msession.Cancel(status.Errorf(codes.Internal, "Party leader not found"))
			}

			// If this user is not the party leader, join the party leader, or return and wait for the party leader to matchmake.
			if leader.GetUserId() != session.UserID().String() {
				p.MatchmakeWithParty(msession, ph)
				return
			}

			// This is the leader, wait for party members before starting match making
			_, err := p.WaitForPartyMembers(msession.Context(), logger, msession)
			if err != nil {
				msession.Cancel(err)
				return
			}

		}

		skipBackfillDelay = false
		create := false
		matchMake := true
		if ml.Mode == evr.ModeSocialPublic || ml.Mode == evr.ModeCombatPublic {
			skipBackfillDelay = true
		}

		if ml.Mode == evr.ModeSocialPublic {
			create = true
			matchMake = false
		}
		// Start the backfill loop
		go p.MatchBackfillLoop(session, msession, skipBackfillDelay, create)

		// Let the backfill complete, otherwise the client will ignore a second ping request.
		select {
		case <-msession.Ctx.Done():
			// Match already found/created (commonly a social lobby)
			return
		case <-time.After(5 * time.Second):
		}
		if matchMake {
			// Put a ticket in for matching
			err := p.MatchMake(session, msession)
			if err != nil {
				msession.Cancel(err)
			}
		}
	}()

	return nil
}

// lobbyPingResponse is a message responding to a ping request.
func (p *EvrPipeline) lobbyPingResponse(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	response := in.(*evr.LobbyPingResponse)

	userID := session.userID
	// Validate the connection.
	if userID == uuid.Nil {
		return fmt.Errorf("session not authenticated")
	}

	// Look up the matching session.
	msession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.id)
	if !ok {
		return fmt.Errorf("matching session not found")
	}
	select {
	case <-msession.Ctx.Done():
		return nil
	case msession.PingResultsCh <- response.Results:
	case <-time.After(time.Second * 2):
		logger.Debug("Failed to send ping results")
	}

	return nil
}

func (p *EvrPipeline) GetGuildPriorityList(ctx context.Context, userID uuid.UUID) (all []uuid.UUID, selected []uuid.UUID, err error) {

	currentGroupID, ok := ctx.Value(ctxGroupIDKey{}).(uuid.UUID)
	if !ok {
		return nil, nil, status.Errorf(codes.InvalidArgument, "Failed to get group ID from context")
	}

	// Get the guild priority from the context
	memberships, err := p.discordRegistry.GetGuildGroupMemberships(ctx, userID, nil)
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "Failed to get guilds: %v", err)
	}

	// Sort the groups by size descending
	sort.Slice(memberships, func(i, j int) bool {
		return memberships[i].GuildGroup.Size() > memberships[j].GuildGroup.Size()
	})

	groupIDs := make([]uuid.UUID, 0)
	for _, group := range memberships {
		groupIDs = append(groupIDs, group.GuildGroup.ID())
	}

	guildPriority := make([]uuid.UUID, 0)
	params, ok := ctx.Value(ctxUrlParamsKey{}).(map[string][]string)
	if ok {
		// If the params are set, use them
		for _, gid := range params["guilds"] {
			for _, guildId := range strings.Split(gid, ",") {
				s := strings.Trim(guildId, " ")
				if s != "" {
					// Get the groupId for the guild
					groupIDstr, found := p.discordRegistry.Get(s)
					if !found {
						continue
					}
					groupID := uuid.FromStringOrNil(groupIDstr)
					if groupID != uuid.Nil && lo.Contains(groupIDs, groupID) {
						guildPriority = append(guildPriority, groupID)
					}
				}
			}
		}
	}

	if len(guildPriority) == 0 {
		// If the params are not set, use the user's guilds
		guildPriority = []uuid.UUID{currentGroupID}
		for _, groupID := range groupIDs {
			if groupID != currentGroupID {
				guildPriority = append(guildPriority, groupID)
			}
		}
	}

	p.logger.Debug("guild priorites", zap.Any("prioritized", guildPriority), zap.Any("all", groupIDs))
	return groupIDs, guildPriority, nil
}

// lobbyCreateSessionRequest is a request to create a new session.
func (p *EvrPipeline) lobbyCreateSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.LobbyCreateSessionRequest)
	response := NewMatchmakingResult(logger, request.Mode, request.Channel)

	features := ctx.Value(ctxFeaturesKey{}).([]string)
	requiredFeatures := ctx.Value(ctxRequiredFeaturesKey{}).([]string)

	// Get the GroupID from the context
	groupID := request.Channel
	if groupID != uuid.Nil {
		groups, _, err := p.runtimeModule.UserGroupsList(ctx, session.userID.String(), 200, nil, "")
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get user groups: %v", err)
		}

		groupStr := groupID.String()
		isMember := lo.ContainsBy(groups, func(g *api.UserGroupList_UserGroup) bool {
			return g.Group.Id == groupStr && g.State.GetValue() <= int32(api.UserGroupList_UserGroup_MEMBER)
		})
		if !isMember {
			groupID = uuid.Nil
		}
	}

	if groupID == uuid.Nil {
		var ok bool
		groupID, ok = ctx.Value(ctxGroupIDKey{}).(uuid.UUID)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "Failed to get group ID from context")
		}
	}

	metricsTags := map[string]string{
		"type":     strconv.FormatInt(int64(request.LobbyType), 10),
		"mode":     request.Mode.String(),
		"channel":  request.Channel.String(),
		"level":    request.Level.String(),
		"team_idx": strconv.FormatInt(int64(request.TeamIndex), 10),
	}

	// Add the features to teh metrics tags as feature_<featurename>
	for _, feature := range features {
		metricsTags[fmt.Sprintf("feature_%s", feature)] = "1"
	}

	p.metrics.CustomCounter("lobbycreatesession_active_count", metricsTags, 1)
	loginSessionID := request.LoginSessionID
	groups, priorities, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		logger.Warn("Failed to get guild priority list", zap.Error(err))
	}

	response := NewMatchmakingResult(session, logger, request.Mode, request.Channel)

	// Validate that the channel is in the user's guilds
	if request.Channel == uuid.Nil || !lo.Contains(groups, request.Channel) {
		if len(priorities) > 0 {
			// If the channel is nil, use the players primary channel
			request.Channel = priorities[0]
		} else if len(groups) > 0 {
			// If the player has no guilds, use the first guild
			request.Channel = groups[0]
		} else {
			// If the player has no guilds,
			return response.SendErrorToSession(status.Errorf(codes.PermissionDenied, "No guilds available"))
		}

	}

	// Check for membership and suspensions on this channel. The user will not be allowed to create lobby's
	if err := p.authorizeMatchmaking(ctx, logger, session, loginSessionID, groupID, true); err != nil {
		switch status.Code(err) {
		case codes.Internal:
			logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
		default:
			return response.SendErrorToSession(session, err)
		}
	}
	// Validating the level against the game mode
	validLevels := map[evr.Symbol][]evr.Symbol{
		evr.ModeArenaPublic:          {evr.LevelArena},
		evr.ModeArenaPrivate:         {evr.LevelArena},
		evr.ModeArenaTournment:       {evr.LevelArena},
		evr.ModeArenaPublicAI:        {evr.LevelArena},
		evr.ModeArenaTutorial:        {evr.LevelArena},
		evr.ModeSocialPublic:         {evr.LevelSocial},
		evr.ModeSocialPrivate:        {evr.LevelSocial},
		evr.ModeSocialNPE:            {evr.LevelSocial},
		evr.ModeCombatPublic:         {evr.LevelCombustion, evr.LevelDyson, evr.LevelFission, evr.LevelGauss},
		evr.ModeCombatPrivate:        {evr.LevelCombustion, evr.LevelDyson, evr.LevelFission, evr.LevelGauss},
		evr.ModeEchoCombatTournament: {evr.LevelCombustion, evr.LevelDyson, evr.LevelFission, evr.LevelGauss},
	}

	flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags)
	if !ok {
		flags = SessionFlags{}
	}

	if !flags.IsDeveloper {
		if levels, ok := validLevels[request.Mode]; ok {
			if request.Level != evr.LevelUnspecified && !lo.Contains(levels, request.Level) {
				return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Invalid level %v for game mode %v", request.Level, request.Mode))
			}
		} else {
			return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Failed to create matchmaking session: Tried to create a match with an unknown level or gamemode: %v", request.Mode))
		}
	}
	regions := make([]evr.Symbol, 0)
	if request.Region != evr.DefaultRegion {
		regions = append(regions, request.Region)
	}
	regions = append(regions, evr.DefaultRegion)

	// Make the regions unique without resorting it
	uniqueRegions := make([]evr.Symbol, 0, len(regions))
	seen := make(map[evr.Symbol]struct{}, len(regions))
	for _, region := range regions {
		if _, ok := seen[region]; !ok {
			uniqueRegions = append(uniqueRegions, region)
			seen[region] = struct{}{}
	regions := make([]evr.Symbol, 0)
	if request.Region != evr.DefaultRegion {
		regions = append(regions, request.Region)
	}
	regions = append(regions, evr.DefaultRegion)

	// Make the regions unique without resorting it
	uniqueRegions := make([]evr.Symbol, 0, len(regions))
	seen := make(map[evr.Symbol]struct{}, len(regions))
	for _, region := range regions {
		if _, ok := seen[region]; !ok {
			uniqueRegions = append(uniqueRegions, region)
			seen[region] = struct{}{}
		}
	}

	regions := make([]evr.Symbol, 0)
	if request.Region != evr.DefaultRegion && request.Region != 0xffffffffffffffff {
		regions = append(regions, request.Region)
	}
	regions = append(regions, evr.DefaultRegion)

	// Make the regions unique without resorting it
	uniqueRegions := make([]evr.Symbol, 0, len(regions))
	seen := make(map[evr.Symbol]struct{}, len(regions))
	for _, region := range regions {
		if _, ok := seen[region]; !ok {
			uniqueRegions = append(uniqueRegions, region)
			seen[region] = struct{}{}
		}
	}

	_, priorities, err := p.GetGuildPriorityList(ctx, session.userID)
	if err != nil {
		logger.Warn("Failed to get guild priority list", zap.Error(err))
	}

	ml := &EvrMatchState{
		Level:            request.Level,
		LobbyType:        LobbyType(request.LobbyType),
		Mode:             request.Mode,
		Open:             true,
		SessionSettings:  &request.SessionSettings,
		TeamIndex:        TeamIndex(request.TeamIndex),
		Channel:          &request.Channel,
		RequiredFeatures: requiredFeatures,
		Broadcaster: MatchBroadcaster{
			VersionLock: uint64(request.VersionLock),
			Regions:     uniqueRegions,
			Channels:    priorities,
			Features:    features,
		},
	}

	// Start the search in a goroutine.
	go func() error {

		joinFn := func(matchID string, query string) error {
			logger := logger.With(zap.String("mid", matchID))

			err := p.JoinEvrMatch(ctx, logger, session, query, matchID, int(ml.TeamIndex))
			switch status.Code(err) {
			case codes.ResourceExhausted:
				logger.Warn("Failed to join match (retrying): ", zap.Error(err))
				return nil
			default:
				return err
			}
		}

		errorFn := func(err error) error {
			return NewMatchmakingResult(session, logger, ml.Mode, *ml.Channel).SendErrorToSession(err)
		}

		// Create a matching session
		timeout := 15 * time.Minute
		msession, err := p.matchmakingRegistry.Create(ctx, logger, session, ml, timeout, errorFn, joinFn, false)
		if err != nil {
			return response.SendErrorToSession(status.Errorf(codes.Internal, "Failed to create matchmaking session: %v", err))
		}
		p.metrics.CustomCounter("create_active_count", map[string]string{}, 1)
		err = p.MatchCreateLoop(session, msession, 5*time.Minute)
		if err != nil {
			return response.SendErrorToSession(err)
		}
		return nil
	}()

	return nil
}

// lobbyJoinSessionRequest is a request to join a specific existing session.
func (p *EvrPipeline) lobbyJoinSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.LobbyJoinSessionRequest)
	response := NewMatchmakingResult(session, logger, 0xFFFFFFFFFFFFFFFF, request.MatchID)

	loginSessionID := request.LoginSessionID
	// Make sure the match exists
	matchToken, err := NewMatchID(request.MatchID, p.node)
	if err != nil {
		return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Invalid match ID"))
	}
	match, _, err := p.matchRegistry.GetMatch(ctx, matchToken.String())
	if err != nil || match == nil {
		return response.SendErrorToSession(status.Errorf(codes.NotFound, "Match not found"))
	}

	groups, err := p.discordRegistry.GetGuildGroups(ctx, session.userID)
	if err != nil {
		return response.SendErrorToSession(status.Errorf(codes.Internal, "Failed to get guild groups: %v", err))
	}
	groupIDs := lo.Map(groups, func(group *api.Group, _ int) uuid.UUID { return uuid.FromStringOrNil(group.Id) })

	// Extract the label
	ml := &EvrMatchState{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), ml); err != nil {
		return response.SendErrorToSession(status.Errorf(codes.NotFound, err.Error()))
	}
	if ml.Channel == nil {
		ml.Channel = &uuid.Nil
	}

	if lo.Contains(ml.Broadcaster.Tags, "membersonly") {
		// Check if the user is a member of the guild
		if !lo.Contains(groupIDs, *ml.Channel) {
			return response.SendErrorToSession(status.Errorf(codes.PermissionDenied, "Match is members only"))
		}
	}

	metricsTags := map[string]string{
		"team":    TeamIndex(request.TeamIndex).String(),
		"mode":    ml.Mode.String(),
		"channel": ml.Channel.String(),
		"level":   ml.Level.String(),
	}
	p.metrics.CustomCounter("lobbyjoinsession_active_count", metricsTags, 1)

	switch {

	case ml.LobbyType == UnassignedLobby:
		return response.SendErrorToSession(status.Errorf(codes.NotFound, "Match is not a lobby"))
	case !ml.Open:
		return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Match is not open"))
	case int(match.GetSize()) >= MatchMaxSize:
		return response.SendErrorToSession(status.Errorf(codes.ResourceExhausted, "Match is full"))
	case ml.LobbyType == PublicLobby:

		// Check if this player is a global moderator or developer
		isModerator, _ := checkIfGlobalModerator(ctx, p.runtimeModule, session.userID)
		isDeveloper, _ := checkIfGlobalDeveloper(ctx, p.runtimeModule, session.userID)

		// Let developers and moderators join public matches
		if request.TeamIndex != int16(Spectator) && !isDeveloper && !isModerator && time.Since(ml.StartedAt) < time.Second*15 {
			// Allow if the match is over 15 seconds old, to allow matchmaking to properly populate the match
			err = status.Errorf(codes.InvalidArgument, "Match is a newly started public match")
		}
	}

	if err != nil {
		return response.SendErrorToSession(session, err)
	}

	// Ensure the client has the required features
	if len(ml.RequiredFeatures) > 0 {
		features, ok := ctx.Value(ctxFeaturesKey{}).([]string)
		if !ok {
			features = make([]string, 0)
		}
		for _, f := range ml.RequiredFeatures {
			if !lo.Contains(features, f) {
				return response.SendErrorToSession(session, status.Errorf(codes.FailedPrecondition, "Missing required feature: %v", f))
			}
		}
	}
	if err := p.authorizeMatchmaking(ctx, logger, session, loginSessionID, *ml.Channel, false); err != nil {
		switch status.Code(err) {
		case codes.Internal:
			logger.Warn("Failed to authorize matchmaking, allowing player to continue. ", zap.Error(err))
		case codes.NotFound:
			// Allow the player to join the match even though they are not a member.
		default:
			return response.SendErrorToSession(session, err)
		}
	}

	if err = p.JoinEvrMatch(ctx, logger, session, "", matchToken.String(), int(request.TeamIndex)); err != nil {
		return response.SendErrorToSession(status.Errorf(codes.NotFound, err.Error()))
	}
	return nil
}

// LobbyPendingSessionCancel is a message from the server to the client, indicating that the user wishes to cancel matchmaking.
func (p *EvrPipeline) lobbyPendingSessionCancel(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	// Look up the matching session.
	if matchingSession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.id); ok {
		matchingSession.Cancel(ErrMatchmakingCanceledByPlayer)
	}
	return nil
}

// pruneMatches prunes matches that are underutilized
func (p *EvrPipeline) pruneMatches(ctx context.Context, session *sessionWS) error {
	session.logger.Warn("Pruning matches")
	matches, err := p.matchmakingRegistry.listMatches(ctx, 1000, 2, 3, "*")
	if err != nil {
		return err
	}
	signal := SignalData{
		OpCode: SignalCodePruneUnderutilized,
	}

	for _, match := range matches {
		_, err := SignalMatch(ctx, p.matchRegistry, match.MatchId, signal)
		if err != nil {
			return err

		}
	}
	return nil
}
