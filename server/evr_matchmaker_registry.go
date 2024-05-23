// Copyright 2022 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	findAttemptsExpiry          = time.Minute * 3
	LatencyCacheRefreshInterval = time.Hour * 3
	LatencyCacheExpiry          = time.Hour * 72 // 3 hours

	MatchmakingStorageCollection = "MatchmakingRegistry"
	LatencyCacheStorageKey       = "LatencyCache"
)

var (
	ErrMatchmakingPingTimeout        = status.Errorf(codes.DeadlineExceeded, "Ping timeout")
	ErrMatchmakingTimeout            = status.Errorf(codes.DeadlineExceeded, "Matchmaking timeout")
	ErrMatchmakingJoiningMatch       = status.Errorf(codes.OK, "Joining match")
	ErrMatchmakingNoAvailableServers = status.Errorf(codes.Unavailable, "No available servers")
	ErrMatchmakingCanceled           = status.Errorf(codes.Canceled, "Matchmaking canceled")
	ErrMatchmakingCanceledByPlayer   = status.Errorf(codes.Canceled, "Matchmaking canceled by player")
	ErrMatchmakingRestarted          = status.Errorf(codes.Canceled, "matchmaking restarted")
	ErrMatchmakingMigrationRequired  = status.Errorf(codes.FailedPrecondition, "Server upgraded, migration")
	MatchmakingStreamSubject         = uuid.NewV5(uuid.Nil, "matchmaking").String()

	MatchmakingConfigStorageCollection = "Matchmaker"
	MatchmakingConfigStorageKey        = "config"
)

type LatencyMetric struct {
	Endpoint  evr.Endpoint
	RTT       time.Duration
	Timestamp time.Time
}

// String returns a string representation of the endpoint
func (e *LatencyMetric) String() string {
	return fmt.Sprintf("EndpointRTT(InternalIP=%s, ExternalIP=%s, RTT=%s, Timestamp=%s)", e.Endpoint.InternalIP, e.Endpoint.ExternalIP, e.RTT, e.Timestamp)
}

// ID returns a unique identifier for the endpoint
func (e *LatencyMetric) ID() string {
	return fmt.Sprintf("%s:%s", e.Endpoint.InternalIP.String(), e.Endpoint.ExternalIP.String())
}

// The key used for matchmaking properties
func (e *LatencyMetric) AsProperty() (string, float64) {
	k := fmt.Sprintf("rtt%s", ipToKey(e.Endpoint.ExternalIP))
	v := float64(e.RTT / time.Millisecond)
	return k, v
}

// LatencyCache is a cache of broadcaster RTTs for a user
type LatencyCache struct {
	MapOf[string, LatencyMetric]
}

func NewLatencyCache() *LatencyCache {
	return &LatencyCache{
		MapOf[string, LatencyMetric]{},
	}
}

func (c *LatencyCache) SelectPingCandidates(endpoints ...evr.Endpoint) []evr.Endpoint {
	// Initialize candidates with a capacity of 16
	metrics := make([]LatencyMetric, 0, len(endpoints))
	for _, endpoint := range endpoints {
		id := endpoint.ID()
		e, ok := c.Load(id)
		if !ok {
			e = LatencyMetric{
				Endpoint:  endpoint,
				RTT:       0,
				Timestamp: time.Now(),
			}
			c.Store(id, e)
		}
		metrics = append(metrics, e)
	}

	sort.SliceStable(endpoints, func(i, j int) bool {
		// sort by expired first
		if time.Since(metrics[i].Timestamp) > LatencyCacheRefreshInterval {
			// Then by those that have responded
			if metrics[j].RTT > 0 {
				// Then by timestamp
				return metrics[i].Timestamp.Before(metrics[j].Timestamp)
			}
		}
		// Otherwise, sort by RTT
		return metrics[i].RTT < metrics[j].RTT
	})

	if len(endpoints) == 0 {
		return []evr.Endpoint{}
	}
	if len(endpoints) > 16 {
		endpoints = endpoints[:16]
	}
	return endpoints
}

// FoundMatch represents the match found and send over the match join channel
type FoundMatch struct {
	MatchID        MatchID
	Ticket         string // matchmaking ticket if any
	Alignment      TeamAlignment
	ForceAlignment bool
}

type TicketMeta struct {
	TicketID  string
	Query     string
	Timestamp time.Time
}

// MatchmakingSession represents a user session looking for a match.
type MatchmakingSession struct {
	sync.RWMutex
	Ctx            context.Context
	CtxCancelFn    context.CancelCauseFunc
	Logger         *zap.Logger
	Session        *sessionWS
	MatchJoinCh    chan FoundMatch               // Channel for MatchId to join.
	PingResultsCh  chan []evr.EndpointPingResult // Channel for ping completion.
	Expiry         time.Time
	Tickets        map[string]TicketMeta // map[ticketId]TicketMeta
	Party          *PartyHandler
	LatencyCache   *LatencyCache
	GlobalSettings GlobalMatchmakingSettings
	UserSettings   UserMatchmakingSettings
	MatchCriteria  MatchCriteria
	metricsTags    map[string]string
	Presence       PlayerPresence
	pingMu         *sync.Mutex
}

// Cancel cancels the matchmaking session with a given reason, and returns the reason.
func (s *MatchmakingSession) Cancel(reason error) error {
	s.Lock()
	defer s.Unlock()
	select {
	case <-s.Ctx.Done():
		return nil
	default:
	}

	s.CtxCancelFn(reason)
	return nil
}

