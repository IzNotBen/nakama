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
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	MatchJoinGracePeriod        = 10 * time.Second
	MatchmakingStartGracePeriod = 3 * time.Second
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
	if len(ml.Broadcaster.GroupIDs) > 0 {
		// Add the channels to the query
		qparts = append(qparts, HostedChannels(ml.Broadcaster.GroupIDs).Query(Must, 0))
	}

	// Add each hosted channel as a SHOULD, with decreasing boost

	for i, channel := range ml.Broadcaster.GroupIDs {
		qparts = append(qparts, GroupID(channel).Query(Should, len(ml.Broadcaster.GroupIDs)-i))
	}

	// Add the regions in descending order of priority
	for i, region := range ml.Broadcaster.Regions {
		qparts = append(qparts, Region(region).Query(Should, len(ml.Broadcaster.Regions)-i))
	}

	// Add tag query for prioritizing certain modes to specific hosts
	// remove dots from the mode string to avoid issues with the query parser
	s := strings.NewReplacer(".", "").Replace(ml.Mode.String())
	qparts = append(qparts, "label.broadcaster.tags:priority_mode_"+s+"^10")

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

	gconfig, err := LoadMatchmakingSettings(ctx, p.runtimeModule, SystemUserID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load global matchmaking config: %v", err)
	}

	config, err := LoadMatchmakingSettings(ctx, p.runtimeModule, session.UserID().String())
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

type LabelLatencies struct {
	label   *EvrMatchState
	latency *LatencyMetric
}

func (p *EvrPipeline) Backfill(ctx context.Context, session *sessionWS, msession *MatchmakingSession) ([]*EvrMatchState, string, error) {

	logger := msession.Logger

	mode := msession.Label.Mode
	groupID := *msession.Label.GroupID

	labels, query, err := p.matchmakingRegistry.listUnfilledLobbies(ctx, logger, mode, groupID)
	if err != nil {
		return nil, query, err

	}
	if len(labels) == 0 {
		return nil, query, nil
	}
	partySize := 1
	if msession.Party != nil {
		partySize = msession.Party.Size()
	}

	candidates := make([]*EvrMatchState, 0)
	isPlayer := msession.Label.TeamIndex != Spectator && msession.Label.TeamIndex != Moderator
	for _, label := range labels {
		if isPlayer {
			if label.GetAvailablePlayerSlots() < partySize {
				continue
			}
		} else if label.GetAvailableNonPlayerSlots() < partySize {
			continue
		}

		candidates = append(candidates, label)
	}

	endpoints := make([]evr.Endpoint, 0, len(candidates))
	for _, label := range candidates {
		endpoints = append(endpoints, label.Broadcaster.Endpoint)
	}

	// Ping the endpoints
	latencies, err := p.GetLatencyMetricByEndpoint(ctx, msession, endpoints, true)
	if err != nil {
		return nil, query, err
	}

	labelLatencies := make([]LabelLatencies, 0, len(candidates))
	for _, label := range labels {
		for _, latency := range latencies {
			if label.Broadcaster.Endpoint.GetExternalIP() == latency.Endpoint.GetExternalIP() {
				labelLatencies = append(labelLatencies, LabelLatencies{label, &latency})
				break
			}
		}
	}

	var sortFn func(i, j time.Duration, o, p int) bool
	switch msession.Label.Mode {
	case evr.ModeArenaPublic:
		sortFn = RTTweightedPopulationCmp
	default:
		sortFn = PopulationCmp
	}

	sort.SliceStable(labelLatencies, func(i, j int) bool {
		return sortFn(labelLatencies[i].latency.RTT, labelLatencies[j].latency.RTT, labelLatencies[i].label.PlayerCount, labelLatencies[j].label.PlayerCount)
	})

	return candidates, query, nil
}

type BroadcasterLatencies struct {
	Endpoint evr.Endpoint
	Latency  time.Duration
}

