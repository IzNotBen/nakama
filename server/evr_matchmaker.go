package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	MatchJoinGracePeriod = 3 * time.Second
)

var (
	ErrDiscordLinkNotFound = errors.New("discord link not found")
)

func (p *EvrPipeline) ListUnassignedLobbies(ctx context.Context, session *sessionWS, ml *EvrMatchState) ([]*EvrMatchState, error) {

	// TODO Move this into the matchmaking registry
	qparts := make([]string, 0, 10)

	// MUST be an unassigned lobby
	qparts = append(qparts, LobbyType(evr.UnassignedLobby).Query(Must, 0))

	// MUST be one of the accessible channels (if provided)
	if len(ml.Broadcaster.Channels) > 0 {
		// Add the channels to the query
		qparts = append(qparts, HostedChannels(ml.Broadcaster.Channels).Query(Must, 0))
	}

	// Add each hosted channel as a SHOULD, with decreasing boost

	for i, channel := range ml.Broadcaster.Channels {
		qparts = append(qparts, Channel(channel).Query(Should, len(ml.Broadcaster.Channels)-i))
	}

	// Add the regions in descending order of priority
	for i, region := range ml.Broadcaster.Regions {
		qparts = append(qparts, Region(region).Query(Should, len(ml.Broadcaster.Regions)-i))
	}

	// Add tag query for prioritizing certain modes to specific hosts
	qparts = append(qparts, "label.broadcaster.tags:priority_mode_"+ml.Mode.String()+"^10")

	// Add the user's region request to the query
	qparts = append(qparts, Regions(ml.Broadcaster.Regions).Query(Must, 0))

	// SHOULD/MUST have the same features
	for _, f := range ml.RequiredFeatures {
		qparts = append(qparts, "+label.broadcaster.features:"+f)
	}

	for _, f := range ml.Broadcaster.Features {
		qparts = append(qparts, "label.broadcaster.features:"+f)
	}

	// TODO FIXME Add version lock and appid
	query := strings.Join(qparts, " ")

	// Load the matchmaking config and add the user's config to the query

	gconfig, err := p.matchmakingRegistry.LoadMatchmakingSettings(ctx, session.logger, SystemUserID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load global matchmaking config: %v", err)
	}

	config, err := p.matchmakingRegistry.LoadMatchmakingSettings(ctx, session.logger, session.UserID().String())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load matchmaking config: %v", err)
	}
	query = fmt.Sprintf("%s %s %s", query, gconfig.CreateQueryAddon, config.CreateQueryAddon)

	limit := 100
	minSize, maxSize := 1, 1 // Only the 1 broadcaster should be there.
	session.logger.Debug("Listing unassigned lobbies", zap.String("query", query))
	matches, err := listMatches(ctx, p, limit, minSize, maxSize, query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to find matches: %v", err)
	}

	// If no servers are available, return immediately.
	if len(matches) == 0 {
		return nil, ErrMatchmakingNoAvailableServers
	}

	// Create a slice containing the matches' labels
	labels := make([]*EvrMatchState, 0, len(matches))
	for _, match := range matches {
		label := &EvrMatchState{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to unmarshal match label: %v", err)
		}
		labels = append(labels, label)
	}

	return labels, nil
}

func (p *EvrPipeline) ListUnfilledLobbies(ctx context.Context, logger *zap.Logger, session *sessionWS, msession *MatchmakingSession) ([]*EvrMatchState, string, error) {
	var err error
	var query string

	var labels []*EvrMatchState

	partySize := 1
	if msession.Party != nil {
		partySize = msession.Party.members.Size()
	}

	if msession.Label.LobbyType != PublicLobby {
		// Do not backfill for private lobbies
		return nil, "", status.Errorf(codes.InvalidArgument, "Cannot backfill private lobbies")
	}

	// Search for existing matches
	if labels, query, err = p.MatchSearch(ctx, logger, session, msession.Label, partySize); err != nil {
		return nil, query, status.Errorf(codes.Internal, "Failed to search for matches: %v", err)
	}

	// Filter/sort the results
	if labels, _, err = p.MatchSort(ctx, session, msession, labels); err != nil {
		return nil, query, status.Errorf(codes.Internal, "Failed to filter matches: %v", err)
	}

	if len(labels) == 0 {
		return nil, query, nil
	}
	return labels, query, nil
}