func (s *MatchmakingSession) AddTicket(ticket string, query string) {
	s.Lock()
	defer s.Unlock()
	ticketMeta := TicketMeta{
		TicketID:  ticket,
		Query:     query,
		Timestamp: time.Now(),
	}
	s.Tickets[ticket] = ticketMeta
}

func (s *MatchmakingSession) RemoveTicket(ticket string) {
	s.Lock()
	defer s.Unlock()
	delete(s.Tickets, ticket)
}

func (s *MatchmakingSession) Wait() error {
	<-s.Ctx.Done()
	return nil
}

func (s *MatchmakingSession) TicketsCount() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.Tickets)
}

func (s *MatchmakingSession) Context() context.Context {
	s.RLock()
	defer s.RUnlock()
	return s.Ctx
}

// MatchmakingResult represents the outcome of a matchmaking request
type MatchmakingResult struct {
	err     error
	Message string
	Code    evr.LobbySessionFailureErrorCode
	Mode    evr.Symbol
	Channel uuid.UUID
	Logger  *zap.Logger
	Session *sessionWS
}

// NewMatchmakingResult initializes a new instance of MatchmakingResult
func NewMatchmakingResult(session *sessionWS, logger *zap.Logger, mode evr.Symbol, channel uuid.UUID) *MatchmakingResult {
	return &MatchmakingResult{
		Session: session,
		Logger:  logger,
		Mode:    mode,
		Channel: channel,
	}
}

// SetErrorFromStatus updates the error and message from a status
func (mr *MatchmakingResult) SetErrorFromStatus(err error) *MatchmakingResult {
	if err == nil {
		return nil
	}
	mr.err = err

	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.OK {
			return nil
		}

		mr.Message = s.Message()
		mr.Code = determineErrorCode(s.Code())
	} else {
		mr.Message = err.Error()
		mr.Code = evr.LobbySessionFailure_InternalError
	}
	return mr
}

// determineErrorCode maps grpc status codes to evr lobby session failure codes
func determineErrorCode(code codes.Code) evr.LobbySessionFailureErrorCode {
	switch code {
	case codes.DeadlineExceeded:
		return evr.LobbySessionFailure_Timeout0
	case codes.InvalidArgument:
		return evr.LobbySessionFailure_BadRequest
	case codes.ResourceExhausted:
		return evr.LobbySessionFailure_ServerIsFull
	case codes.Unavailable:
		return evr.LobbySessionFailure_ServerFindFailed
	case codes.NotFound:
		return evr.LobbySessionFailure_ServerDoesNotExist
	case codes.PermissionDenied, codes.Unauthenticated:
		return evr.LobbySessionFailure_KickedFromLobbyGroup
	case codes.FailedPrecondition:
		return evr.LobbySessionFailure_ServerIsIncompatible

	default:
		return evr.LobbySessionFailure_InternalError
	}
}

// SendErrorToSession sends an error message to a session
func (mr *MatchmakingResult) SendErrorToSession(err error) error {
	result := mr.SetErrorFromStatus(err)
	if result == nil {
		return nil
	}
	// If it was cancelled by the user, don't send and error
	if result.err == ErrMatchmakingCanceledByPlayer || result.err.Error() == "context canceled" {
		return nil
	}

	mr.Logger.Warn("Matchmaking error", zap.String("message", result.Message), zap.Error(result.err))
	return mr.Session.SendEvr(evr.NewLobbySessionFailure(result.Mode, result.Channel, result.Code, result.Message).Version4())
}

// MatchmakingRegistry is a registry for matchmaking sessions
type MatchmakingRegistry struct {
	sync.RWMutex

	ctx         context.Context
	ctxCancelFn context.CancelFunc

	nk            runtime.NakamaModule
	db            *sql.DB
	logger        *zap.Logger
	matchRegistry MatchRegistry
	matchmaker    Matchmaker
	metrics       Metrics
	config        Config
	evrPipeline   *EvrPipeline

	matchingBySession   *MapOf[uuid.UUID, *MatchmakingSession]
	cacheByUserId       *MapOf[uuid.UUID, *LatencyCache]
	broadcasterRegistry *BroadcasterRegistry
}

func NewMatchmakingRegistry(logger *zap.Logger, matchRegistry MatchRegistry, matchmaker Matchmaker, metrics Metrics, db *sql.DB, nk runtime.NakamaModule, config Config, broadcasterRegistry *BroadcasterRegistry) *MatchmakingRegistry {
	ctx, ctxCancelFn := context.WithCancel(context.Background())
	c := &MatchmakingRegistry{
		ctx:         ctx,
		ctxCancelFn: ctxCancelFn,

		nk:            nk,
		db:            db,
		logger:        logger,
		matchRegistry: matchRegistry,
		matchmaker:    matchmaker,
		metrics:       metrics,
		config:        config,

		matchingBySession:   &MapOf[uuid.UUID, *MatchmakingSession]{},
		cacheByUserId:       &MapOf[uuid.UUID, *LatencyCache]{},
		broadcasterRegistry: broadcasterRegistry,
	}
	// Set the matchmaker's OnMatchedEntries callback
	matchmaker.OnMatchedEntries(c.matchedEntriesFn)

	return c
}

type GlobalMatchmakingSettings struct {
	MinCount              int    `json:"min_count"`               // Minimum number of matches to create
	MaxCount              int    `json:"max_count"`               // Maximum number of matches to create
	CountMultiple         int    `json:"count_multiple"`          // Count multiple of the party size
	BackfillQueryAddon    string `json:"backfill_query_addon"`    // Additional query to add to the matchmaking query
	MatchmakingQueryAddon string `json:"matchmaking_query_addon"` // Additional query to add to the matchmaking query
	CreateQueryAddon      string `json:"create_query_addon"`      // Additional query to add to the match
	BackfillSameMatch     bool   `json:"backfill_same_match"`     // Backfill the same match
}