// Matchmake attempts to find/create a match for the user using the nakama matchmaker
func (p *EvrPipeline) MatchMake(session *sessionWS, msession *MatchmakingSession) (ticket string, err error) {
	// TODO Move this into the matchmaking registry
	ctx := msession.Context()
	// TODO FIXME Add a custom matcher for broadcaster matching
	// Get a list of all the broadcasters
	endpoints := make([]evr.Endpoint, 0, 100)
	p.broadcasterRegistrationBySession.Range(func(_ string, b *MatchBroadcaster) bool {
		endpoints = append(endpoints, b.Endpoint)
		return true
	})

	allRTTs, err := p.GetLatencyMetricByEndpoint(ctx, msession, endpoints, true)
	if err != nil {
		return "", err
	}
	// Get the EVR ID from the context
	evrID, ok := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	if !ok {
		return "", status.Errorf(codes.Internal, "EVR ID not found in context")
	}

	query, stringProps, numericProps, err := msession.BuildQuery(allRTTs, evrID)
	if err != nil {
		return "", status.Errorf(codes.Internal, "Failed to build matchmaking query: %v", err)
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
	// Load the global matchmaking config
	gconfig, err := LoadMatchmakingSettings(ctx, p.runtimeModule, SystemUserID)
	if err != nil {
		return "", status.Errorf(codes.Internal, "Failed to load global matchmaking config: %v", err)
	}

	// Load the user's matchmaking config
	config, err := LoadMatchmakingSettings(ctx, p.runtimeModule, userID)
	if err != nil {
		return "", status.Errorf(codes.Internal, "Failed to load matchmaking config: %v", err)
	}
	// Merge the user's config with the global config
	query = fmt.Sprintf("%s %s %s", query, gconfig.BackfillQueryAddon, config.BackfillQueryAddon)

	minCount := 2
	maxCount := 8
	if msession.Label.Mode == evr.ModeCombatPublic {
		maxCount = 10
	} else {
		maxCount = 8
	}
	countMultiple := 2

	// Create a status presence for the user
	stream := PresenceStream{Mode: StreamModeMatchmaking, Subject: uuid.NewV5(uuid.Nil, "matchmaking")}
	meta := PresenceMeta{Format: session.format, Username: session.Username(), Status: query, Hidden: true}
	ok = session.tracker.Update(ctx, session.id, stream, session.userID, meta)
	if !ok {
		return "", status.Errorf(codes.Internal, "Failed to track user: %v", err)
	}
	tags := map[string]string{
		"type":  msession.Label.LobbyType.String(),
		"mode":  msession.Label.Mode.String(),
		"level": msession.Label.Level.String(),
	}

	p.metrics.CustomCounter("matchmaker_tickets_count", tags, 1)
	// Add the user to the matchmaker
	pID := ""
	if msession.Party != nil {
		pID = msession.Party.ID().String()
		stringProps["party_group"] = config.GroupID
		query = fmt.Sprintf("%s properties.party_group:%s^5", query, config.GroupID)
	}
	msession.Logger.Debug("Adding matchmaking ticket", zap.String("query", query), zap.String("party_id", pID))
	ticket, _, err = session.matchmaker.Add(ctx, presences, sessionID.String(), pID, query, minCount, maxCount, countMultiple, stringProps, numericProps)
	if err != nil {
		return "", fmt.Errorf("failed to add to matchmaker with query `%s`: %v", query, err)
	}
	msession.AddTicket(ticket, query)
	return ticket, nil
}

// Wrapper for the matchRegistry.ListMatches function.
func listMatches(ctx context.Context, p *EvrPipeline, limit int, minSize int, maxSize int, query string) ([]*api.Match, error) {
	return p.runtimeModule.MatchList(ctx, limit, true, "", &minSize, &maxSize, query)
}

// mroundRTT rounds the rtt to the nearest modulus
func mroundRTT(rtt time.Duration, modulus time.Duration) time.Duration {
	if rtt == 0 {
		return 0
	}
	if rtt < modulus {
		return rtt
	}
	r := float64(rtt) / float64(modulus)
	return time.Duration(math.Round(r)) * modulus
}

// RTTweightedPopulationCmp compares two RTTs and populations
func RTTweightedPopulationCmp(i, j time.Duration, o, p int) bool {
	if i == 0 && j != 0 {
		return false
	}

	// Sort by if over or under 90ms
	if i < 90*time.Millisecond && j > 90*time.Millisecond {
		return true
	}
	if i > 90*time.Millisecond && j < 90*time.Millisecond {
		return false
	}

	// Sort by Population
	if o != p {
		return o > p
	}

	// If all else equal, sort by rtt
	return i < j
}

// PopulationCmp compares two populations
func PopulationCmp(i, j time.Duration, o, p int) bool {
	if o == p {
		// If all else equal, sort by rtt
		return i != 0 && i < j
	}
	return o > p
}

// LatencyCmp compares by latency, round to the nearest 10ms
func LatencyCmp(i, j time.Duration) bool {
	// Round to the closest 10ms
	i = mroundRTT(i, 10*time.Millisecond)
	j = mroundRTT(j, 10*time.Millisecond)
	return i < j
}

// TODO FIXME This need to use allocateBroadcaster instad.
// MatchCreate creates a match on an available unassigned broadcaster using the given label
func (p *EvrPipeline) MatchCreate(ctx context.Context, session *sessionWS, msession *MatchmakingSession, ml *EvrMatchState) (matchID MatchID, err error) {
	ml.MaxSize = MatchMaxSize
	// Lock the broadcaster's until the match is created
	p.matchmakingRegistry.Lock()

	defer func() {
		// Hold onto the lock for one extra second
		<-time.After(1 * time.Second)
		p.matchmakingRegistry.Unlock()
	}()

	select {
	case <-msession.Ctx.Done():
		return MatchID{}, nil
	default:
	}

	// TODO Move this into the matchmaking registry
	// Create a new match
	labels, err := p.ListUnassignedLobbies(ctx, session, ml)
	if err != nil {
		return MatchID{}, err
	}

	if len(labels) == 0 {
		return MatchID{}, ErrMatchmakingNoAvailableServers
	}

	endpoints := make([]evr.Endpoint, 0, len(labels))
	for _, label := range labels {
		endpoints = append(endpoints, label.Broadcaster.Endpoint)
	}

	// Ping the endpoints
	latencies, err := p.GetLatencyMetricByEndpoint(ctx, msession, endpoints, true)
	if err != nil {
		return MatchID{}, err
	}

	labelLatencies := make([]LabelLatencies, 0, len(labels))
	for _, label := range labels {
		for _, latency := range latencies {
			if label.Broadcaster.Endpoint.GetExternalIP() == latency.Endpoint.GetExternalIP() {
				labelLatencies = append(labelLatencies, LabelLatencies{label, &latency})
				break
			}
		}
	}
	region := ml.Broadcaster.Regions[0]
	sort.SliceStable(labelLatencies, func(i, j int) bool {
		// Sort by region, then by latency (zero'd RTTs at the end)
		if labelLatencies[i].label.Broadcaster.Regions[0] == region && labelLatencies[j].label.Broadcaster.Regions[0] != region {
			return true
		}
		if labelLatencies[i].label.Broadcaster.Regions[0] != region && labelLatencies[j].label.Broadcaster.Regions[0] == region {
			return false
		}
		if labelLatencies[i].latency.RTT == 0 && labelLatencies[j].latency.RTT != 0 {
			return false
		}
		if labelLatencies[i].latency.RTT != 0 && labelLatencies[j].latency.RTT == 0 {
			return true
		}
		return LatencyCmp(labelLatencies[i].latency.RTT, labelLatencies[j].latency.RTT)
	})

	matchID = labelLatencies[0].label.ID

	ml.SpawnedBy = session.UserID().String()

	// Prepare the match
	response, err := SignalMatch(ctx, p.matchRegistry, matchID, SignalPrepareSession, ml)
	if err != nil {
		return MatchID{}, ErrMatchmakingUnknownError
	}

	msession.Logger.Info("Match created", zap.String("match_id", matchID.String()), zap.String("label", response))

	// Return the prepared session
	return matchID, nil
}

// JoinEvrMatch allows a player to join a match.
func (p *EvrPipeline) JoinEvrMatch(ctx context.Context, logger *zap.Logger, session *sessionWS, query string, matchID MatchID, teamIndex int) error {
	// Append the node to the matchID if it doesn't already contain one.

	label, err := MatchLabelByID(ctx, p.runtimeModule, matchID)
	if err != nil {
		return fmt.Errorf("failed to get match label: %w", err)
	}

	partyID := uuid.Nil
	msession, ok := p.matchmakingRegistry.GetMatchingBySessionId(session.ID())
	if ok && msession != nil && msession.Party != nil {
		partyID = msession.Party.ID()
	}

	evrID := ctx.Value(ctxEvrIDKey{}).(evr.EvrID)
	profile, _ := p.profileRegistry.Load(session.userID, evrID)

	// Add the user's profile to the cache (by EvrID)
	err = p.profileCache.Add(matchID, evrID, profile.GetServer())
	if err != nil {
		logger.Warn("Failed to add profile to cache", zap.Error(err))
	}

	memberships := ctx.Value(ctxMembershipsKey{}).([]GuildGroupMembership)

	membership := memberships[0]
	for _, m := range memberships {
		if membership.GuildGroup.ID() == label.GetGroupID() {
			membership = m
			break
		}
	}

	// Prepare the player session metadata.
	discordID, err := GetDiscordIDByUserID(ctx, p.db, session.userID.String())
	if err != nil {
		logger.Error("Failed to get discord id", zap.Error(err))
	}
	loginSession := ctx.Value(ctxLoginSessionKey{}).(*sessionWS)

	mp := &EntrantPresence{

		Node:           p.node,
		UserID:         session.userID,
		SessionID:      session.id,
		LoginSessionID: loginSession.id,
		Username:       session.Username(),
		DisplayName:    membership.DisplayName.String(),
		EvrID:          evrID,
		PartyID:        partyID,
		RoleAlignment:  int(teamIndex),
		DiscordID:      discordID,
		ClientIP:       session.clientIP,
		ClientPort:     session.clientPort,
	}

	logger.Debug("Joining match", zap.String("mid", matchID.UUID().String()))
	label, mp, _, err = EVRMatchJoinAttempt(ctx, logger, matchID, p.sessionRegistry, p.matchRegistry, p.tracker, *mp, nil)
	if err != nil {
		if err == ErrDuplicateJoin {
			logger.Warn("Player already in match. Ignoring join attempt.", zap.String("mid", matchID.UUID().String()), zap.Error(err))
			if msession != nil {
				msession.Cancel(err)
			}
			return nil
		} else {
			return fmt.Errorf("failed to join match: %w", err)
		}
	}

	// Get the broadcasters session
	bsession := p.sessionRegistry.Get(uuid.FromStringOrNil(label.Broadcaster.SessionID)).(*sessionWS)
	if bsession == nil {
		return fmt.Errorf("broadcaster session not found: %s", label.Broadcaster.SessionID)
	}

	// Send the lobbysessionSuccess, this will trigger the broadcaster to send a lobbysessionplayeraccept once the player connects to the broadcaster.
	msg := evr.NewLobbySessionSuccess(label.Mode, label.ID.UUID(), label.GetGroupID(), label.GetEndpoint(), int16(mp.RoleAlignment))
	messages := []evr.Message{msg.Version4(), msg.Version5(), evr.NewSTcpConnectionUnrequireEvent()}

	if err = bsession.SendEVR(messages...); err != nil {
		return fmt.Errorf("failed to send messages to broadcaster: %w", err)
	}

	if err = session.SendEVR(messages...); err != nil {
		err = fmt.Errorf("failed to send messages to player: %w", err)
	}
	if msession != nil {
		msession.Cancel(err)
	}
	return err
}

var ErrDuplicateJoin = errors.New(JoinRejectReasonDuplicateJoin)

func EVRMatchJoinAttempt(ctx context.Context, logger *zap.Logger, matchID MatchID, sessionRegistry SessionRegistry, matchRegistry MatchRegistry, tracker Tracker, presence EntrantPresence, partyMembers []rtapi.UserPresence) (*EvrMatchState, *EntrantPresence, []*MatchPresence, error) {

	matchIDStr := matchID.String()
	metadata := JoinMetadata{Presence: presence, PartyPresences: partyMembers}.MarshalMap()

	found, allowed, isNew, reason, labelStr, presences := matchRegistry.JoinAttempt(ctx, matchID.UUID(), matchID.Node(), presence.UserID, presence.SessionID, presence.Username, presence.SessionExpiry, nil, presence.ClientIP, presence.ClientPort, matchID.Node(), metadata)
	if !found {
		return nil, nil, nil, fmt.Errorf("match not found: %s", matchIDStr)
	} else if labelStr == "" {
		return nil, nil, nil, fmt.Errorf("match label not found: %s", matchIDStr)
	}

	label := &EvrMatchState{}
	if err := json.Unmarshal([]byte(labelStr), label); err != nil {
		return nil, nil, presences, fmt.Errorf("failed to unmarshal match label: %w", err)
	}

	if !allowed {
		return label, nil, presences, fmt.Errorf("join not allowed: %s", reason)
	} else if !isNew {
		return label, nil, presences, ErrDuplicateJoin
	}
	entrantID := uuid.NewV5(matchID.uuid, presence.EvrID.String())
	presence = EntrantPresence{}
	if err := json.Unmarshal([]byte(reason), &presence); err != nil {
		return label, nil, presences, fmt.Errorf("failed to unmarshal attempt response: %w", err)
	}

	ops := []*TrackerOp{
		{
			PresenceStream{Mode: StreamModeEntrant, Subject: matchID.uuid, Subcontext: entrantID, Label: matchID.node},
			PresenceMeta{Format: SessionFormatEvr, Username: presence.Username, Status: presence.String(), Hidden: true},
		},
		{
			PresenceStream{Mode: StreamModeService, Subject: presence.SessionID, Label: StreamLabelMatchService},
			PresenceMeta{Format: SessionFormatEvr, Username: presence.Username, Status: matchIDStr, Hidden: true},
		},
		{
			PresenceStream{Mode: StreamModeService, Subject: presence.LoginSessionID, Label: StreamLabelMatchService},
			PresenceMeta{Format: SessionFormatEvr, Username: presence.Username, Status: matchIDStr, Hidden: true},
		},
		{
			PresenceStream{Mode: StreamModeService, Subject: presence.UserID, Label: StreamLabelMatchService},
			PresenceMeta{Format: SessionFormatEvr, Username: presence.Username, Status: matchIDStr, Hidden: true},
		},
		{
			PresenceStream{Mode: StreamModeService, Subject: presence.EvrID.UUID(), Label: StreamLabelMatchService},
			PresenceMeta{Format: SessionFormatEvr, Username: presence.Username, Status: matchIDStr, Hidden: true},
		},
	}

	// Update the statuses
	for _, op := range ops {
		if ok := tracker.Update(ctx, presence.SessionID, op.Stream, presence.UserID, op.Meta); !ok {
			return label, nil, presences, fmt.Errorf("failed to track session ID: %s", presence.SessionID)
		}
	}

	return label, &presence, presences, nil
}

// PingEndpoints pings the endpoints and returns the latencies.
func (p *EvrPipeline) GetLatencyMetricByEndpoint(ctx context.Context, msession *MatchmakingSession, endpoints []evr.Endpoint, update bool) ([]LatencyMetric, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	logger := msession.Logger

	if update {
		// Get the candidates for pinging
		candidates := msession.GetPingCandidates(endpoints...)
		if len(candidates) > 0 {
			if err := p.sendPingRequest(logger, msession.Session, candidates); err != nil {
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

				p.broadcasterRegistrationBySession.Range(func(_ string, b *MatchBroadcaster) bool {
					for _, response := range results {
						if b.Endpoint.ExternalIP.Equal(response.ExternalIP) && b.Endpoint.InternalIP.Equal(response.InternalIP) {
							r := LatencyMetric{
								Endpoint:  b.Endpoint,
								RTT:       response.RTT(),
								Timestamp: time.Now(),
							}

							cache.Store(b.Endpoint.GetExternalIP(), r)
						}
					}
					return true
				})

			}
		}
	}

	results := p.matchmakingRegistry.GetLatencies(msession.UserId, endpoints)
	return results, nil
}

// sendPingRequest sends a ping request to the given candidates.
func (p *EvrPipeline) sendPingRequest(logger *zap.Logger, session *sessionWS, candidates []evr.Endpoint) error {

	if err := session.SendEVR(
		evr.NewLobbyPingRequest(275, candidates),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return err
	}

	logger.Debug("Sent ping request", zap.Any("candidates", candidates))
	return nil
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
		return nil, status.Errorf(codes.Internal, "Failed to get guild member: %v", err)
	}

	if member == nil {
		return nil, status.Errorf(codes.Internal, "Member is nil for discordId: %s", discordId)
	}

	if !slices.Contains(member.Roles, md.SuspensionRole) {
		return nil, nil
	}

	// TODO FIXME This needs to be refactored. extract method.
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

// selectTeamForPlayer decides which team27 to assign a player to.
func selectTeamForPlayer(presence *EntrantPresence, state *EvrMatchState) (int, bool) {
	t := presence.RoleAlignment

	teams := lo.GroupBy(lo.Values(state.presences), func(p *EntrantPresence) int { return p.RoleAlignment })

	blueTeam := teams[evr.TeamBlue]
	orangeTeam := teams[evr.TeamOrange]
	playerpop := len(blueTeam) + len(orangeTeam)
	spectators := len(teams[evr.TeamSpectator]) + len(teams[evr.TeamModerator])
	teamsFull := playerpop >= state.TeamSize*2
	specsFull := spectators >= int(state.MaxSize)-state.TeamSize*2

	// If the lobby is full, reject
	if len(state.presences) >= int(state.MaxSize) {
		return evr.TeamUnassigned, false
	}

	// If the player is a moderator and the spectators are not full, return
	if t == evr.TeamModerator && !specsFull {
		return t, true
	}

	// If the match has been running for less than 15 seconds check the presets for the team
	if time.Since(state.StartTime) < 15*time.Second {
		if teamIndex, ok := state.TeamAlignments[presence.GetUserId()]; ok {
			// Make sure the team isn't already full
			if len(teams[teamIndex]) < state.TeamSize {
				return teamIndex, true
			}
		}
	}

	// If this is a social lobby, put the player on the social team.
	if state.Mode == evr.ModeSocialPublic || state.Mode == evr.ModeSocialPrivate {
		return evr.TeamSocial, true
	}

	// If the player is unassigned, assign them to a team.
	if t == evr.TeamUnassigned {
		t = evr.TeamBlue
	}

	// If this is a private lobby, and the teams are full, put them on spectator
	if t != evr.TeamSpectator && teamsFull {
		if state.LobbyType == PrivateLobby {
			t = evr.TeamSpectator
		} else {
			// The lobby is full, reject the player.
			return evr.TeamUnassigned, false
		}
	}

	// If this is a spectator, and the spectators are not full, return
	if t == evr.TeamSpectator {
		if specsFull {
			return evr.TeamUnassigned, false
		}
		return t, true
	}
	// If this is a private lobby, and their team is not full,  put the player on the team they requested
	if state.LobbyType == PrivateLobby && len(teams[t]) < state.TeamSize {
		return t, true
	}

	// If the players team is unbalanced, put them on the other team
	if len(teams[evr.TeamBlue]) != len(teams[evr.TeamOrange]) {
		if len(teams[evr.TeamBlue]) < len(teams[evr.TeamOrange]) {
			t = evr.TeamBlue
		} else {
			t = evr.TeamOrange
		}
	}

	return t, true
}

func (p *EvrPipeline) MatchSpectateStreamLoop(session *sessionWS, msession *MatchmakingSession, skipDelay bool, create bool) error {
	logger := msession.Logger
	ctx := msession.Context()
	p.metrics.CustomCounter("spectatestream_active_count", msession.metricsTags(), 1)

	limit := 100
	minSize := 1
	maxSize := MatchMaxSize - 1
	query := fmt.Sprintf("+label.open:T +label.lobby_type:public +label.mode:%s +label.size:>=%d +label.size:<=%d", msession.Label.Mode.Token(), minSize, maxSize)
	// creeate a delay timer
	timer := time.NewTimer(0 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		case <-ticker.C:
		}

		// list existing matches
		matches, err := listMatches(ctx, p, limit, minSize+1, maxSize+1, query)
		if err != nil {
			return msession.Cancel(fmt.Errorf("failed to find spectate match: %w", err))
		}

		if len(matches) != 0 {
			// sort matches by population
			sort.SliceStable(matches, func(i, j int) bool {
				// Sort by newer matches
				return matches[i].Size > matches[j].Size
			})

			if err := p.JoinEvrMatch(msession.Ctx, msession.Logger, msession.Session, "", MatchIDFromStringOrNil(matches[0].GetMatchId()), int(evr.TeamSpectator)); err != nil {
				logger.Error("Error joining player to match", zap.Error(err))
			}
		}

	}
}