// Backfill returns a match that the player can backfill
func (p *EvrPipeline) Backfill(ctx context.Context, session *sessionWS, msession *MatchmakingSession, minCount int) (*EvrMatchState, string, error) {
	// TODO Move this into the matchmaking registry
	// TODO Add a goroutine to look for matches that:
	// Are short 1 or more players
	// Have been short a player for X amount of time (~30 seconds?)
	// afterwhich, the match is considered a backfill candidate and the goroutine
	// Will open a matchmaking ticket. Any players that have a (backfill) ticket that matches
	logger := msession.Logger
	labels, query, err := p.ListUnfilledLobbies(ctx, logger, session, msession)
	if err != nil {
		return nil, query, err
	}

	var selected *EvrMatchState
	// Select the first match
	for _, label := range labels {
		// Check that the match is not full
		logger = logger.With(zap.String("match_id", label.MatchID.String()))

		mu, _ := p.backfillQueue.LoadOrStore(label.ID(), &sync.Mutex{})

		// Lock this backfill match
		mu.Lock()
		match, _, err := p.matchRegistry.GetMatch(ctx, label.ID())
		if match == nil || err != nil {
			logger.Warn("Match not found")
			mu.Unlock()
			continue
		}
		if err != nil {
			logger.Warn("Failed to get match label")
			mu.Unlock()
			continue
		}
		// Extract the latest label
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
			logger.Warn("Failed to get match label")
			mu.Unlock()
			continue
		}

		// Check if the match is full
		if msession.Label.Mode != evr.ModeSocialPublic && msession.Label.Mode != evr.ModeSocialPrivate {
			if msession.Label.TeamIndex != Spectator && msession.Label.TeamIndex != Moderator {
				// Check if there is space for the player(s)
				if label.PlayerCount > label.MaxPlayerCount-partySize {
					mu.Unlock()
					continue
				}
			}
		}

		go func() {
			// Delay unlock to prevent join overflows
			<-time.After(1 * time.Second)
			mu.Unlock()
		}()
		selected = label
		break
	}

	return selected, query, nil
}

// TODO FIXME Create a broadcaster registry

type BroadcasterLatencies struct {
	Endpoint evr.Endpoint
	Latency  time.Duration
}

// Matchmake attempts to find/create a match for the user using the nakama matchmaker
func (p *EvrPipeline) MatchMake(session *sessionWS, msession *MatchmakingSession) (err error) {
	// TODO Move this into the matchmaking registry
	ctx := msession.Context()
	// TODO FIXME Add a custom matcher for broadcaster matching
	// Get a list of all the broadcasters
	logger := msession.Logger
	// Ping endpoints
	endpoints := make([]evr.Endpoint, 0, 100)
	p.matchmakingRegistry.broadcasters.Range(func(key string, value evr.Endpoint) bool {
		endpoints = append(endpoints, value)
		return true
	})

	allRTTs, err := p.PingEndpoints(ctx, session, msession, endpoints)
	if err != nil {
		return err
	}

	query, stringProps, numericProps, err := msession.BuildQuery(allRTTs)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to build matchmaking query: %v", err)
	}

	// Add the user to the matchmaker
	sessionID := session.ID()

	userID := session.UserID().String()
	presences := []*MatchmakerPresence{
		{
			UserId:    userID,
			SessionId: session.ID().String(),
			Username:  session.Username(),
			Node:      p.node,
			SessionID: sessionID,
		},
	}
	userSettings := msession.UserSettings
	globalSettings := msession.GlobalSettings
	// Merge the user's config with the global config
	query = fmt.Sprintf("%s %s %s", query, globalSettings.BackfillQueryAddon, userSettings.BackfillQueryAddon)

	// Get the EVR ID from the context
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return status.Errorf(codes.Internal, "EVR ID not found in context")
	}
	stringProps["evr_id"] = evrID.Token()

	minCount := globalSettings.MinCount
	countMultiple := globalSettings.CountMultiple
	if msession.Label.Mode == evr.ModeCombatPublic {
		maxCount = 10
	} else {
		maxCount = 8
	}
	tags := map[string]string{
		"type":  msession.Label.LobbyType.String(),
		"mode":  msession.Label.Mode.String(),
		"level": msession.Label.Level.String(),
	}
	partyID := ""
	if msession.Party != nil {

		// Add the user's group to the string properties
		stringProps["party_group"] = userSettings.GroupID
		// Add the user's group to the query string
		query = fmt.Sprintf("%s properties.party_group:%s^5", query, userSettings.GroupID)

		ticket, presences, err := msession.Party.MatchmakerAdd(session.id.String(), p.node, query, minCount, maxCount, countMultiple, stringProps, numericProps)
		if err != nil {
			logger.Error("Failed to add to matchmaker", zap.Error(err))
			msession.Cancel(status.Errorf(codes.Internal, "Failed to add to matchmaker: %v", err))
			return nil
		}
		logger.Debug("Matchmaking with: ", zap.Any("presences", presences), zap.Error(err))
		msession.AddTicket(ticket, query)
		p.metrics.CustomCounter("matchmaker_party_tickets_count", tags, int64(len(presences)))
		p.metrics.CustomCounter("matchmaker_tickets_count", tags, int64(len(presences)))

		return nil
	}

	ticket, _, err := session.matchmaker.Add(ctx, presences, "", partyID, query, minCount, maxCount, countMultiple, stringProps, numericProps)
	if err != nil {
		return fmt.Errorf("failed to add to matchmaker: %v", err)
	}

	msession.AddTicket(ticket, query)
	p.metrics.CustomCounter("matchmaker_tickets_count", tags, 1)

	return nil
}

