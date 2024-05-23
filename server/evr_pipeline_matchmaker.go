package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func matchDefaults() {

	type selector struct {
		VersionLock uint64
		Mode        evr.Symbol

		Channel         uuid.UUID
		SessionSettings evr.SessionSettings
	}

}

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

func EnforceSingleMatch(logger *zap.Logger, session *sessionWS) {
	evrID, ok := session.Context().Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		logger.Warn("Failed to get evrID from context")
		return
	}

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



func (p *EvrPipeline) validateLobbyFindSessionRequest(ctx context.Context, request *evr.LobbyFindSessionRequest) error {

	flags := ctx.Value(ctxFlagsKey{}).(SessionFlags)
	channels := ctx.Value(ctxChannelsKey{}).([]ChannelMember)

	// Verify that the FindSession request is valid
	validModes := []evr.Symbol{
		evr.ModeArenaPublic,
		evr.ModeCombatPublic,
		evr.ModeSocialPublic,
	}

	if !lo.Contains(validModes, request.Mode) {
		return status.Errorf(codes.InvalidArgument, "Invalid mode for match find")
	}

	// Eshure at least one Entrant is provided
	if len(request.Entrants) == 0 {
		return status.Errorf(codes.InvalidArgument, "No entrants provided")
	}

	if !flags.IsDeveloper {
		return nil
	}

	// Ensure that the team index is valid
	if request.Entrants[0].Alignment < -1 || request.Entrants[0].Alignment > 4 {
		return status.Errorf(codes.InvalidArgument, "Invalid team index")
	}

	// Ensure that at least one channel is valid
	var found bool
	for _, channel := range channels {
		// Move the channel to the end of the list
		if channel.ChannelID == request.Channel {
			if channel.isSuspended || channel.ExcludeFromMatchmaking {
				// Move it to the bottom of the list back of the list
				channels = append(channels[:0], channels[1:]...)
				channels = append(channels, channel)
			}
		}
		found = true
		break
	}
	if !found {
		return status.Errorf(codes.InvalidArgument, "No valid channels found.")
	}

	if channels[0].isSuspended {
		return status.Errorf(codes.PermissionDenied, "Channel is suspended")
	}
	if channels[0].ExcludeFromMatchmaking {
		// Try the next channel
		return status.Errorf(codes.PermissionDenied, "Channel is excluded from matchmaking")
	}

	if request.Mode == evr.ModeSocialPublic && request.Entrants[0].Alignment == int16(evr.SpectatorRole) {
		return status.Errorf(codes.InvalidArgument, "Spectators are only allowed in arena and combat matches")
	}
	return nil
}

// lobbyFindSessionRequest is a message requesting to find a public session to join.
func (p *EvrPipeline) lobbyFindSessionRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) (err error) {
	request := in.(*evr.LobbyFindSessionRequest)

	response := NewMatchmakingResult(session, logger, request.Mode, request.Channel)

	err = p.validateLobbyFindSessionRequest(ctx, request)
	if err != nil {
		return response.SendErrorToSession(err)
	}
	logger = logger.With(zap.String("channel", request.Channel.String())).With(zap.String("mode", request.Mode.String())).With(zap.String("level", request.Level.String())).With(zap.String("team_idx", TeamAlignment(request.Entrants[0].Alignment).String()))

	memberships := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	channels := lo.Map(memberships, func(m ChannelMember, _ int) uuid.UUID { return m.ChannelID })

	criteria := MatchCriteria{
		LobbyType:      evr.PublicLobby,
		VersionLock:    request.VersionLock,
		Mode:           request.Mode,
		Channels:       channels,
		Level:          request.Level,
		CurrentMatchID: MatchID{request.CurrentMatch, p.node},
		Alignment:      evr.UnassignedRole,
	}

	if len(request.Entrants) > 0 {
		criteria.Alignment = evr.Role(request.Entrants[0].Alignment)
	}

	presence := NewPlayerPresence(session, request.VersionLock, criteria.Alignment)

	metricsTags := map[string]string{
		"version_lock": criteria.VersionLock.String(),
		"type":         criteria.LobbyType.String(),
		"mode":         criteria.Mode.String(),
		"level":        criteria.Level.String(),
		"channel":      criteria.Channels[0].String(),
		"alignment":    presence.Alignment.String(),
	}

	p.metrics.CustomCounter("lobbyfindsession_active_count", metricsTags, 1)

	// Wait for a graceperiod Unless this is a social lobby
	matchmakingDelay := MatchJoinGracePeriod
	if criteria.Mode == evr.ModeSocialPublic {
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
		err = p.MatchFind(ctx, logger, session, presence, criteria, metricsTags)
		if err != nil {
			response.SendErrorToSession(err)
		}
	}()

	return nil
}

