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

// Backfill returns a match that the player can backfill
func (p *EvrPipeline) Backfill(ctx context.Context, msession *MatchmakingSession) (*MatchLabel, string, error) { // Create a new matching session
	// TODO Move this into the matchmaking registry
	// TODO Add a goroutine to look for matches that:
	// Are short 1 or more players
	// Have been short a player for X amount of time (~30 seconds?)
	// afterwhich, the match is considered a backfill candidate and the goroutine
	// Will open a matchmaking ticket. Any players that have a (backfill) ticket that matches

	var err error
	var query string
	logger := msession.Logger
	var labels []*MatchLabel

	partySize := 1
	if msession.Party != nil {
		partySize = msession.Party.members.Size()
	}

	if msession.MatchCriteria.LobbyType != evr.PublicLobby {
		// Do not backfill for private lobbies
		return nil, "", status.Errorf(codes.InvalidArgument, "Cannot backfill private lobbies")
	}

	// Search for existing matches
	if labels, query, err = p.MatchSearch(ctx, logger, msession); err != nil {
		return nil, query, status.Errorf(codes.Internal, "Failed to search for matches: %v", err)
	}

	// Filter/sort the results
	if labels, _, err = p.MatchSort(ctx, msession, labels); err != nil {
		return nil, query, status.Errorf(codes.Internal, "Failed to filter matches: %v", err)
	}

	if len(labels) == 0 {
		return nil, query, nil
	}

	var selected *MatchLabel
	// Select the first match
	for _, label := range labels {
		// Check that the match is not full
		logger = logger.With(zap.String("match_id", label.MatchID()))

		mu, _ := p.backfillQueue.LoadOrStore(label.MatchID(), &sync.Mutex{})

		// Lock this backfill match
		mu.Lock()
		match, _, err := p.matchRegistry.GetMatch(ctx, label.MatchID())
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

		roles := label.getRoleOpenings()

		switch msession.Presence.Alignment {
		case evr.UnassignedRole, evr.BlueTeamRole, evr.OrangeTeamRole:
			if roles[evr.BlueTeamRole]+roles[evr.OrangeTeamRole] >= partySize {
				selected = label
			}
		default:
			if roles[msession.Presence.Alignment] >= partySize {
				selected = label
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
func (p *EvrPipeline) MatchMake(msession *MatchmakingSession) (err error) {
	// TODO Move this into the matchmaking registry
	ctx := msession.Context()
	// TODO FIXME Add a custom matcher for broadcaster matching
	// Get a list of all the broadcasters
	logger := msession.Logger
	// Ping endpoints
	endpoints := make([]evr.Endpoint, 0, 100)

	broadcasters := p.matchmakingRegistry.broadcasterRegistry.GetBroadcasters(msession.MatchCriteria.VersionLock, false)
	for _, broadcaster := range broadcasters {
		endpoints = append(endpoints, broadcaster.Presence.Endpoint)
	}

	endpointMap := make(map[string]evr.Endpoint)
	for _, endpoint := range endpoints {
		endpointMap[endpoint.ID()] = endpoint
	}

	allRTTs, err := msession.PingEndpoints(endpointMap)
	if err != nil {
		return err
	}

	query, stringProps, numericProps, err := msession.BuildQuery(allRTTs)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to build matchmaking query: %v", err)
	}

	// Add the user to the matchmaker
	sessionID := msession.Session.ID()

	userID := msession.Session.UserID().String()
	presences := []*MatchmakerPresence{
		{
			UserId:    userID,
			SessionId: msession.Session.ID().String(),
			Username:  msession.Session.Username(),
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
		"type":  msession.MatchCriteria.LobbyType.String(),
		"mode":  msession.MatchCriteria.Mode.String(),
		"level": msession.MatchCriteria.Level.String(),
	}
	partyID := ""
	if msession.Party != nil {

		// Add the user's group to the string properties
		stringProps["party_group"] = userSettings.GroupID
		// Add the user's group to the query string
		query = fmt.Sprintf("%s properties.party_group:%s^5", query, userSettings.GroupID)

		ticket, presences, err := msession.Party.MatchmakerAdd(msession.Session.id.String(), p.node, query, minCount, maxCount, countMultiple, stringProps, numericProps)
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

	ticket, _, err := msession.Session.matchmaker.Add(ctx, presences, "", partyID, query, minCount, maxCount, countMultiple, stringProps, numericProps)
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

	for {
		select {
		case <-ctx.Done():
			logger.Debug("Context done")
			return nil, nil
		case <-time.After(5 * time.Second):
		}

		// Check if the whole team is in the same social lobby
		err := p.CollectParty(ctx, logger, msession)
		if err != nil {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(1 * time.Second):
		default:
		}

		ph := msession.Party
		// Wait for the whole party press find match before adding them to the match maker
		msessions := make([]*MatchmakingSession, 0, ph.members.Size())
		missingMembers = make([]string, 0)
		for _, member := range ph.members.List() {
			// If the matching session exists, the player is matchmaking.
			m, found := p.matchmakingRegistry.GetMatchingBySessionId(uuid.FromStringOrNil(member.UserPresence.GetSessionId()))
			if found {
				msessions = append(msessions, m)
				continue
			}
			missingMembers = append(missingMembers, member.UserPresence.GetUserId())
		}

		if len(msessions) >= ph.members.Size() {
			// Everyone is matchmaking.
			return msessions, nil
		}
		select {
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
		default:
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

		err = p.discordRegistry.SendMessage(ctx, msession.Session.UserID(), fmt.Sprintf("Matchmaking with party members: %s", strings.Join(members, ", ")))
		if err != nil {
			logger.Error("Failed to send discord message", zap.Error(err))
		}

		return msessions, nil
	}
}

func (p *EvrPipeline) CollectParty(ctx context.Context, logger *zap.Logger, msession *MatchmakingSession) error {
	var label *matchState
	thisMatch, found := p.matchBySessionID.Load(msession.Session.ID().String())
	if found {
		// Get the match info
		match, _, err := p.matchRegistry.GetMatch(ctx, thisMatch.String())
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to get match: %v", err)
		}
		// Get the label
		label = &matchState{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
			return status.Errorf(codes.Internal, "Failed to unmarshal match label: %v", err)
		}
		// If this match is not a social lobby, then return
		// If the match is a social lobby, check if the party members are in the same social lobby
		if label.Settings.Mode == evr.ModeSocialPublic || label.Settings.Mode == evr.ModeSocialPrivate {
			return nil
		}
	}

	// Check if the whole party is in this social lobby
	missingMembers := make([]string, 0)

	for _, member := range msession.Party.members.List() {
		m, found := p.matchBySessionID.Load(member.UserPresence.GetSessionId())
		if !found {
			missingMembers = append(missingMembers, member.UserPresence.GetUserId())
		}
		if m != thisMatch {
			missingMembers = append(missingMembers, member.UserPresence.GetUserId())
		}
	}

	if len(missingMembers) == 0 || label.Settings.Mode == evr.ModeSocialPrivate {
		return nil
	}

	matchSettings := NewMatchSettingsFromMode(msession.MatchCriteria.Mode, msession.MatchCriteria.VersionLock)
	// Create and join a new private social lobby
	matchID, err := p.MatchCreate(ctx, msession, matchSettings)

	switch status.Code(err) {
	case codes.OK:
		if matchID.IsNil() {
			return msession.Cancel(fmt.Errorf("match is nil"))
		}
		foundMatch := FoundMatch{
			MatchID: matchID,

			Alignment: TeamSocial,
		}
		select {
		case <-ctx.Done():
			return nil
		case msession.MatchJoinCh <- foundMatch:
			p.metrics.CustomCounter("match_create_private_party_lobby_count", msession.metricsTags, 1)
			logger.Debug("Joining match", zap.String("mid", foundMatch.MatchID.String()))
		}
	default:
		return msession.Cancel(status.Errorf(codes.Internal, "Failed to create private social lobby: %v", err))
	}
	return nil
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
		socialLabel := &matchState{
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
	if ph.leader == nil {
		return status.Errorf(codes.Internal, "Party leader not found")
	}

	ctx := msession.Context()

	// If the party leader is in a non-social lobby, try to join that match.
	// If the party leader is in a private social lobby, create a new match and join that.
	// If the party leader is in a public social lobby, and the rest of the party is not in the same social lobby, create a new private social lobby, and join that.
	// If the party leader is in a public social lobby, and the rest of the party is in the same social lobby, start matchmaking.

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
		<-time.After(5 * time.Second)

		cause := context.Cause(leaderCtx)
		if cause == leaderCtx.Err() || cause == ErrMatchmakingJoiningMatch {
			// The party leader has joined a match
			msession.Cancel(nil)
		}
		msession.Cancel(cause)

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

	match, _, err := p.matchRegistry.GetMatch(ctx, matchID.String())
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to get match: %v", err)
	}

	// Get the label
	label := &matchState{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
		return status.Errorf(codes.Internal, "Failed to unmarshal match label: %v", err)
	}
	leaderTeamIndex := evr.UnassignedRole

	// Get the party leaders team index
	for _, player := range label.Presences {
		if player.Alignment == evr.SpectatorRole || player.Alignment == evr.ModeratorRole {
			continue
		}
		if player.SessionID.String() == sessionID {
			leaderTeamIndex = player.Alignment
			break
		}
	}

	if label.Settings.ParticipantLimit > len(label.Presences) {
		// Send this user to the match
		foundMatch := FoundMatch{
			MatchID:   matchID,
			Alignment: TeamAlignment(leaderTeamIndex),
		}
		msession.MatchJoinCh <- foundMatch
	}

	return nil
}

// Wrapper for the matchRegistry.ListMatches function.
func listMatches(ctx context.Context, p *EvrPipeline, limit int, minSize int, maxSize int, query string) ([]*api.Match, error) {
	return p.runtimeModule.MatchList(ctx, limit, true, "", &minSize, &maxSize, query)
}

type MatchCriteria struct {
	LobbyType             evr.LobbyType
	Open                  bool
	Public                bool
	VersionLock           evr.Symbol
	Mode                  evr.Symbol
	Level                 evr.Symbol
	Channels              []uuid.UUID
	Region                evr.Symbol
	CurrentMatchID        MatchID
	Alignment             evr.Role
	BackfillQueryAddon    string
	MatchmakingQueryAddon string
}

func (c MatchCriteria) String() string {

	qparts := make([]string, 0, 10)

	if c.Open {
		qparts = append(qparts, "+label.open:")
	} else {
		qparts = append(qparts, "-label.open:")
	}

	if c.Public {
		qparts = append(qparts, "+label.public:")
	} else {
		qparts = append(qparts, "-label.public:")
	}

	if c.VersionLock != evr.Symbol(0) {
		qparts = append(qparts, "+label.version_lock:"+c.VersionLock.String())
	} else {
		qparts = append(qparts, "-label.flags.versionlockrequired")
	}

	if c.Mode != evr.Symbol(0) {
		qparts = append(qparts, "+label.mode:"+c.Mode.String())
	} else {
		qparts = append(qparts, "-label.flags.moderequired")
	}

	if c.Level != evr.Symbol(0) {
		qparts = append(qparts, "+label.level:"+c.Level.String())
	} else {
		qparts = append(qparts, "-label.flags.levelrequired")
	}

	if len(c.Channels) != 0 {

		channels := lo.Map(c.Channels, func(id uuid.UUID, _ int) string { return id.String() })
		qparts = append(qparts, fmt.Sprintf("+label.broadcaster.channels:/(%s)/", strings.Join(channels, "|")))
	} else {
		qparts = append(qparts, "-label.flags.channelrequired")
	}

	if c.Region != evr.Symbol(0) {
		qparts = append(qparts, "+label.region:"+c.Region.String())
	} else {
		qparts = append(qparts, "-label.flags.regionrequired")
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
func (p *EvrPipeline) MatchSearch(ctx context.Context, logger *zap.Logger, msession *MatchmakingSession) ([]*MatchLabel, string, error) {

	criteria := MatchCriteria{
		Open:                  true,
		Public:                true,
		VersionLock:           msession.MatchCriteria.VersionLock,
		Mode:                  msession.MatchCriteria.Mode,
		Level:                 msession.MatchCriteria.Level,
		Channels:              msession.Presence.GetChannels(),
		Region:                msession.MatchCriteria.Region,
		BackfillQueryAddon:    msession.GlobalSettings.BackfillQueryAddon + " " + msession.UserSettings.BackfillQueryAddon,
		MatchmakingQueryAddon: msession.GlobalSettings.MatchmakingQueryAddon + " " + msession.UserSettings.MatchmakingQueryAddon,
	}

	backfillQuery := criteria.GetBackfillQuery()
	// Basic search defaults
	const (
		minSize = 1
		limit   = 100
	)
	partySize := 1
	if msession.Party != nil {
		// If the user is in a party, the party size is the party size
		partySize = msession.Party.members.Size()
	}

	maxSize := MatchMaxSize - partySize // the broadcaster is included, so this has free spots
	logger = logger.With(zap.String("backfill_query", backfillQuery), zap.Any("criteria", criteria))

	// Search for possible matches
	matches, err := listMatches(ctx, p, limit, minSize, maxSize, msession.MatchCriteria.GetBackfillQuery()) // +1 for the broadcaster
	if err != nil {
		return nil, "", status.Errorf(codes.Internal, "Failed to find matches: %v", err)
	}

	// Create a label slice of the matches
	labels := make([]*MatchLabel, len(matches))
	for i, match := range matches {
		label := &MatchLabel{}
		if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
			logger.Error("Error unmarshalling match label", zap.Error(err))
			continue
		}
		labels[i] = label
	}

	return labels, backfillQuery, nil
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
func (p *EvrPipeline) MatchSort(ctx context.Context, msession *MatchmakingSession, labels []*MatchLabel) (filtered []*MatchLabel, rtts []time.Duration, err error) {
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
	result, err := msession.PingEndpoints(endpoints)
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
		datas = append(datas, labelData{id, label.GetPlayerCount(), rtt})
	}

	// Sort the matches
	switch msession.MatchCriteria.Mode {
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
	filtered = make([]*MatchLabel, 0, len(datas))
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
func (p *EvrPipeline) MatchCreate(ctx context.Context, msession *MatchmakingSession, matchSettings MatchSettings) (matchId MatchID, err error) {
	return MatchID{}, nil
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
	match, _, err := p.matchRegistry.GetMatch(ctx, matchID.String())
	if err != nil {
		return fmt.Errorf("failed to get match %s to join: %w", matchID, err)
	}
	if match == nil {
		return fmt.Errorf("match not found: %s", matchID)
	}

	label := &MatchLabel{}
	if err := json.Unmarshal([]byte(match.GetLabel().GetValue()), label); err != nil {
		return fmt.Errorf("failed to unmarshal match label: %w", err)
	}

	groupID := uuid.Nil
	// Get the channel
	if label.Metadata.Channel != uuid.Nil {
		groupID = uuid.FromStringOrNil((label.Metadata.Channel).String())
	}

	// Determine the display name
	displayName, err := SetDisplayNameByChannelBySession(ctx, p.runtimeModule, NewRuntimeGoLogger(logger), p.discordRegistry, session.userID.String(), groupID.String())
	if err != nil {
		logger.Warn("Failed to set display name.", zap.Error(err))
	}

	// If this is a NoVR user, give the profile's displayName a bot suffix
	// Get the NoVR key from context
	if flags, ok := ctx.Value(ctxFlagsKey{}).(SessionFlags); ok {
		if flags.IsNoVR {
			displayName = fmt.Sprintf("%s [NoVR]", displayName)
		}
	}

	// Set the profile's display name.
	profile, found := p.profileRegistry.Load(session.UserID(), evrID)
	if !found {
		defer session.Close("profile not found", runtime.PresenceReasonUnknown)
		return fmt.Errorf("profile not found: %s", session.UserID())
	}

	profile.SetDisplayName(displayName)
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

	mp := PlayerPresence{
		Node:        p.node,
		UserID:      session.userID,
		SessionID:   session.id,
		Username:    session.Username(),
		DisplayName: displayName,
		EvrID:       evrID,
		Alignment:   evr.Role(alignment),
		DiscordID:   discordID,

		ClientIP: session.clientIP,
		PartyID:  groupID,
	}

	// Marshal the player metadata into JSON.
	jsonMeta, err := json.Marshal(mp)
	if err != nil {
		return fmt.Errorf("failed to marshal player meta: %w", err)
	}
	metadata := map[string]string{"playermeta": string(jsonMeta)}
	// Do the join attempt to avoid race conditions
	found, allowed, _, reason, _, _ := p.matchRegistry.JoinAttempt(ctx, matchID.uuid, p.node, session.UserID(), session.ID(), session.Username(), session.Expiry(), session.Vars(), session.clientIP, session.clientPort, p.node, metadata)
	if !found {
		return fmt.Errorf("match not found: %s", matchID.String())
	}
	if !allowed {
		switch reason {
		case ErrJoinRejectedDuplicateJoin.Error():
			return status.Errorf(codes.AlreadyExists, "join not allowed: %s", reason)
		case ErrJoinRejectedLobbyFull.Error():
			return status.Errorf(codes.ResourceExhausted, "join not allowed: %s", reason)
		default:
			return status.Errorf(codes.Internal, "join not allowed: %s", reason)
		}
	}

	p.matchBySessionID.Store(session.ID().String(), matchID)
	p.matchByEvrID.Store(evrID.Token(), matchID)

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

	// Get channel memberships from the context
	memberships, ok := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	if !ok {
		return nil, fmt.Errorf("channel memberships not found in context")
	}

	for _, m := range memberships {
		if m.ChannelID == channel && m.isSuspended {

			// Get the guild
			guild, _, err := p.discordRegistry.GetGuildByGroupId(ctx, m.ChannelID)
			if err != nil {
				return nil, fmt.Errorf("failed to get guild: %w", err)
			}
			if guild == nil {
				return nil, fmt.Errorf("guild not found")
			}
			// Get teh user's discord ID
			discordID, err := p.discordRegistry.GetDiscordIdByUserId(ctx, uuid.FromStringOrNil(userID))
			if err != nil {
				return nil, fmt.Errorf("failed to get discord id: %w", err)
			}

			// Check if the user is suspended from the channel
			if m.isSuspended {
				return []*SuspensionStatus{
					{
						GuildID:       guild.ID,
						GuildName:     guild.Name,
						UserID:        userID,
						UserDiscordID: discordID,
						Reason:        "You are suspended from this channel.\nContact a moderator for more information.",
					},
				}, nil
			}
		}
	}

	return nil, nil
}