func (p *EvrPipeline) WaitForPartyMembers(ctx context.Context, logger *zap.Logger, msession *MatchmakingSession) ([]*MatchmakingSession, error) {
	var missingMembers []string
	matchmakingTimeout := time.NewTimer(30 * time.Second)
	ph := msession.Party
	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(5 * time.Second):
		case <-matchmakingTimeout.C:
			if missingMembers != nil {
				members := make([]string, 0, ph.members.Size())
				for _, member := range missingMembers {
					discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, uuid.FromStringOrNil(member))
					if err != nil {
						logger.Error("Failed to get discord id", zap.Error(err))
					}
					s := fmt.Sprintf("<@%s>", discordID)
					members = append(members, s)
				}
				content := fmt.Sprintf("Party members did not start matchmaking in time. Party members: %v", strings.Join(members, ", "))
				p.discordRegistry.SendMessage(ctx, msession.Session.UserID(), content)
				msession.Cancel(status.Errorf(codes.DeadlineExceeded, content))
			}
		}

		// Wait for the whole party press find match before adding them to the match maker
		msessions := make([]*MatchmakingSession, 0, ph.members.Size())
		missingMembers = make([]string, 0)
		for _, member := range ph.members.List() {
			// If the matching session exists, the player is matchmaking.
			m, found := p.matchmakingRegistry.GetMatchingBySessionId(uuid.FromStringOrNil(member.UserPresence.GetSessionId()))
			if !found {
				missingMembers = append(missingMembers, member.UserPresence.GetUserId())
				continue
			}
			msessions = append(msessions, m)
		}
		if len(msessions) >= ph.members.Size() {
			// Everyone is matchmaking.
			return msessions, nil
		}

		members := make([]string, 0, ph.members.Size())
		for _, m := range msessions {

			discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, m.Session.UserID())
			if err != nil {
				logger.Error("Failed to get discord id", zap.Error(err))
			}
			s := fmt.Sprintf("<@%s>", discordID)
			members = append(members, s)
		}

		err := p.discordRegistry.SendMessage(ctx, msession.Session.UserID(), fmt.Sprintf("Matchmaking with party members: %s", strings.Join(members, ", ")))
		if err != nil {
			logger.Error("Failed to send discord message", zap.Error(err))
		}

		return msessions, nil
	}
}

/*
func checkParty() {
	logger.Warn("Failed to join party group.", zap.String("group_id", groupID), zap.Error(err))
	p.discordRegistry.SendMessage(ctx, session.UserID(), fmt.Sprintf("Failed to join party group `%s`: %v", groupID, err))
	// Inform the player they have joined the party
	members := make([]string, 0, ph.members.Size())
	for _, member := range ph.members.List() {
		discordID, found := p.discordRegistry.Get(member.UserPresence.GetUserId())
		if !found {
			return status.Errorf(codes.Internal, "Discord ID not found of party member")
		}
		s := fmt.Sprintf("<@%s>", discordID)
		members = append(members, s)
	}
	ms.discordRegistry.SendMessage(ctx, session.UserID(), fmt.Sprintf("joined party group: %s with %s", ph.ID.String(), strings.Join(members, ", ")))
	logger.Debug("Joined party", zap.String("party_id", ph.IDStr), zap.Any("members", ph.members.List()))
}


func (p *EvrPipeline) CreatePartySocialLobby() {

	// Make sure all the members are in this lobby.
	thisMatchID, found := p.matchBySessionID.Load(sessionID)
	if !found {
		// when using -mp, the user won't be in a match.. just return and go to a social lobby first.
		return nil
	}

	missingMembers := make([]string, 0)
	for _, member := range ph.members.List() {
		if member.UserPresence.GetSessionId() != sessionID.String() {
			matchID, found := p.matchBySessionID.Load(member.UserPresence.GetSessionId())
			if !found {
				// User is not in a match at all.
				logger.Error("Party member is not in a match", zap.String("user_id", member.UserPresence.GetUserId()), zap.String("session_id", member.UserPresence.GetSessionId()))
				// Disband the party.
				session.pipeline.partyRegistry.Delete(partyID)
				return status.Errorf(codes.Internal, "Party member is not in a match")
			}
			if matchID != thisMatchID {
				discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, member.UserPresence.GetUserId())
				if err != nil {
					logger.Error("Failed to get discord id", zap.Error(err))
				}
				missingMembers = append(missingMembers, fmt.Sprintf("<@%s>", discordID))
		}
	}
	// If this is not a private social lobby, then create one and join it.
	if msession.Label.LobbyType != evr.LobbyTypePrivate {
		// Create a new match
		socialLabel := &EvrMatchState{
			LobbyType: evr.LobbyTypePrivate,
			Mode:      evr.ModeSocialPrivate,
			Level:     evr.LevelSocial,
			TeamIndex: evr.TeamSocial,
			Channel:   msession.Label.Channel,
		}

		matchID, err := p.MatchCreate(ctx, session, msession, socialLabel)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to create social lobby: %v", err)
		}
		// Join the social lobby
		err = p.JoinEvrMatch(ctx, logger, session, "", matchID, evr.TeamSocial)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to join social lobby: %v", err)
		}
		return nil
	}
}

*/