type UserMatchmakingSettings struct {
	BackfillQueryAddon    string  `json:"backfill_query_addon,omitempty"`    // Additional query to add to the matchmaking query
	MatchmakingQueryAddon string  `json:"matchmaking_query_addon,omitempty"` // Additional query to add to the matchmaking query
	CreateQueryAddon      string  `json:"create_query_addon,omitempty"`      // Additional query to add to the matchmaking query
	GroupID               string  `json:"group_id"`                          // Group ID to matchmake with
	NextMatchID           MatchID `json:"next_match_id"`                     // Try to join this match immediately when finding a match
}

func StorageLoad[T any](ctx context.Context, nk runtime.NakamaModule, userID string, collection string, key string, defaultValue T, create bool) (value T, err error) {
	objs, err := nk.StorageRead(ctx, []*runtime.StorageRead{
		{
			Collection: collection,
			Key:        key,
			UserID:     userID,
		},
	})
	if err != nil {
		return
	}

	if len(objs) == 0 && create {
		err = StorageSave(ctx, nk, defaultValue, userID, collection, key)
		if err != nil {
			return
		}
		return
	}
	value = defaultValue

	if err = json.Unmarshal([]byte(objs[0].Value), &value); err != nil {

		return
	}
	return
}

func StorageSave[T any](ctx context.Context, nk runtime.NakamaModule, value T, userID string, collection string, key string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{
		{
			UserID:          userID,
			Collection:      collection,
			Key:             key,
			Value:           string(data),
			PermissionRead:  0,
			PermissionWrite: 0,
		},
	})
	return err
}

func LoadGlobalMatchmakingSettings(ctx context.Context, nk runtime.NakamaModule) (config GlobalMatchmakingSettings, err error) {
	defaultConfig := GlobalMatchmakingSettings{
		MinCount:      2,
		MaxCount:      8,
		CountMultiple: 2,
	}

	config, err = StorageLoad(ctx, nk, SystemUserID, MatchmakingConfigStorageCollection, MatchmakingConfigStorageKey, defaultConfig, true)
	return
}

func LoadUserMatchmakingSettings(ctx context.Context, nk runtime.NakamaModule, userID string) (config UserMatchmakingSettings, err error) {
	defaultConfig := UserMatchmakingSettings{}

	config, err = StorageLoad(ctx, nk, userID, MatchmakingConfigStorageCollection, MatchmakingConfigStorageKey, defaultConfig, true)
	return
}

func StoreGlobalMatchmakingSettings(ctx context.Context, nk runtime.NakamaModule, config GlobalMatchmakingSettings) error {
	return StorageSave(ctx, nk, config, SystemUserID, MatchmakingConfigStorageCollection, MatchmakingConfigStorageKey)
}

func StoreUserMatchmakingSettings(ctx context.Context, nk runtime.NakamaModule, userID string, config UserMatchmakingSettings) error {
	return StorageSave(ctx, nk, config, userID, MatchmakingConfigStorageCollection, MatchmakingConfigStorageKey)
}

func ipToKey(ip net.IP) string {
	b := ip.To4()
	return fmt.Sprintf("rtt%02x%02x%02x%02x", b[0], b[1], b[2], b[3])
}
func keyToIP(key string) net.IP {
	b, _ := hex.DecodeString(key[3:])
	return net.IPv4(b[0], b[1], b[2], b[3])
}

func (mr *MatchmakingRegistry) matchedEntriesFn(entries [][]*MatchmakerEntry) {
	// Get the matchmaking config from the storage
	config, err := LoadGlobalMatchmakingSettings(mr.ctx, mr.nk)
	if err != nil {
		mr.logger.Error("Failed to load matchmaking config", zap.Error(err))
		return
	}

	queryAddon := config.CreateQueryAddon
	for _, entrants := range entries {

		go mr.buildMatch(entrants, queryAddon)
	}
}

func (mr *MatchmakingRegistry) buildRosters(logger *zap.Logger, entrants []*MatchmakerEntry) [][]*MatchmakingSession {
	// Get the matching sessions for each entrant

	partyMap := make(map[string][]*MatchmakingSession, len(entrants))
	for _, entrant := range entrants {
		ms, ok := mr.GetMatchingBySessionId(entrant.Presence.SessionID)
		if !ok {
			logger.Error("Could not find matching session for user", zap.String("sessionID", entrant.Presence.SessionID.String()))
			continue
		}
		partyMap[entrant.PartyId] = append(partyMap[entrant.PartyId], ms)
	}

	partySlice := make([][]*MatchmakingSession, 0, len(partyMap))
	for _, p := range partyMap {
		partySlice = append(partySlice, p)
	}

	// Generate the rosters, and distribute the players to the teams based on the parties
	rosters := distributeParties(partySlice)
	if len(rosters) != 2 {
		logger.Error("Invalid number of teams", zap.Int("teams", len(rosters)))
		return nil
	}
	return rosters
}

func (mr *MatchmakingRegistry) pickChannel(entrants []*MatchmakerEntry) []uuid.UUID {
	// Get the channels from the entrants
	channelMap := make(map[uuid.UUID]int, len(entrants))
	for _, e := range entrants {
		channel := uuid.FromStringOrNil(e.StringProperties["channel"])
		channelMap[channel] += 1
	}

	channels := lo.Keys(channelMap)

	// Sort the channels by the most common
	sort.SliceStable(channels, func(i, j int) bool {
		return channelMap[channels[i]] > channelMap[channels[j]]
	})

	// Sort the channel map by
	return channels

}