func (p *EvrPipeline) MatchBackfillLoop(session *sessionWS, msession *MatchmakingSession) error {
	logger := msession.Logger
	ctx := msession.Context()

	// Wait for at least 1 interval before starting to look for a backfill.
	// This gives the matchmaker a chance to find a full ideal match
	// interval := p.config.GetMatchmaker().IntervalSec
	//idealMatchIntervals := p.config.GetMatchmaker().RevThreshold
	//backfillDelay := time.Duration(interval*idealMatchIntervals) * time.Second

	delayStartTimer := time.NewTimer(100 * time.Millisecond)
	retryTicker := time.NewTicker(4 * time.Second)
	createTicker := time.NewTicker(6 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-msession.Ctx.Done():
			return nil
		case <-delayStartTimer.C:
		case <-retryTicker.C:
		}

		// Backfill any existing matches
		labels, query, err := p.Backfill(ctx, session, msession)
		if err != nil {
			return msession.Cancel(fmt.Errorf("failed to find backfill match: %w", err))
		}

		for _, label := range labels {

			select {
			case <-ctx.Done():
				return nil
			case <-msession.Ctx.Done():
				return nil
			default:
			}
			// Lock the match
			p.matchMutexs.Get(label.ID.UUID().String()).Lock()
			// Get the latest match state
			label, err := MatchLabelByID(ctx, p.runtimeModule, label.ID)
			if err != nil {
				p.matchMutexs.Get(label.ID.UUID().String()).Unlock()
				return msession.Cancel(fmt.Errorf("failed to get match label: %w", err))
			}
			if label.GetAvailablePlayerSlots() < msession.Party.Size() {
				p.matchMutexs.Get(label.ID.UUID().String()).Unlock()
				continue
			}

			p.metrics.CustomCounter("match_backfill_found_count", msession.metricsTags(), 1)
			logger.Debug("Attempting to backfill match", zap.String("mid", label.ID.UUID().String()))

			// Join all the party members to the match
			for _, presence := range msession.Party.GetMembers() {
				ms, ok := p.matchmakingRegistry.GetMatchingBySessionId(uuid.FromStringOrNil(presence.Presence.GetSessionId()))
				if !ok {
					logger.Debug("Session not found in matchmaking registry", zap.String("sid", presence.Presence.GetSessionId()))
					continue
				}
				if err := p.JoinEvrMatch(ctx, ms.Logger, ms.Session, query, label.ID, evr.TeamUnassigned); err != nil {
					msession.Logger.Warn("Failed to backfill match", zap.Error(err))
					continue
				}
			}

			// If the backfill was successful, stop the backfill loop
			return nil
		}
		// Only social lobbies get created
		if msession.Label.Mode == evr.ModeSocialPublic {
			select {
			case <-createTicker.C:
				if p.createLobbyMu.TryLock() {
					if _, err := p.MatchCreate(ctx, session, msession, msession.Label); err != nil {
						logger.Warn("Failed to create match", zap.Error(err))
					}
					p.createLobbyMu.Unlock()
				}
			default:
			}
		}
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
			if matchID.IsNil() {
				return msession.Cancel(fmt.Errorf("match is nil"))
			}
			p.metrics.CustomCounter("match_create_join_active_count", msession.metricsTags(), 1)
			if err := p.JoinEvrMatch(msession.Ctx, msession.Logger, msession.Session, "", matchID, int(msession.Label.TeamIndex)); err != nil {
				logger.Warn("Error joining player to match", zap.Error(err))
			}

			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
				p.metrics.CustomCounter("make_create_create_join_timeout_count", msession.metricsTags(), 1)
			}

			// Keep trying until the context is done
		case codes.NotFound, codes.ResourceExhausted, codes.Unavailable:
			p.metrics.CustomCounter("create_unavailable_count", msession.metricsTags(), 1)
		case codes.Unknown, codes.Internal:
			logger.Warn("Failed to create match", zap.Error(err))
		default:
			return msession.Cancel(err)
		}
		<-time.After(10 * time.Second)
	}
}