func (p *EvrPipeline) MatchSpectateStreamLoop(session *sessionWS, msession *MatchmakingSession) error {
	logger := msession.Logger
	ctx := msession.Context()
	p.metrics.CustomCounter("spectatestream_active_count", msession.metricsTags, 1)
	// Create a ticker to spectate
	spectateInterval := time.Duration(10) * time.Second

	limit := 100
	minSize := 3
	maxSize := MatchMaxSize - 1
	query := fmt.Sprintf("+label.open:T +label.lobby_type:public +label.mode:%s +label.size:>=2", msession.MatchCriteria.Mode.Token())
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
			MatchID:   MatchIDFromStringOrNil(matches[0].GetMatchId()),
			Alignment: TeamSpectator,
		}
		select {
		case <-ctx.Done():
			return nil
		case msession.MatchJoinCh <- foundMatch:
			p.metrics.CustomCounter("spectatestream_found_count", msession.metricsTags, 1)
			logger.Debug("Spectating match", zap.String("mid", foundMatch.MatchID.String()))
		case <-time.After(3 * time.Second):
			p.metrics.CustomCounter("spectatestream_join_timeout_count", msession.metricsTags, 1)
			logger.Warn("Failed to spectate match", zap.String("mid", foundMatch.MatchID.String()))
		}
		<-time.After(spectateInterval)
	}
}

func (p *EvrPipeline) MatchBackfillLoop(msession *MatchmakingSession) error {
	logger := msession.Logger
	ctx := msession.Context()

	// Check for a backfill match on a regular basis
	backfilInterval := time.Duration(10) * time.Second
	backfillDelayTimer := time.NewTimer(5 * time.Second)
	// Create a ticker to check for backfill matches
	backfillTicker := time.NewTimer(backfilInterval)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-backfillTicker.C:
		case <-backfillDelayTimer.C:
		}

		foundMatch := FoundMatch{}
		// Backfill any existing matches
		label, _, err := p.Backfill(ctx, msession)
		if err != nil {
			return msession.Cancel(fmt.Errorf("failed to find backfill match: %w", err))
		}

		if label != nil {
			// Found a backfill match
			foundMatch = FoundMatch{
				MatchID:   label.ID,
				Alignment: TeamAlignment(evr.UnassignedRole),
			}
		}
		if foundMatch.MatchID.IsNil() {
			// No match found
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		logger.Debug("Attempting to backfill match", zap.String("mid", foundMatch.MatchID.String()))
		p.metrics.CustomCounter("match_backfill_found_count", msession.metricsTags, 1)

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
					p.metrics.CustomCounter("match_backfill_party_member_count", msession.metricsTags, 1)
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
			p.metrics.CustomCounter("match_backfill_join_timeout_count", msession.metricsTags, 1)
			logger.Warn("Failed to backfill match", zap.String("mid", foundMatch.MatchID.String()))
		}
		// Continue to loop until the context is done

	}
}

func (p *EvrPipeline) MatchCreateLoop(msession *MatchmakingSession, pruneDelay time.Duration) error {
	ctx := msession.Context()
	logger := msession.Logger
	// set a timeout
	//stageTimer := time.NewTimer(pruneDelay)
	p.metrics.CustomCounter("match_create_active_count", msession.metricsTags, 1)
	for {

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		matchSettings := NewMatchSettingsFromMode(msession.MatchCriteria.Mode, msession.MatchCriteria.VersionLock)
		// Stage 1: Check if there is an available broadcaster
		matchID, err := p.MatchCreate(ctx, msession, matchSettings)

		switch status.Code(err) {

		case codes.OK:
			if matchID.IsNil() {
				return msession.Cancel(fmt.Errorf("match is nil"))
			}
			foundMatch := FoundMatch{
				MatchID:   matchID,
				Alignment: TeamAlignment(evr.UnassignedRole),
			}
			select {
			case <-ctx.Done():
				return nil
			case msession.MatchJoinCh <- foundMatch:
				p.metrics.CustomCounter("match_create_join_active_count", msession.metricsTags, 1)
				logger.Debug("Joining match", zap.String("mid", foundMatch.MatchID.String()))
			case <-time.After(3 * time.Second):
				p.metrics.CustomCounter("make_create_create_join_timeout_count", msession.metricsTags, 1)
				msession.Cancel(fmt.Errorf("failed to join match"))
			}
			// Keep trying until the context is done
		case codes.NotFound, codes.ResourceExhausted, codes.Unavailable:
			p.metrics.CustomCounter("create_unavailable_count", msession.metricsTags, 1)
		default:
			return msession.Cancel(err)
		}
		<-time.After(10 * time.Second)
	}
}