func (mr *MatchmakingRegistry) scoreBroadcasters(versionLock evr.Symbol, entrants []*MatchmakerEntry) []string {

	// Get a map of all broadcasters by their key
	broadcastersByExtIP := make(map[string]uuid.UUID, 100)
	broadcasters := mr.broadcasterRegistry.GetBroadcasters(versionLock, false)

	for s, b := range broadcasters {
		broadcastersByExtIP[ipToKey(b.Presence.Endpoint.ExternalIP)] = s
	}

	// Create a map of each endpoint and it's latencies to each entrant
	latencies := make(map[string][]int, len(broadcastersByExtIP))
	for _, e := range entrants {
		// loop over the number props and get the latencies
		for k, v := range e.NumericProperties {
			if strings.HasPrefix(k, "rtt") {
				latencies[k] = append(latencies[k], int(v))
			}
		}
	}

	// Score each endpoint based on the latencies
	scores := make(map[string]int, len(latencies))
	for k, v := range latencies {
		// Sort the latencies
		sort.Ints(v)
		// Get the average
		average := 0
		for _, i := range v {
			average += i
		}
		average /= len(v)
		scores[k] = average
	}
	// Sort the scored endpoints
	sorted := make([]string, 0, len(scores))
	for k := range scores {
		sorted = append(sorted, k)
	}

	// Sort the scored endpoints by the score
	sort.SliceStable(sorted, func(i, j int) bool {
		return scores[sorted[i]] < scores[sorted[j]]
	})

	return sorted
}

func (mr *MatchmakingRegistry) createLoop(channels []uuid.UUID, broadcasters []string, matchCriteria MatchCriteria) (mt MatchID, err error) {
	// Loop until a server becomes available or matchmaking times out.
	timeout := time.After(3 * time.Minute)
	interval := time.NewTicker(10 * time.Second)
	var matchID MatchID
	for {

		matchID, err := mr.allocateBroadcaster(channels, broadcasters, matchCriteria)
		if err != nil {
			if err != ErrMatchmakingNoAvailableServers {
				mr.logger.Error("Error allocating broadcaster", zap.Error(err))
			}
		}
		if !matchID.IsNil() {
			break
		}

		select {
		case <-mr.ctx.Done(): // Context cancelled
			return mt, ErrMatchmakingCanceled

		case <-timeout: // Timeout
			return mt, ErrMatchmakingTimeout
		case <-interval.C:
		}
	}

	if matchID.IsNil() {
		mr.logger.Error("No match ID found")
		return mt, ErrMatchmakingNoAvailableServers
	}
	return matchID, nil
}

func (mr *MatchmakingRegistry) buildMatch(entrants []*MatchmakerEntry, queryAddon string) {
	logger := mr.logger
	if len(entrants) == 0 {
		logger.Warn("No entrants in the match")
		return
	}

	// Get the matchSettings from the first participant
	var matchCriteria MatchCriteria

	for _, e := range entrants {
		if s, ok := mr.GetMatchingBySessionId(e.Presence.SessionID); ok {
			matchCriteria = s.MatchCriteria
			break
		}
	}

	metricsTags := map[string]string{
		"version_lock": entrants[0].StringProperties["version_lock"],
		"mode":         matchCriteria.Mode.String(),
		"level":        matchCriteria.Level.String(),
	}
	mr.metrics.CustomCounter("matchmaking_matched_participant_count", metricsTags, int64(len(entrants)))

	versionLock := entrants[0].StringProperties["version_lock"]

	rosters := mr.buildRosters(logger, entrants)

	channels := mr.pickChannel(entrants)

	broadcasterScores := mr.scoreBroadcasters(evr.ToSymbol(versionLock), entrants)

	matchID, err := mr.createLoop(channels, broadcasterScores, matchCriteria)
	if err != nil {
		logger.Error("Failed to create match", zap.Error(err))
		return
	}

	participants := make(map[uuid.UUID]*MatchmakingSession, len(entrants))
	var ok bool
	for _, e := range entrants {
		participants[e.Presence.SessionID], ok = mr.GetMatchingBySessionId(e.Presence.SessionID)
		if !ok {
			logger.Error("Could not find matching session for user", zap.String("sessionID", e.Presence.SessionID.String()))
			continue
		}
	}
	entrantMap := make(map[uuid.UUID]*MatchmakerEntry, len(entrants))

	// Assign the teams to the match
	for i, msessions := range rosters {
		// Assign each player in the team to the match
		for _, msession := range msessions {
			// Get the ticket metadata

			entrant := entrantMap[msession.Session.id]

			foundMatch := FoundMatch{
				MatchID:        matchID,
				Ticket:         entrant.GetTicket(),
				Alignment:      TeamAlignment(i),
				ForceAlignment: true,
			}
			// Send the join instruction
			select {
			case <-msession.Ctx.Done():
				logger.Warn("Matchmaking session cancelled", zap.String("sessionID", entrant.Presence.SessionID.String()))
				continue
			case msession.MatchJoinCh <- foundMatch:
				logger.Info("Sent match join instruction", zap.String("sessionID", entrant.Presence.SessionID.String()))
			case <-time.After(2 * time.Second):
				logger.Warn("Failed to send match join instruction", zap.String("sessionID", entrant.Presence.SessionID.String()))
			}
		}
	}
}