func (p *EvrPipeline) MatchFind(parentCtx context.Context, logger *zap.Logger, session *sessionWS, ml *EvrMatchState) error {
	if s, found := p.matchmakingRegistry.GetMatchingBySessionId(session.id); found {
		// Replace the session
		logger.Debug("Matchmaking session already exists", zap.Any("tickets", s.Tickets))
	}

	// Create a new matching session
	logger.Debug("Creating a new matchmaking session")
	partySize := 1
	timeout := 60 * time.Minute

	if ml.TeamIndex == TeamIndex(evr.TeamSpectator) {
		timeout = 12 * time.Hour
	}

	msession, err := p.matchmakingRegistry.Create(parentCtx, logger, session, ml, partySize, timeout)
	if err != nil {
		logger.Error("Failed to create matchmaking session", zap.Error(err))
		return err
	}
	p.metrics.CustomCounter("match_find_active_count", msession.metricsTags(), 1)
	// Load the user's matchmaking config

	config, err := LoadMatchmakingSettings(msession.Ctx, p.runtimeModule, session.userID.String())
	if err != nil {
		logger.Error("Failed to load matchmaking config", zap.Error(err))
	}

	var matchID MatchID
	// Check for a direct match first
	if !config.NextMatchID.IsNil() {
		matchID = config.NextMatchID
	}

	config.NextMatchID = MatchID{}
	err = StoreMatchmakingSettings(msession.Ctx, p.runtimeModule, session.userID.String(), config)
	if err != nil {
		logger.Error("Failed to save matchmaking config", zap.Error(err))
	}

	if !matchID.IsNil() {
		logger.Debug("Attempting to join match from settings", zap.String("mid", matchID.String()))
		match, _, err := p.matchRegistry.GetMatch(msession.Ctx, matchID.String())
		if err != nil {
			logger.Error("Failed to get match", zap.Error(err))
		} else {
			if match == nil {
				logger.Warn("Match not found", zap.String("mid", matchID.String()))
			} else {
				p.metrics.CustomCounter("match_next_join_count", map[string]string{}, 1)
				// Join the match

				if err := p.JoinEvrMatch(msession.Ctx, msession.Logger, msession.Session, "", matchID, int(evr.TeamUnassigned)); err != nil {
					logger.Warn("Error joining player to match", zap.Error(err))
				}

				select {
				case <-msession.Ctx.Done():
					return nil
				case <-time.After(3 * time.Second):
				}
			}
		}
	}
	logger = msession.Logger

	skipBackfillDelay := false

	if ml.TeamIndex == TeamIndex(evr.TeamModerator) {
		skipBackfillDelay = true
		// Check that the user is a moderator for this channel, or globally
		guild, err := p.discordRegistry.GetGuildByGroupId(parentCtx, ml.GroupID.String())
		if err != nil || guild == nil {
			logger.Warn("failed to get guild: %v", zap.Error(err))
			ml.TeamIndex = TeamIndex(evr.TeamSpectator)
		} else {
			discordID, err := GetDiscordIDByUserID(parentCtx, p.db, session.userID.String())
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

	// Join party

	partyGroupID := session.UserID().String()
	if config.GroupID != "" {
		partyGroupID = config.GroupID
	}

	partyID := uuid.NewV5(uuid.Nil, partyGroupID)
	if config.GroupID != "" {

		msession.Party, err = JoinPartyGroup(session, partyGroupID, partyID)
		if err != nil {
			logger.Warn("Failed to join party group", zap.String("group_id", config.GroupID), zap.Error(err))
		} else {
			logger.Debug("Joined party", zap.String("party_id", msession.Party.ID().String()), zap.Any("members", msession.Party.GetMembers()))
			logger = logger.With(zap.String("party_id", msession.Party.ID().String()))
		}
	}

	// Only the leader is allowed to matchmake
	if msession.Party == nil || msession.Party.IsLeader() {

		go p.MatchBackfillLoop(session, msession)

		// Put a ticket in for matching
		_, err := p.MatchMake(session, msession)
		if err != nil {
			return err
		}
	} else {
		// Set a timeout for the party leader to start matchmaking
		timeout := time.After(30 * time.Second)
		originalLeaderID := msession.Party.GetLeader().GetSessionId()
		for {
			select {
			case <-msession.Ctx.Done():
				return nil
			case <-timeout:
			case <-time.After(1 * time.Second):
				// Check if this player is not the leader
				if msession.Party.GetLeader().GetSessionId() != originalLeaderID {
					// Cancel matchmaking
					msession.Cancel(ErrMatchmakingPartyChanged)
				}
			}
		}
	}

	switch ml.Mode {
	// For public matches, backfill or matchmake
	// If it's a social match, backfill or create immediately
	case evr.ModeSocialPublic:
		// Continue to try to backfill

		go p.MatchBackfillLoop(session, msession)

	case evr.ModeArenaPublic:

		// Only the leader is allowed to matchmaking
		if msession.Party == nil || msession.Party.IsLeader() {

			// Don't backfill party members, let the matchmaker handle it.
			go p.MatchBackfillLoop(session, msession)

			// Put a ticket in for matching
			_, err := p.MatchMake(session, msession)
			if err != nil {
				return err
			}
		} else {
			// Set a timeout for the party leader to start matchmaking
			timeout := time.After(30 * time.Second)
			originalLeaderID := msession.Party.GetLeader().GetSessionId()
			for {
				select {
				case <-msession.Ctx.Done():
					return nil
				case <-timeout:
				case <-time.After(1 * time.Second):
					// Check if this player is not the leader
					if msession.Party.GetLeader().GetSessionId() != originalLeaderID {
						// Cancel matchmaking
						msession.Cancel(ErrMatchmakingPartyChanged)
					}
				}
			}
		}

		// For public arena/combat matches, backfill while matchmaking
	case evr.ModeCombatPublic:

		env := p.config.GetRuntime().Environment
		channelID, ok := env["COMBAT_MATCHMAKING_CHANNEL_ID"]
		if ok {

			if bot := p.discordRegistry.GetBot(); bot != nil && ml.TeamIndex != TeamIndex(evr.TeamSpectator) {
				// Count how many players are matchmaking for this mode right now
				sessionsByMode := p.matchmakingRegistry.SessionsByMode()

				userIDs := make([]uuid.UUID, 0)
				for _, s := range sessionsByMode[ml.Mode] {

					if s.Session.userID == session.userID || s.Label.TeamIndex == TeamIndex(evr.TeamSpectator) {
						continue
					}

					userIDs = append(userIDs, s.Session.userID)
				}

				matchID, _, err := GetMatchBySessionID(p.runtimeModule, session.id)
				if err != nil {
					logger.Warn("Failed to get match by session ID", zap.Error(err))
				}

				// Translate the userID's to discord ID's
				discordIDs := make([]string, 0, len(userIDs))
				for _, userID := range userIDs {

					account, err := p.runtimeModule.AccountGetId(parentCtx, userID.String())
					if err != nil {
						logger.Warn("Failed to get account", zap.Error(err))
					}
					discordIDs = append(discordIDs, account.GetUser().GetDisplayName())
				}

				account, err := p.runtimeModule.AccountGetId(parentCtx, session.userID.String())
				if err != nil {
					logger.Warn("Failed to get account", zap.Error(err))
				}
				// Get the user's displayname
				msg := fmt.Sprintf("*%d* is matchmaking...", account.GetUser().GetDisplayName())
				if len(discordIDs) > 0 {
					msg = fmt.Sprintf("%s along with %s...", msg, strings.Join(discordIDs, ", "))
				}

				embed := discordgo.MessageEmbed{
					Title:       "Matchmaking",
					Description: msg,
					Color:       0x00cc00,
				}

				if !matchID.IsNil() {
					embed.Footer = &discordgo.MessageEmbedFooter{
						Text: fmt.Sprintf("https://echo.taxi/spark://j/%s", strings.ToUpper(matchID.UUID().String())),
					}
				}

				// Notify the channel that this person started queuing
				message, err := bot.ChannelMessageSendEmbed(channelID, &embed)
				if err != nil {
					logger.Warn("Failed to send message", zap.Error(err))
				}
				go func() {
					// Delete the message when the player stops matchmaking
					select {
					case <-msession.Ctx.Done():
						if message != nil {
							err := bot.ChannelMessageDelete(channelID, message.ID)
							if err != nil {
								logger.Warn("Failed to delete message", zap.Error(err))
							}
						}
					case <-time.After(15 * time.Minute):
						if message != nil {
							err := bot.ChannelMessageDelete(channelID, message.ID)
							if err != nil {
								logger.Warn("Failed to delete message", zap.Error(err))
							}
						}
					}
				}()
			}
		}

		// Join any on-going combat match without delay
		skipBackfillDelay = false
		// Start the backfill loop, if the player is not in a party.
		go p.MatchBackfillLoop(session, msession)

		// Put a ticket in for matching
		_, err = p.MatchMake(session, msession)
		if err != nil {
			return err
		}

	default:
		return status.Errorf(codes.InvalidArgument, "invalid mode")
	}

	return nil
}

func (p *EvrPipeline) GetGuildPriorityList(ctx context.Context, userID uuid.UUID) (all []uuid.UUID, selected []uuid.UUID, err error) {

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
	params, ok := ctx.Value(ctxURLParamsKey{}).(map[string][]string)
	if ok {
		// If the params are set, use them
		for _, gid := range params["guilds"] {
			for _, guildId := range strings.Split(gid, ",") {
				s := strings.Trim(guildId, " ")
				if s != "" {
					// Get the groupId for the guild
					groupIDStr, err := GetGroupIDByGuildID(ctx, p.db, s)
					if err != nil {
						continue
					}
					groupID := uuid.FromStringOrNil(groupIDStr)
					if groupID != uuid.Nil && lo.Contains(groupIDs, groupID) {
						guildPriority = append(guildPriority, groupID)
					}
				}
			}
		}
	}

	if len(guildPriority) == 0 {
		// If the params are not set, use the user's guilds
		guildPriority = []uuid.UUID{memberships[0].ID()}
		for _, groupID := range groupIDs {
			if groupID != memberships[0].ID() {
				guildPriority = append(guildPriority, groupID)
			}
		}
	}

	p.logger.Debug("guild priorites", zap.Any("prioritized", guildPriority), zap.Any("all", groupIDs))
	return groupIDs, guildPriority, nil
}