func (p *EvrPipeline) MatchmakeWithParty(msession *MatchmakingSession, ph *PartyHandler) error {

	ctx := msession.Context()
	// Join the leader

	err := p.joinPartyLeaderMatchLoop(ctx, msession, ph)
	if err != nil {
		msession.Cancel(status.Errorf(codes.Internal, "Failed to join party leader: %v", err))
		return nil
	}
	ph.RLock()
	leaderSessionID := ph.leader.UserPresence.GetSessionId()
	ph.RUnlock()

	// Wait for the leader to matchmake, for up to 30 seconds
	leaderMatchmakingTimeout := time.NewTimer(30 * time.Second)
	var leaderCtx context.Context
	for {
		select {
		case <-ctx.Done():
			// If the user has become the party leader, then this context will be done.
			return nil
		case <-leaderMatchmakingTimeout.C:
			msession.Cancel(status.Errorf(codes.DeadlineExceeded, "Party leader did not start matchmaking in time"))
		case <-time.After(5 * time.Second):
		}

		// Check if the party leader is matchmaking
		if leaderMSession, found := p.matchmakingRegistry.GetMatchingBySessionId(uuid.FromStringOrNil(leaderSessionID)); found {
			leaderCtx = leaderMSession.Context()
			break
		}
	}

	// Track the party leader's matchmaking context
	select {
	case <-leaderCtx.Done():
		cause := context.Cause(leaderCtx)
		if cause == leaderCtx.Err() || cause == ErrMatchmakingJoiningMatch {
			// The party leader has joined a match
			return nil
		}
		msession.Cancel(status.Errorf(codes.Canceled, "Party leader's matchmaking context done"))
	case <-ctx.Done():
		return nil
	}
	return nil
}

func (p *EvrPipeline) joinPartyLeaderMatchLoop(ctx context.Context, msession *MatchmakingSession, ph *PartyHandler) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-msession.Context().Done():
			return nil
		default:
		}
		ph.RLock()
		defer ph.RUnlock()

		leaderSessionID := ph.leader.UserPresence.GetSessionId()
		thisSessionID := msession.Session.ID().String()
		// If this user has become the party leader, return
		if leaderSessionID == thisSessionID {
			return msession.Cancel(fmt.Errorf("user has become party leader"))
		}
		matchID, found := p.matchBySessionID.Load(thisSessionID)
		if !found {
			return msession.Cancel(status.Errorf(codes.NotFound, "this user's match not found"))
		}
		// Verify that the party leader is not in the same match

		leaderMatchID, found := p.matchBySessionID.Load(leaderSessionID)
		if !found {
			return msession.Cancel(status.Errorf(codes.NotFound, "leader's match not found"))
		}

		if leaderMatchID == matchID {
			return nil
		}

		err := p.joinMatchBySessionID(ctx, msession, leaderSessionID)
		if err != nil {
			return msession.Cancel(status.Errorf(codes.Internal, "Failed to join leader's match: %v", err))
		}
		<-time.After(5 * time.Second)
	}

}

func (p *EvrPipeline) joinMatchBySessionID(ctx context.Context, msession *MatchmakingSession, sessionID string) error {

	// Check if the party leader is another social lobby
	// If the party leader is in a different social lobby, check if the match has space
	// If the match is full, return match full

	matchID, found := p.matchBySessionID.Load(sessionID)
	if !found {
		return status.Errorf(codes.NotFound, "Match not found")
	}

	match, _, err := p.matchRegistry.GetMatch(ctx, matchID)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to get match: %v", err)
	}

	// Get the label
	label := &EvrMatchState{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
		return status.Errorf(codes.Internal, "Failed to unmarshal match label: %v", err)
	}
	leaderTeamIndex := -1

	// Get the party leaders team index
	for index, player := range label.Players {
		if index == evr.TeamSpectator || index == evr.TeamModerator {
			continue
		}
		if player.SessionID == sessionID {
			leaderTeamIndex = index
			break
		}
	}

	if label.PlayerCount < label.MaxPlayerCount {
		// Send this user to the match
		foundMatch := FoundMatch{
			MatchID:   matchID,
			TeamIndex: TeamIndex(leaderTeamIndex),
		}
		msession.MatchJoinCh <- foundMatch
	}

	return nil
}

// Wrapper for the matchRegistry.ListMatches function.
func listMatches(ctx context.Context, p *EvrPipeline, limit int, minSize int, maxSize int, query string) ([]*api.Match, error) {
	return p.runtimeModule.MatchList(ctx, limit, true, "", &minSize, &maxSize, query)
}