func distributeParties(parties [][]*MatchmakingSession) [][]*MatchmakingSession {
	// Distribute the players from each party on the two teams.
	// Sort the parties by size, smallest to largest
	parties = make([][]*MatchmakingSession, 0, len(parties))
	for _, p := range parties {
		parties = append(parties, p)
	}

	slices.SortStableFunc(parties, func(a, b []*MatchmakingSession) int {
		return len(a) - len(b)
	})

	rosters := make([][]*MatchmakingSession, 2)

	// Starting with parties of two, try to add them to the teams, if the party doesn't fit, put it on the other team
	for i, party := range parties {
		// If the party fits on the first team, add it
		if len(rosters[0])+len(party) <= 4 {
			for _, player := range party {
				rosters[0] = append(rosters[0], player)
			}
			parties = append(parties[:i], parties[i+1:]...)
		} else {
			// If the party fits on the second team, add it
			for _, player := range party {
				rosters[1] = append(rosters[1], player)
			}
			parties = append(parties[:i], parties[i+1:]...)
		}
	}

	// Put all the remaining players on the first team.
	for _, party := range parties {
		for _, p := range party {
			rosters[0] = append(rosters[0], p)
		}
	}

	// Balance the teams
	if len(rosters[0]) > 4 {
		rosters[1] = append(rosters[1], rosters[0][4:]...)
	}

	return rosters
}

func (mr *MatchmakingRegistry) allocateBroadcaster(channels []uuid.UUID, sorted []string, matchCriteria MatchCriteria) (mt MatchID, err error) {
	// Lock the broadcasters so that they aren't double allocated
	mr.Lock()
	defer mr.Unlock()

	mr.broadcasterRegistry.Lock()
	defer mr.broadcasterRegistry.Unlock()
	broadcasters := mr.broadcasterRegistry.GetBroadcasters(matchCriteria.VersionLock, false)

	var broadcaster *Broadcaster
	for _, k := range sorted {
		for _, b := range broadcasters {
			if ipToKey(b.Presence.Endpoint.ExternalIP) == k {
				// Allocate the broadcaster to the match
				broadcaster = &b
			}
		}
	}
	// Find the first matching channel for this server
	var channel uuid.UUID
	for _, c := range channels {
		if slices.Contains(broadcaster.Presence.Channels, c) {
			channel = c
			break
		}
	}

	if broadcaster == nil {
		return mt, ErrMatchmakingNoAvailableServers
	}

	matchSettings := NewMatchSettingsFromMode(matchCriteria.Mode, matchCriteria.VersionLock)

	params := MatchParameters{
		Settings: matchSettings,
		Metadata: MatchMetadata{
			Channel: channel,
		},
		Broadcaster: broadcaster.Presence,
		TickRate:    15,
	}

	matchID, err := mr.nk.MatchCreate(mr.ctx, MatchModule, params.AsMap())
	if err != nil {
		return mt, fmt.Errorf("error creating match: %v", err)
	}

	return MatchIDFromStringOrNil(matchID), nil
}

// GetPingCandidates returns a list of endpoints to ping for a user. It also updates the broadcasters.
func (m *MatchmakingSession) GetPingCandidates(endpoints ...evr.Endpoint) (candidates []evr.Endpoint) {

	const LatencyCacheExpiry = 6 * time.Hour

	// Initialize candidates with a capacity of 16
	candidates = make([]evr.Endpoint, 0, 16)

	// Return early if there are no endpoints
	if len(endpoints) == 0 {
		return candidates
	}

	// Retrieve the user's cache and lock it for use
	cache := m.LatencyCache

	// Get the endpoint latencies from the cache
	entries := make([]LatencyMetric, 0, len(endpoints))

	// Iterate over the endpoints, and load/create their cache entry
	for _, endpoint := range endpoints {
		id := endpoint.ID()

		e, ok := cache.Load(id)
		if !ok {
			e = LatencyMetric{
				Endpoint:  endpoint,
				RTT:       0,
				Timestamp: time.Now(),
			}
			cache.Store(id, e)
		}
		entries = append(entries, e)
	}

	// If there are no cache entries, return the empty endpoints
	if len(entries) == 0 {
		return candidates
	}

	// Sort the cache entries by timestamp in descending order.
	// This will prioritize the oldest endpoints first.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].RTT == 0 && entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	// Fill the rest of the candidates with the oldest entries
	for _, e := range entries {
		endpoint := e.Endpoint
		candidates = append(candidates, endpoint)
		if len(candidates) >= 16 {
			break
		}
	}

	/*
		// Clean up the cache by removing expired entries
		for id, e := range cache.Store {
			if time.Since(e.Timestamp) > LatencyCacheExpiry {
				delete(cache.Store, id)
			}
		}
	*/

	return candidates
}

// GetCache returns the latency cache for a user
func (r *MatchmakingRegistry) GetCache(userId uuid.UUID) *LatencyCache {
	cache, _ := r.cacheByUserId.LoadOrStore(userId, &LatencyCache{})
	return cache
}

// listMatches returns a list of matches
func (c *MatchmakingRegistry) listMatches(ctx context.Context, limit int, minSize, maxSize int, query string) ([]*api.Match, error) {
	authoritativeWrapper := &wrapperspb.BoolValue{Value: true}
	var labelWrapper *wrapperspb.StringValue
	var queryWrapper *wrapperspb.StringValue
	if query != "" {
		queryWrapper = &wrapperspb.StringValue{Value: query}
	}
	minSizeWrapper := &wrapperspb.Int32Value{Value: int32(minSize)}

	maxSizeWrapper := &wrapperspb.Int32Value{Value: int32(maxSize)}

	matches, _, err := c.matchRegistry.ListMatches(ctx, limit, authoritativeWrapper, labelWrapper, minSizeWrapper, maxSizeWrapper, queryWrapper, nil)
	return matches, err
}