func (p *EvrPipeline) MatchFind(parentCtx context.Context, logger *zap.Logger, session *sessionWS, presence PlayerPresence, criteria MatchCriteria, metricsTags map[string]string) error {
	response := NewMatchmakingResult(session, logger, criteria.Mode, presence.Memberships[0].ChannelID)
	if s, found := p.matchmakingRegistry.GetMatchingBySessionId(session.id); found {
		// Replace the session
		logger.Warn("Matchmaking session already exists", zap.Any("tickets", s.Tickets))
	}
	joinFn := func(matchID MatchID) error {
		err := p.JoinEvrMatch(parentCtx, logger, session, matchID, criteria.Alignment)
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

	if presence.Alignment == evr.SpectatorRole {
		timeout = 12 * time.Hour
	}

	matchSettings := NewMatchSettingsFromMode(criteria.Mode, criteria.VersionLock)
	joinParty := true
	msession, err := p.matchmakingRegistry.Create(parentCtx, logger, session, presence, matchSettings, timeout, errorFn, joinFn, joinParty)
	if err != nil {
		logger.Error("Failed to create matchmaking session", zap.Error(err))
		return err
	}
	p.metrics.CustomCounter("match_find_active_count", msession.metricsTags, 1)
	// Load the user's matchmaking config

	matchID := msession.UserSettings.NextMatchID

	if matchID.IsNil() {
		logger.Debug("Attempting to join match from settings", zap.String("mid", matchID.String()))
		msession.UserSettings.NextMatchID = MatchID{}
		err = StoreUserMatchmakingSettings(msession.Ctx, p.runtimeModule, session.userID.String(), msession.UserSettings)
		if err != nil {
			logger.Error("Failed to save matchmaking config", zap.Error(err))
		}
		match, _, err := p.matchRegistry.GetMatch(msession.Ctx, matchID.String())
		if err != nil {
			logger.Error("Failed to get match", zap.Error(err))
		} else {
			if match == nil {
				logger.Warn("Match not found", zap.String("mid", matchID.String()))
			} else {
				p.metrics.CustomCounter("match_next_join_count", map[string]string{}, 1)
				// Join the match
				msession.MatchJoinCh <- FoundMatch{
					MatchID:   matchID,
					Alignment: TeamAlignment(evr.UnassignedRole),
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

	flags := parentCtx.Value(ctxFlagsKey{}).(SessionFlags)

	channels := parentCtx.Value(ctxChannelsKey{}).([]ChannelMember)

	var thisChannel ChannelMember
	for _, channel := range channels {
		if channel.ChannelID == criteria.Channels[0] {
			thisChannel = channel
			break
		}
	}

	if !flags.IsDeveloper {

		validModes := []evr.Symbol{
			evr.ModeArenaPublic,
			evr.ModeCombatPublic,
			evr.ModeSocialPublic,
		}

		if !lo.Contains(validModes, criteria.Mode) {
			return fmt.Errorf("invalid mode for match find")
		}

		if criteria.Mode == evr.ModeSocialPublic && criteria.Alignment == evr.SpectatorRole {
			return fmt.Errorf("spectators are only allowed in arena and combat matches")
		}

		if criteria.Alignment == evr.ModeratorRole && !flags.IsModerator && !thisChannel.isModerator {
			return fmt.Errorf("not a moderator")
		}
	}

	if criteria.Alignment == evr.SpectatorRole {
		// Spectators don't matchmake, and they don't have a delay for backfill.
		// Spectators also don't time out.
		go p.MatchSpectateStreamLoop(session, msession)
		return nil
	}

	go func() {

		// This is in a go function because there are timers that need to be waited on
		// Get the user into a match first (the matchID will be nil)

		if msession.Party != nil && !msession.Party.ID.IsNil() && criteria.CurrentMatchID.IsNotNil() && msession.Party.leader != nil {
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

		// Start the backfill loop
		go p.MatchBackfillLoop(msession)

		if criteria.Mode == evr.ModeSocialPublic {
			go func() {
				<-time.After(5 * time.Second)
				select {
				case <-msession.Ctx.Done():
					return
				default:
				}
				p.MatchCreateLoop(msession, 5*time.Minute)
			}()
		} else {

			// Let the backfill complete, otherwise the client will ignore a second ping request.
			select {
			case <-msession.Ctx.Done():
				// Match already found/created (commonly for a social lobby)
				return
			case <-time.After(5 * time.Second):
			}

			// Put a ticket in for matching
			err := p.MatchMake(msession)
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
		"type":     LobbyType(request.LobbyType).String(),
		"mode":     request.Mode.String(),
		"channel":  request.Channel.String(),
		"level":    request.Level.String(),
		"team_idx": TeamAlignment(request.TeamIndex).String(),
	}
	p.metrics.CustomCounter("lobbycreatesession_count", metricsTags, 1)

	response := NewMatchmakingResult(session, logger, request.Mode, request.Channel)

	channels := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	for _, channel := range channels {
		if channel.isSuspended || channel.ExcludeFromMatchmaking {
			continue
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

	presence := NewPlayerPresence(session, request.VersionLock, evr.Role(request.TeamIndex))
	matchSettings := NewMatchSettingsFromMode(request.Mode, request.VersionLock)

	// Start the search in a goroutine.
	go func() error {

		joinFn := func(matchID MatchID) error {
			logger := logger.With(zap.String("mid", matchID.String()))

			err := p.JoinEvrMatch(ctx, logger, session, matchID, evr.Role(request.TeamIndex))
			switch status.Code(err) {
			case codes.ResourceExhausted:
				logger.Warn("Failed to join match (retrying): ", zap.Error(err))
				return nil
			default:
				return err
			}
		}

		errorFn := func(err error) error {
			return NewMatchmakingResult(session, logger, matchSettings.Mode, presence.Memberships[0].ChannelID).SendErrorToSession(err)
		}

		// Create a matching session
		timeout := 15 * time.Minute
		msession, err := p.matchmakingRegistry.Create(ctx, logger, session, presence, matchSettings, timeout, errorFn, joinFn, false)
		if err != nil {
			return response.SendErrorToSession(status.Errorf(codes.Internal, "Failed to create matchmaking session: %v", err))
		}
		p.metrics.CustomCounter("create_active_count", map[string]string{}, 1)
		err = p.MatchCreateLoop(msession, 5*time.Minute)
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

	teamIndex := TeamUnassigned
	if len(request.Entrants) > 0 {
		teamIndex = TeamAlignment(request.Entrants[0].Alignment)
	}

	response := NewMatchmakingResult(session, logger, 0xFFFFFFFFFFFFFFFF, request.MatchID)

	// Make sure the match exists
	matchToken, err := NewMatchID(request.MatchID, p.node)
	if err != nil {
		return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Invalid match ID"))
	}

	match, _, err := p.matchRegistry.GetMatch(ctx, matchToken.String())
	if err != nil || match == nil {
		return response.SendErrorToSession(status.Errorf(codes.NotFound, "Match not found"))
	}

	// Extract the label
	ml := &MatchLabel{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), ml); err != nil {
		return response.SendErrorToSession(status.Errorf(codes.NotFound, err.Error()))
	}

	isMember := false
	channels := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	var thisChannel ChannelMember
	for _, channel := range channels {
		if channel.ChannelID == ml.Metadata.Channel {
			thisChannel = channel
			isMember = true
			break
		}
	}

	flags := ctx.Value(ctxFlagsKey{}).(SessionFlags)

	metricsTags := map[string]string{
		"team":    teamIndex.String(),
		"mode":    ml.Settings.Mode.String(),
		"channel": ml.Metadata.Channel.String(),
		"level":   ml.Settings.Level.String(),
	}

	p.metrics.CustomCounter("lobbyjoinsession_active_count", metricsTags, 1)

	if !flags.IsDeveloper {

		if teamIndex == TeamModerator {
			if !flags.IsModerator && !thisChannel.isModerator {
				return response.SendErrorToSession(status.Errorf(codes.PermissionDenied, "Not a moderator"))
			}
		} else {

			if lo.Contains(ml.Broadcaster.Tags, "membersonly") {
				// Check if the user is a member of the guild
				if !isMember {
					return response.SendErrorToSession(status.Errorf(codes.PermissionDenied, "Match is members only"))
				}
			}

			if teamIndex == TeamSpectator {
				if ml.Settings.Mode != evr.ModeSocialPublic {
					return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Match is not a social match"))
				}

			} else {
				if ml.Settings.LobbyType == evr.PublicLobby && teamIndex != TeamSpectator {
					return response.SendErrorToSession(status.Errorf(codes.InvalidArgument, "Match is a public match"))
				}
			}
		}
	}

	// Join the match

	if err = p.JoinEvrMatch(ctx, logger, session, matchToken, evr.Role(teamIndex)); err != nil {
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