func buildMatchQueryFromLabel(ml *EvrMatchState) string {
	var boost int = 0 // Default booster

	qparts := []string{
		// MUST be an open lobby
		OpenLobby.Query(Must, boost),
		// MUST be the same lobby type
		LobbyType(ml.LobbyType).Query(Must, boost),
		// MUST be the same mode
		GameMode(ml.Mode).Query(Must, boost),
	}

	if ml.TeamIndex != Spectator && ml.TeamIndex != Moderator {
		// MUST have room for this party on the teams
		qparts = append(qparts, fmt.Sprintf("+label.player_count:<=%d", ml.PlayerCount))
	}

	// MUST NOT much into the same lobby
	if ml.MatchID != uuid.Nil {
		qparts = append(qparts, MatchId(ml.MatchID).Query(MustNot, boost))
	}

	// MUST be a broadcaster on a channel the user has access to
	if len(ml.Broadcaster.Channels) != 0 {
		qparts = append(qparts, Channels(ml.Broadcaster.Channels).Query(Must, 0))
	}

	// SHOULD Add the current channel as a high boost SHOULD
	if *ml.Channel != uuid.Nil {
		qparts = append(qparts, Channel(*ml.Channel).Query(Should, 3))
	}

	switch len(ml.Broadcaster.Regions) {
	case 0:
		// If no regions are specified, use default
		qparts = append(qparts, Region(evr.Symbol(0)).Query(Must, 2))
	case 1:
		// If only one region is specified, use it as a MUST
		qparts = append(qparts, Region(ml.Broadcaster.Regions[0]).Query(Must, 2))
	default:
		// If multiple regions are specified, use the first as a MUST and the rest as SHOULD
		qparts = append(qparts, Region(ml.Broadcaster.Regions[0]).Query(Must, 2))
		for _, r := range ml.Broadcaster.Regions[1:] {
			qparts = append(qparts, Region(r).Query(Should, 2))
		}
	}

	for _, r := range ml.Broadcaster.Regions {
		qparts = append(qparts, Region(r).Query(Should, 2))
	}

	// Setup the query and logger
	return strings.Join(qparts, " ")
}

// MatchSearch attempts to find/create a match for the user.
func (p *EvrPipeline) MatchSearch(ctx context.Context, logger *zap.Logger, session *sessionWS, ml *EvrMatchState, partySize int) ([]*EvrMatchState, string, error) {

	// TODO Move this into the matchmaking registry
	query := buildMatchQueryFromLabel(ml)

	// Basic search defaults
	const (
		minSize = 1
		limit   = 50
	)
	maxSize := MatchMaxSize - partySize // the broadcaster is included, so this has free spots
	logger = logger.With(zap.String("query", query), zap.Any("label", ml))

	// Search for possible matches
	logger.Debug("Searching for matches")
	matches, err := listMatches(ctx, p, limit, minSize, maxSize+1, query) // +1 for the broadcaster
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "Failed to find matches: %v", err)
	}

	// Create a label slice of the matches
	labels := make([]*EvrMatchState, len(matches))
	for i, match := range matches {
		label := &EvrMatchState{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
			logger.Error("Error unmarshalling match label", zap.Error(err))
			continue
		}
		labels[i] = label
	}

	return labels, query, nil
}

// mroundRTT rounds the rtt to the nearest modulus
func mroundRTT(rtt time.Duration, modulus time.Duration) time.Duration {
	r := float64(rtt) / float64(modulus)
	return time.Duration(math.Round(r)) * modulus
}

// RTTweightedPopulationCmp compares two RTTs and populations
func RTTweightedPopulationCmp(i, j time.Duration, o, p int) bool {
	// Sort by if over or under 90ms
	if i > 90*time.Millisecond && j < 90*time.Millisecond {
		return true
	}
	if i < 90*time.Millisecond && j > 90*time.Millisecond {
		return false
	}
	// Sort by Population
	if o > p {
		return true
	}
	if o < p {
		return false
	}
	// If all else equal, sort by rtt
	return i < j
}

// PopulationCmp compares two populations
func PopulationCmp(o, p int, i, j time.Duration) bool {
	if o == p {
		// If all else equal, sort by rtt
		return i < j
	}
	return o > p
}

// MatchSort pings the matches and filters the matches by the user's cached latencies.
func (p *EvrPipeline) MatchSort(ctx context.Context, session *sessionWS, msession *MatchmakingSession, labels []*EvrMatchState) (filtered []*EvrMatchState, rtts []time.Duration, err error) {
	// TODO Move this into the matchmaking registry

	// Create a slice for the rtts of the filtered matches
	rtts = make([]time.Duration, 0, len(labels))

	// If there are no matches, return
	if len(labels) == 0 {
		return labels, rtts, nil
	}

	// Only ping the unique endpoints
	endpoints := make(map[string]evr.Endpoint, len(labels))
	for _, label := range labels {
		endpoints[label.Broadcaster.Endpoint.ID()] = label.Broadcaster.Endpoint
	}

	// Ping the endpoints
	result, err := p.PingEndpoints(ctx, session, msession, lo.Values(endpoints))
	if err != nil {
		return nil, nil, err
	}

	modulus := 10 * time.Millisecond
	// Create a map of endpoint Ids to endpointRTTs, rounding the endpointRTTs to the nearest 10ms
	endpointRTTs := make(map[string]time.Duration, len(result))
	for _, r := range result {
		endpointRTTs[r.Endpoint.ID()] = mroundRTT(r.RTT, modulus)
	}

	type labelData struct {
		Id          string
		PlayerCount int
		RTT         time.Duration
	}
	// Create a map of endpoint Ids to sizes and latencies
	datas := make([]labelData, 0, len(labels))
	for _, label := range labels {
		id := label.Broadcaster.Endpoint.ID()
		rtt := endpointRTTs[id]
		// If the rtt is 0 or over 270ms, skip the match
		if rtt == 0 || rtt > 270*time.Millisecond {
			continue
		}
		datas = append(datas, labelData{id, label.PlayerCount, rtt})
	}

	// Sort the matches
	switch msession.Label.Mode {
	case evr.ModeArenaPublic:
		// Split Arena matches into two groups: over and under 90ms
		// Sort by if over or under 90ms, then population, then latency
		sort.SliceStable(datas, func(i, j int) bool {
			return RTTweightedPopulationCmp(datas[i].RTT, datas[j].RTT, datas[i].PlayerCount, datas[j].PlayerCount)
		})

	case evr.ModeCombatPublic:
		// Sort Combat matches by population, then latency
		fallthrough

	case evr.ModeSocialPublic:
		fallthrough

	default:
		sort.SliceStable(datas, func(i, j int) bool {
			return PopulationCmp(datas[i].PlayerCount, datas[j].PlayerCount, datas[i].RTT, datas[j].RTT)
		})
	}

	// Create a slice of the filtered matches
	filtered = make([]*EvrMatchState, 0, len(datas))
	for _, data := range datas {
		for _, label := range labels {
			if label.Broadcaster.Endpoint.ID() == data.Id {
				filtered = append(filtered, label)
				rtts = append(rtts, data.RTT)
				break
			}
		}
	}

	// Sort the matches by region, putting the user's region first

	if len(msession.Label.Broadcaster.Regions) == 0 {
		return filtered, rtts, nil
	}

	region := msession.Label.Broadcaster.Regions[0]
	sort.SliceStable(filtered, func(i, j int) bool {
		// Sort by the user's region first
		if slices.Contains(filtered[i].Broadcaster.Regions, region) {
			return true
		}
		return false
	})

	return filtered, rtts, nil
}