type LatencyCacheStorageObject struct {
	Entries map[string]LatencyMetric `json:"entries"`
}

// LoadLatencyCache loads the latency cache for a user
func (c *MatchmakingRegistry) LoadLatencyCache(ctx context.Context, logger *zap.Logger, session *sessionWS, msession *MatchmakingSession) (*LatencyCache, error) {
	// Load the latency cache
	// retrieve the document from storage
	userId := session.UserID()
	// Get teh user's latency cache
	cache := c.GetCache(userId)
	result, err := StorageReadObjects(ctx, logger, session.pipeline.db, uuid.Nil, []*api.ReadStorageObjectId{
		{
			Collection: MatchmakingStorageCollection,
			Key:        LatencyCacheStorageKey,
			UserId:     userId.String(),
		},
	})
	if err != nil {
		logger.Error("failed to read objects", zap.Error(err))
		return nil, status.Errorf(codes.Internal, "Failed to read latency cache: %v", err)
	}

	objs := result.Objects
	if len(objs) != 0 {
		store := &LatencyCacheStorageObject{}
		if err := json.Unmarshal([]byte(objs[0].Value), store); err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to unmarshal latency cache: %v", err)
		}
		// Load the entries into the cache
		for k, v := range store.Entries {
			cache.Store(k, v)

		}
	}

	return cache, nil
}

// StoreLatencyCache stores the latency cache for a user
func (c *MatchmakingRegistry) StoreLatencyCache(session *sessionWS) {
	cache := c.GetCache(session.UserID())

	if cache == nil {
		return
	}

	store := &LatencyCacheStorageObject{
		Entries: make(map[string]LatencyMetric, 100),
	}

	cache.Range(func(k string, v LatencyMetric) bool {
		store.Entries[k] = v
		return true
	})

	// Save the latency cache
	jsonBytes, err := json.Marshal(store)
	if err != nil {
		session.logger.Error("Failed to marshal latency cache", zap.Error(err))
		return
	}
	data := string(jsonBytes)
	ops := StorageOpWrites{
		{
			OwnerID: session.UserID().String(),
			Object: &api.WriteStorageObject{
				Collection:      MatchmakingStorageCollection,
				Key:             LatencyCacheStorageKey,
				Value:           data,
				PermissionRead:  &wrapperspb.Int32Value{Value: int32(0)},
				PermissionWrite: &wrapperspb.Int32Value{Value: int32(0)},
			},
		},
	}
	if _, _, err = StorageWriteObjects(context.Background(), session.logger, session.pipeline.db, session.metrics, session.storageIndex, true, ops); err != nil {
		session.logger.Error("Failed to save latency cache", zap.Error(err))
	}
}

// Stop stops the matchmaking registry
func (c *MatchmakingRegistry) Stop() {
	c.ctxCancelFn()
}

// GetMatchingBySessionId returns the matching session for a given session ID
func (c *MatchmakingRegistry) GetMatchingBySessionId(sessionId uuid.UUID) (session *MatchmakingSession, ok bool) {
	session, ok = c.matchingBySession.Load(sessionId)
	return session, ok
}

// Delete removes a matching session from the registry
func (c *MatchmakingRegistry) Delete(sessionId uuid.UUID) {
	c.matchingBySession.Delete(sessionId)
}

// Add adds a matching session to the registry
func (c *MatchmakingRegistry) Create(ctx context.Context, logger *zap.Logger, session *sessionWS, presence PlayerPresence, settings MatchSettings, timeout time.Duration, errorFn func(err error) error, joinFn func(matchToken MatchID) error, joinParty bool) (*MatchmakingSession, error) {
	// Check if there is an existing session
	if _, ok := c.GetMatchingBySessionId(session.ID()); ok {
		// Cancel it
		c.Cancel(session.ID(), ErrMatchmakingCanceledByPlayer)
	}
	userSettings, err := LoadUserMatchmakingSettings(ctx, c.nk, session.UserID().String())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load matchmaking settings: %v", err)
	}

	globalSettings, err := LoadGlobalMatchmakingSettings(ctx, c.nk)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load matchmaking settings: %v", err)
	}

	metricsTags := map[string]string{
		"version_lock": presence.VersionLock.String(),
		"type":         settings.LobbyType.String(),
		"mode":         settings.Mode.String(),
		"level":        settings.Level.String(),
		"alignment":    presence.Alignment.String(),
	}

	// Set defaults for the matching label
	logger = logger.With(zap.String("msid", session.ID().String()))
	ctx, cancel := context.WithCancelCause(ctx)
	msession := &MatchmakingSession{
		Ctx:         ctx,
		CtxCancelFn: cancel,

		Logger:         logger,
		Session:        session,
		MatchJoinCh:    make(chan FoundMatch, 5),
		PingResultsCh:  make(chan []evr.EndpointPingResult),
		Expiry:         time.Now().UTC().Add(findAttemptsExpiry),
		Tickets:        make(map[string]TicketMeta),
		UserSettings:   userSettings,
		GlobalSettings: globalSettings,
		metricsTags:    metricsTags,
		pingMu:         &sync.Mutex{},
	}

	// Set the player presence
	// Load the latency cache
	cache, err := c.LoadLatencyCache(ctx, logger, session, msession)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to load latency cache: %v", err)
	}
	msession.LatencyCache = cache

	// Join the party if requested
	if joinParty && userSettings.GroupID != "" {
		if err := msession.joinParty(); err != nil {
			return nil, err
		}
	}

	// listen for a match ID to join
	go func() {
		// Create a timer for this session
		startedAt := time.Now().UTC()
		metricsTags := map[string]string{
			"mode":    msession.MatchCriteria.Mode.String(),
			"channel": msession.Presence.Memberships[0].ChannelID.String(),
			"level":   msession.MatchCriteria.Level.String(),
			"team":    strconv.FormatInt(int64(msession.Presence.Alignment), 10),
			"result":  "success",
		}
		defer func() {
			c.metrics.CustomTimer("matchmaking_session_duration_ms", metricsTags, time.Since(startedAt)*time.Millisecond)
		}()

		defer cancel(nil)
		var err error
		select {
		case <-ctx.Done():
			if ctx.Err() != nil {
				err = context.Cause(ctx)
			}
		case <-time.After(timeout):
			// Timeout
			err = ErrMatchmakingTimeout
			c.metrics.CustomCounter("matchmaking_session_timeout_count", metricsTags, 1)
		case matchFound := <-msession.MatchJoinCh:
			// join the match
			err = joinFn(matchFound.MatchID)

			c.metrics.CustomCounter("matchmaking_session_success_count", metricsTags, 1)

		}
		switch err {
		case ErrMatchmakingJoiningMatch:
			metricsTags["result"] = "joining"
		case ErrMatchmakingCanceledByPlayer:
			metricsTags["result"] = "canceled"
		case nil:
		default:
			metricsTags["result"] = "error"
			defer errorFn(err)
		}

		if len(msession.Tickets) > 0 {
			// Remove the tickets from the matchmaker
			tickets := make([]string, 0, len(msession.Tickets))
			for _, v := range msession.Tickets {
				tickets = append(tickets, v.TicketID)
			}
			c.matchmaker.Remove(tickets)
		}
		c.StoreLatencyCache(session)
		c.Delete(session.id)
		if err := session.matchmaker.RemoveSessionAll(session.id.String()); err != nil {
			logger.Error("Failed to remove session from matchmaker", zap.Error(err))
		}
	}()
	c.Add(session.id, msession)
	return msession, nil
}