// TODO FIXME This need to use allocateBroadcaster instad.
// MatchCreate creates a match on an available unassigned broadcaster using the given label
func (p *EvrPipeline) MatchCreate(ctx context.Context, session *sessionWS, msession *MatchmakingSession, label *EvrMatchState) (matchId string, err error) {
	label.MaxSize = MatchMaxSize
	// Lock the broadcaster's until the match is created
	p.matchmakingRegistry.Lock()
	defer p.matchmakingRegistry.Unlock()
	// TODO Move this into the matchmaking registry
	ml := msession.Label

	channels := ml.Broadcaster.Channels
	region := ml.Broadcaster.Region
	queryAddon := msession.GlobalSettings.CreateQueryAddon + " " + msession.UserSettings.CreateQueryAddon

	// Create a new match
	matches, err := p.matchmakingRegistry.ListUnassignedLobbies(ctx, channels, region, queryAddon)
	if err != nil {
		return "", err
	}

	// Filter/sort the results
	matches, _, err = p.MatchSort(ctx, session, msession, matches)
	if err != nil {
		return "", fmt.Errorf("failed to filter unassigned matches: %v", err)
	}

	if len(matches) == 0 {
		return "", ErrMatchmakingNoAvailableServers
	}

	// Join the lowest rtt match
	match := matches[0]

	// Load the level.
	parkingMatchId := fmt.Sprintf("%s.%s", match.MatchID, p.node)

	label.SpawnedBy = session.UserID().String()

	prepareSignalData := SignalPrepareSession{
		State: *label,
	}
	// Prepare the match
	response, err := SignalMatch(ctx, p.matchRegistry, parkingMatchId, prepareSignalData)
	if err != nil {
		return "", fmt.Errorf("failed to load level: %v", err)
	}
	if response != "" {
		return "", fmt.Errorf("failed to prepare match: %s", response)
	}
	<-time.After(5 * time.Second)
	// Return the newly active match.
	return parkingMatchId, nil
}