func (c *MatchmakingRegistry) Cancel(sessionId uuid.UUID, reason error) {
	if session, ok := c.GetMatchingBySessionId(sessionId); ok {
		c.logger.Debug("Canceling matchmaking session", zap.String("reason", reason.Error()))
		session.Cancel(reason)
	}
}

func (c *MatchmakingRegistry) Add(id uuid.UUID, s *MatchmakingSession) {
	c.matchingBySession.Store(id, s)

}

// PingEndpoints pings the endpoints and returns the latencies.
func (ms *MatchmakingSession) PingEndpoints(endpoints map[string]evr.Endpoint) ([]LatencyMetric, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	logger := ms.Logger

	// get teh "ping" lock so that two ping requests are not sent out within the same time frame
	ms.pingMu.Lock()
	defer ms.pingMu.Unlock()

	// Get the candidates for pinging
	candidates := ms.GetPingCandidates(lo.Values(endpoints)...)
	if len(candidates) > 0 {
		if err := ms.sendPingRequest(logger, candidates); err != nil {
			return nil, err
		}

		select {
		case <-ms.Ctx.Done():
			return nil, ErrMatchmakingCanceled
		case <-time.After(5 * time.Second):
			return nil, ErrMatchmakingPingTimeout
		case results := <-ms.PingResultsCh:
			cache := ms.LatencyCache

			// Add the latencies to the cache
			for _, response := range results {

				r := LatencyMetric{
					Endpoint:  endpoints[response.EndpointID()],
					RTT:       response.RTT(),
					Timestamp: time.Now(),
				}

				cache.Store(r.ID(), r)
			}
		}
	}

	return ms.getEndpointLatencies(endpoints), nil
}

// sendPingRequest sends a ping request to the given candidates.
func (ms *MatchmakingSession) sendPingRequest(logger *zap.Logger, candidates []evr.Endpoint) error {

	if err := ms.Session.SendEvr(
		evr.NewLobbyPingRequest(275, candidates),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return err
	}
	return nil
}

// getEndpointLatencies returns the latencies for the given endpoints.
func (ms *MatchmakingSession) getEndpointLatencies(endpoints map[string]evr.Endpoint) []LatencyMetric {

	// Create endpoint id slice
	endpointIDs := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		endpointIDs = append(endpointIDs, e.ID())
	}

	var endpointRTTs map[string]LatencyMetric
	// If no endpoints are provided, get all the latencies from the cache
	// build a new map to avoid locking the cache for too long
	if len(endpointIDs) == 0 {
		endpointRTTs = make(map[string]LatencyMetric, 100)
		ms.LatencyCache.Range(func(k string, v LatencyMetric) bool {
			endpointRTTs[k] = v
			return true
		})
	} else {
		// Return just the endpoints requested
		endpointRTTs = make(map[string]LatencyMetric, len(endpointIDs))
		for _, id := range endpointIDs {
			endpoint := evr.FromEndpointID(id)
			// Create the endpoint and add it to the cache
			e := LatencyMetric{
				Endpoint:  endpoint,
				RTT:       0,
				Timestamp: time.Now(),
			}
			r, _ := ms.LatencyCache.LoadOrStore(id, e)
			endpointRTTs[id] = r
		}
	}

	results := make([]LatencyMetric, 0, len(endpoints))
	for _, e := range endpoints {
		if l, ok := endpointRTTs[e.ID()]; ok {
			results = append(results, l)
		}
	}

	return results
}