// JoinEvrMatch allows a player to join a match.
func (p *EvrPipeline) JoinEvrMatch(ctx context.Context, logger *zap.Logger, session *sessionWS, query string, matchIDStr string, teamIndex int) error {
	// Append the node to the matchID if it doesn't already contain one.
	if !strings.Contains(matchIDStr, ".") {
		matchIDStr = fmt.Sprintf("%s.%s", matchIDStr, p.node)
	}

	partyID := uuid.Nil

	if msession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.ID()); ok {
		if msession != nil && msession.Party != nil {
			partyID = msession.Party.ID
		}
	}

	s := strings.Split(matchIDStr, ".")[0]
	matchID := uuid.FromStringOrNil(s)
	if matchID == uuid.Nil {
		return fmt.Errorf("invalid match id: %s", matchIDStr)
	}

	// Retrieve the evrID from the context.
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	if !ok {
		return errors.New("evr id not found in context")
	}

	// Get the match label
	match, _, err := p.matchRegistry.GetMatch(ctx, matchIDStr)
	if err != nil {
		return fmt.Errorf("failed to get match: %w", err)
	}
	if match == nil {
		return fmt.Errorf("match not found: %s", matchIDStr)
	}

	label := &EvrMatchState{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
		return fmt.Errorf("failed to unmarshal match label: %w", err)
	}

	groupID := uuid.Nil
	// Get the channel
	if label.Channel != nil {
		groupID = uuid.FromStringOrNil((*label.Channel).String())
	}

	// Determine the display name
	displayName, err := SetDisplayNameByChannelBySession(ctx, p.runtimeModule, logger, p.discordRegistry, session, groupID.String())
	if err != nil {
		logger.Warn("Failed to set display name.", zap.Error(err))
	}

	// If this is a NoVR user, give the profile's displayName a bot suffix
	// Get the NoVR key from context
	if flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags); ok {
		if flags.IsNoVR {
			displayName = fmt.Sprintf("%s [BOT]", displayName)
		}
	}

	// Set the profile's display name.
	profile, found := p.profileRegistry.Load(session.UserID(), evrID)
	if !found {
		defer session.Close("profile not found", runtime.PresenceReasonUnknown)
		return fmt.Errorf("profile not found: %s", session.UserID())
	}

	profile.UpdateDisplayName(displayName)

	// Add the user's profile to the cache (by EvrID)
	err = p.profileRegistry.Cache(profile.GetServer())
	if err != nil {
		logger.Warn("Failed to add profile to cache", zap.Error(err))
	}

	// Prepare the player session metadata.

	discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, session.UserID())
	if err != nil {
		logger.Error("Failed to get discord id", zap.Error(err))
	}

	mp := EvrMatchPresence{
		Node:          p.node,
		UserID:        session.userID,
		SessionID:     session.id,
		Username:      session.Username(),
		DisplayName:   displayName,
		EvrID:         evrID,
		PlayerSession: uuid.Must(uuid.NewV4()),
		PartyID:       partyID,
		TeamIndex:     int(teamIndex),
		DiscordID:     discordID,
		Query:         query,
		ClientIP:      session.clientIP,
		PartyID:       groupID,
	}

	// Marshal the player metadata into JSON.
	jsonMeta, err := json.Marshal(mp)
	if err != nil {
		return fmt.Errorf("failed to marshal player meta: %w", err)
	}
	metadata := map[string]string{"playermeta": string(jsonMeta)}
	// Do the join attempt to avoid race conditions
	found, allowed, isNew, reason, _, _ := p.matchRegistry.JoinAttempt(ctx, matchID, p.node, session.UserID(), session.ID(), session.Username(), session.Expiry(), session.Vars(), session.clientIP, session.clientPort, p.node, metadata)
	if !found {
		return fmt.Errorf("match not found: %s", matchIDStr)
	}
	if !allowed {
		switch reason {
		case ErrJoinRejectedUnassignedLobby:
			return status.Errorf(codes.FailedPrecondition, "join not allowed: %s", reason)
		case ErrJoinRejectedDuplicateJoin:
			return status.Errorf(codes.AlreadyExists, "join not allowed: %s", reason)
		case ErrJoinRejectedNotModerator:
			return status.Errorf(codes.PermissionDenied, "join not allowed: %s", reason)
		case ErrJoinRejectedLobbyFull:
			return status.Errorf(codes.ResourceExhausted, "join not allowed: %s", reason)
		default:
			return status.Errorf(codes.Internal, "join not allowed: %s", reason)
		}
	}

	if isNew {
		// Trigger the MatchJoin event.
		stream := PresenceStream{Mode: StreamModeMatchAuthoritative, Subject: matchID, Label: p.node}
		m := PresenceMeta{
			Username: session.Username(),
			Format:   session.Format(),
			Status:   mp.Query,
		}
		if success, _ := p.tracker.Track(session.Context(), session.ID(), stream, session.UserID(), m); success {
			// Kick the user from any other matches they may be part of.
			// WARNING This cannot be used during transition. It will kick the player from their current match.
			//p.tracker.UntrackLocalByModes(session.ID(), matchStreamModes, stream)
		}
	}

	p.matchBySessionID.Store(session.ID().String(), matchIDStr)
	p.matchByEvrID.Store(evrID.Token(), matchIDStr)

	return nil
}

// PingEndpoints pings the endpoints and returns the latencies.
func (p *EvrPipeline) PingEndpoints(ctx context.Context, session *sessionWS, msession *MatchmakingSession, endpoints []evr.Endpoint) ([]LatencyMetric, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	logger := msession.Logger
	p.matchmakingRegistry.UpdateBroadcasters(endpoints)

	// Get the candidates for pinging
	candidates := msession.GetPingCandidates(endpoints...)
	if len(candidates) > 0 {
		if err := p.sendPingRequest(logger, session, candidates); err != nil {
			return nil, err
		}

		select {
		case <-msession.Ctx.Done():
			return nil, ErrMatchmakingCanceled
		case <-time.After(5 * time.Second):
			// Just ignore the ping results if the ping times out
			logger.Warn("Ping request timed out")
		case results := <-msession.PingResultsCh:
			cache := msession.LatencyCache
			// Look up the endpoint in the cache and update the latency

			// Add the latencies to the cache
			for _, response := range results {

				broadcaster, ok := p.matchmakingRegistry.broadcasters.Load(response.EndpointID())
				if !ok {
					logger.Warn("Endpoint not found in cache", zap.String("endpoint", response.EndpointID()))
					continue
				}

				r := LatencyMetric{
					Endpoint:  broadcaster,
					RTT:       response.RTT(),
					Timestamp: time.Now(),
				}

				cache.Store(r.ID(), r)
			}

		}
	}

	return p.getEndpointLatencies(session, endpoints), nil
}

// sendPingRequest sends a ping request to the given candidates.
func (p *EvrPipeline) sendPingRequest(logger *zap.Logger, session *sessionWS, candidates []evr.Endpoint) error {

	if err := session.SendEvr(
		evr.NewLobbyPingRequest(275, candidates),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return err
	}

	logger.Debug("Sent ping request", zap.Any("candidates", candidates))
	return nil
}

// getEndpointLatencies returns the latencies for the given endpoints.
func (p *EvrPipeline) getEndpointLatencies(session *sessionWS, endpoints []evr.Endpoint) []LatencyMetric {
	endpointRTTs := p.matchmakingRegistry.GetLatencies(session.UserID(), endpoints)

	results := make([]LatencyMetric, 0, len(endpoints))
	for _, e := range endpoints {
		if l, ok := endpointRTTs[e.ID()]; ok {
			results = append(results, l)
		}
	}

	return results
}

// checkSuspensionStatus checks if the user is suspended from the channel and returns the suspension status.
func (p *EvrPipeline) checkSuspensionStatus(ctx context.Context, logger *zap.Logger, userID string, channel uuid.UUID) (statuses []*SuspensionStatus, err error) {
	if channel == uuid.Nil {
		return nil, nil
	}

	// Get the guild group metadata
	md, err := p.discordRegistry.GetGuildGroupMetadata(ctx, channel.String())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get guild group metadata: %v", err)
	}
	if md == nil {
		return nil, status.Errorf(codes.Internal, "Metadata is nil for channel: %s", channel)
	}

	if md.SuspensionRole == "" {
		return nil, nil
	}

	// Get the user's discordId
	discordId, err := p.discordRegistry.GetDiscordIdByUserId(ctx, uuid.FromStringOrNil(userID))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get discord id: %v", err)

	}

	// Get the guild member
	member, err := p.discordRegistry.GetGuildMember(ctx, md.GuildID, discordId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Failed to get guild member: %v", err)
	}

	if member == nil {
		return nil, status.Errorf(codes.Internal, "Member is nil for discordId: %s", discordId)
	}

	if !slices.Contains(member.Roles, md.SuspensionRole) {
		return nil, nil
	}

	// TODO This needs to be refactored. extract method.
	// Check if the user has a detailed suspension status in storage
	keys := make([]string, 0, 2)
	// List all the storage objects in the SuspensionStatusCollection for this user
	ids, _, err := p.runtimeModule.StorageList(ctx, uuid.Nil.String(), userID, SuspensionStatusCollection, 1000, "")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to list suspension status: %v", err)
	}
	if len(ids) == 0 {
		// Get the guild name and Id
		guild, err := p.discordRegistry.GetGuild(ctx, md.GuildID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get guild: %v", err)
		}
		// Return the basic suspension status
		return []*SuspensionStatus{
			{
				GuildId:       guild.ID,
				GuildName:     guild.Name,
				UserId:        userID,
				UserDiscordId: discordId,
				Reason:        fmt.Sprintf("You are currently suspended from %s.", guild.Name),
			},
		}, nil
	}

	for _, id := range ids {
		keys = append(keys, id.Key)
	}

	// Construct the read operations
	ops := make([]*runtime.StorageRead, 0, len(keys))
	for _, id := range keys {
		ops = append(ops, &runtime.StorageRead{
			Collection: SuspensionStatusCollection,
			Key:        id,
			UserID:     userID,
		})
	}
	objs, err := p.runtimeModule.StorageRead(ctx, ops)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to read suspension status: %v", err)
	}
	// If no suspension status was found, return the basic suspension status
	if len(objs) == 0 {
		// Get the guild name and Id
		guild, err := p.discordRegistry.GetGuild(ctx, md.GuildID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get guild: %v", err)
		}

		// Return the basic suspension status
		return []*SuspensionStatus{
			{
				GuildId:       guild.ID,
				GuildName:     guild.Name,
				UserId:        userID,
				UserDiscordId: discordId,
				Reason:        "You are suspended from this channel.\nContact a moderator for more information.",
			},
		}, nil
	}

	// Check the status to see if it's expired.
	suspensions := make([]*SuspensionStatus, 0, len(objs))
	// Suspension status was found. Check its expiry
	for _, obj := range objs {
		// Unmarshal the suspension status
		suspension := &SuspensionStatus{}
		if err := json.Unmarshal([]byte(obj.Value), suspension); err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to unmarshal suspension status: %v", err)
		}
		// Check if the suspension has expired
		if suspension.Expiry.After(time.Now()) {
			// The user is suspended from this lobby
			suspensions = append(suspensions, suspension)
		} else {
			// The suspension has expired, delete the object
			if err := p.runtimeModule.StorageDelete(ctx, []*runtime.StorageDelete{
				{
					Collection: SuspensionStatusCollection,
					Key:        obj.Key,
					UserID:     userID,
				},
			}); err != nil {
				logger.Error("Failed to delete suspension status", zap.Error(err))
			}
		}
	}
	return suspensions, nil
}