func (ms *MatchmakingSession) joinParty() error {

	groupID := ms.UserSettings.GroupID
	partyID := uuid.NewV5(uuid.Nil, groupID)
	logger := ms.Logger.With(zap.String("party_id", partyID.String()))

	if ms.Party != nil && ms.Party.IDStr == partyID.String() {
		return nil
	}

	session := ms.Session
	partyRegistry := session.pipeline.partyRegistry.(*LocalPartyRegistry)

	// Check if the party exists
	ph, found := partyRegistry.parties.Load(partyID)
	if !found {
		presence := &rtapi.UserPresence{
			UserId:    session.UserID().String(),
			SessionId: session.ID().String(),
			Username:  session.Username(),
		}
		open := true
		maxSize := 8

		ph = NewPartyHandler(logger, partyRegistry, session.matchmaker, session.tracker, session.evrPipeline.streamManager, session.pipeline.router, partyID, session.pipeline.node, open, maxSize, presence)
		partyRegistry.parties.Store(partyID, ph)

		success, _ := session.tracker.Track(session.Context(), session.ID(), ph.Stream, session.UserID(), PresenceMeta{
			Format:   session.Format(),
			Username: session.Username(),
			Status:   "",
		})
		if !success {
			return status.Errorf(codes.Internal, "Failed to track user in party")
		}
		return nil
	}

	autoJoin, err := ph.JoinRequest(&Presence{
		ID: PresenceID{
			Node:      session.pipeline.node,
			SessionID: session.ID(),
		},
		// Presence stream not needed.
		UserID: session.UserID(),
		Meta: PresenceMeta{
			Username: session.Username(),
			// Other meta fields not needed.
		},
	})
	if err != nil {
		return err
	}

	if autoJoin {
		success, _ := session.tracker.Track(session.Context(), session.ID(), PresenceStream{Mode: StreamModeParty, Subject: partyID, Label: session.pipeline.node}, session.UserID(), PresenceMeta{
			Format:   session.Format(),
			Username: session.Username(),
			Status:   "",
		})
		if !success {
			return status.Errorf(codes.Internal, "Failed to track user in party as member")
		}
	}

	// Wait for the tracker to update
	<-time.After(1 * time.Second)
	if err != nil {
		if err != runtime.ErrPartyJoinRequestAlreadyMember {
			return status.Errorf(codes.Internal, "Failed to join party group: %v", err)
		}
	}
	ms.Party = ph
	return nil
}

func (ms *MatchmakingSession) BuildQuery(latencies []LatencyMetric) (query string, stringProps map[string]string, numericProps map[string]float64, err error) {
	// Create the properties maps
	stringProps = make(map[string]string)
	numericProps = make(map[string]float64, len(latencies))
	qparts := make([]string, 0, 10)

	// SHOULD match this channel
	chstr := strings.Replace(ms.Presence.Memberships[0].ChannelID.String(), "-", "", -1)
	qparts = append(qparts, fmt.Sprintf("properties.channel:%s^3", chstr))
	stringProps["channel"] = chstr

	// Add this user's ID to the string props
	stringProps["userid"] = strings.Replace(ms.Session.UserID().String(), "-", "", -1)

	// Add a property of the external IP's RTT
	for _, b := range latencies {
		if b.RTT == 0 {
			continue
		}

		latency := mroundRTT(b.RTT, 10)
		// Turn the IP into a hex string like 127.0.0.1 -> 7f000001
		ip := ipToKey(b.Endpoint.ExternalIP)
		// Add the property
		numericProps[ip] = float64(latency.Milliseconds())

		// TODO FIXME Add second matchmaking ticket for over 150ms
		n := 0
		switch {
		case latency < 25:
			n = 10
		case latency < 40:
			n = 7
		case latency < 60:
			n = 5
		case latency < 80:
			n = 2

			// Add a score for each endpoint
			p := fmt.Sprintf("properties.%s:<=%d^%d", ip, latency+15, n)
			qparts = append(qparts, p)
		}
	}
	// MUST be the same mode
	qparts = append(qparts, GameMode(ms.MatchCriteria.Mode).Label(Must, 0).Property())
	stringProps["mode"] = ms.MatchCriteria.Mode.Token().String()

	for _, membership := range ms.Presence.Memberships {
		// Add the properties
		// Strip out the hyphens from the group ID
		s := strings.ReplaceAll(membership.ChannelID.String(), "-", "")
		stringProps[s] = "T"

		// If this is the user's current channel, then give it a +3 boost
		if membership == ms.Presence.Memberships[0] {
			qparts = append(qparts, fmt.Sprintf("properties.%s:T^3", s))
		} else {
			qparts = append(qparts, fmt.Sprintf("properties.%s:T", s))
		}
	}

	query = strings.Join(qparts, " ")
	// TODO Promote friends
	ms.Logger.Debug("Matchmaking query", zap.String("query", query), zap.Any("stringProps", stringProps), zap.Any("numericProps", numericProps))
	// TODO Avoid ghosted
	return query, stringProps, numericProps, nil
}

func (c *MatchmakingRegistry) SessionsByMode() map[evr.Symbol]map[uuid.UUID]*MatchmakingSession {

	sessionByMode := make(map[evr.Symbol]map[uuid.UUID]*MatchmakingSession)

	c.matchingBySession.Range(func(sid uuid.UUID, ms *MatchmakingSession) bool {
		if sessionByMode[ms.Label.Mode] == nil {
			sessionByMode[ms.Label.Mode] = make(map[uuid.UUID]*MatchmakingSession)
		}

		sessionByMode[ms.Label.Mode][sid] = ms
		return true
	})
	return sessionByMode
}
