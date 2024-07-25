package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/ipinfo/go/v2/ipinfo"
)

const (
	VersionLock       uint64 = 0xc62f01d78f77910d // The game build version.
	MatchmakingModule        = "evr"              // The module used for matchmaking

	MatchMaxSize                             = 12 // The total max players (not including the broadcaster) for a EVR lobby.
	LevelSelectionFirst  MatchLevelSelection = "first"
	LevelSelectionRandom MatchLevelSelection = "random"

	StatGroupArena  MatchStatGroup = "arena"
	StatGroupCombat MatchStatGroup = "combat"
)

const (
	OpCodeBroadcasterDisconnected int64 = iota
	OpCodeEVRPacketData

	SignalPrepareSession
	SignalStartSession
	SignalEndSession
	SignalLockSession
	SignalUnlockSession
	SignalGetEndpoint
	SignalGetPresences
	SignalPruneUnderutilized
	SignalTerminate
)

var (
	displayNameRegex = regexp.MustCompile(`"displayname": "(\\"|[^"])*"`)
	updatetimeRegex  = regexp.MustCompile(`"updatetime": \d*`)
)

type MatchStatGroup string
type MatchLevelSelection string

type EvrSignal struct {
	UserId  string
	Signal  int64
	Payload []byte
}

func NewEvrSignal(userId string, signal int64, data any) *EvrSignal {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return &EvrSignal{
		UserId:  userId,
		Signal:  signal,
		Payload: payload,
	}
}

func (s EvrSignal) GetOpCode() int64 {
	return s.Signal
}

func (s EvrSignal) GetData() []byte {
	return s.Payload
}

func (s EvrSignal) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

const (
	EvrMatchmakerModule = "evrmatchmaker"
	EvrBackfillModule   = "evrbackfill"
)

var _ runtime.Presence = &EntrantPresence{}

// Represents identity information for a single match participant.
type EntrantPresence struct {
	Node           string    `json:"node,omitempty"`
	SessionID      uuid.UUID `json:"session_id,omitempty"`
	LoginSessionID uuid.UUID `json:"login_session_id,omitempty"`
	UserID         uuid.UUID `json:"user_id,omitempty"`
	EvrID          evr.EvrID `json:"evr_id,omitempty"`
	DiscordID      string    `json:"discord_id,omitempty"`
	ClientIP       string    `json:"client_ip,omitempty"`
	ClientPort     string    `json:"client_port,omitempty"`
	Username       string    `json:"username,omitempty"`
	DisplayName    string    `json:"display_name,omitempty"`
	PartyID        uuid.UUID `json:"party_id,omitempty"`
	RoleAlignment  int       `json:"role,omitempty"`
	SessionExpiry  int64     `json:"session_expiry,omitempty"`
}

func (p EntrantPresence) String() string {
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(data)
}

func (p EntrantPresence) GetUserId() string {
	return p.UserID.String()
}
func (p EntrantPresence) GetSessionId() string {
	return p.SessionID.String()
}
func (p EntrantPresence) GetNodeId() string {
	return p.Node
}
func (p EntrantPresence) GetHidden() bool {
	return false
}
func (p EntrantPresence) GetPersistence() bool {
	return false
}
func (p EntrantPresence) GetUsername() string {
	return p.Username
}
func (p EntrantPresence) GetStatus() string {
	data, _ := json.Marshal(p)
	return string(data)
}
func (p *EntrantPresence) GetReason() runtime.PresenceReason {
	return runtime.PresenceReasonUnknown
}
func (p EntrantPresence) GetEvrID() string {
	return p.EvrID.String()
}

func (p EntrantPresence) IsPlayer() bool {
	return p.RoleAlignment != evr.TeamModerator && p.RoleAlignment != evr.TeamSpectator
}

func (p EntrantPresence) IsModerator() bool {
	return p.RoleAlignment == evr.TeamModerator
}

func (p EntrantPresence) IsSpectator() bool {
	return p.RoleAlignment == evr.TeamSpectator
}

func (p EntrantPresence) IsSocial() bool {
	return p.RoleAlignment == evr.TeamSocial
}

type JoinMetadata struct {
	Presence       EntrantPresence
	PartyPresences []rtapi.UserPresence
}

func NewJoinMetadata(p EntrantPresence) *JoinMetadata {
	return &JoinMetadata{Presence: p}
}

func (m JoinMetadata) MarshalMap() map[string]string {
	data, err := json.Marshal(m.Presence)
	if err != nil {
		return nil
	}
	return map[string]string{"presence": string(data)}
}

func (m *JoinMetadata) UnmarshalMap(md map[string]string) error {
	data, ok := md["presence"]
	if !ok {
		return errors.New("no presence")
	}
	if err := json.Unmarshal([]byte(data), &m.Presence); err != nil {
		return err
	}
	return nil
}

type SlotReservation struct {
	PartyID   string
	SessionID string
	Expiry    time.Time
}

type EvrMatchMeta struct {
	MatchBroadcaster
	Players []EntrantPresence `json:"players,omitempty"` // The displayNames of the players (by team name) in the match.
	// Stats
}
type PlayerInfo struct {
	UserID      string    `json:"user_id,omitempty"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	EvrID       evr.EvrID `json:"evr_id,omitempty"`
	Team        TeamIndex `json:"team"`
	ClientIP    string    `json:"client_ip,omitempty"`
	DiscordID   string    `json:"discord_id,omitempty"`
	PartyID     string    `json:"party_id,omitempty"`
}

type MatchBroadcaster struct {
	SessionID     string       `json:"sid,omitempty"`            // The broadcaster's Session ID
	OperatorID    string       `json:"oper,omitempty"`           // The user id of the broadcaster.
	GroupIDs      []uuid.UUID  `json:"group_ids,omitempty"`      // The channels this broadcaster will host matches for.
	Endpoint      evr.Endpoint `json:"endpoint,omitempty"`       // The endpoint data used for connections.
	VersionLock   uint64       `json:"version_lock,omitempty"`   // The game build version. (EVR)
	AppId         string       `json:"app_id,omitempty"`         // The game app id. (EVR)
	Regions       []evr.Symbol `json:"regions,omitempty"`        // The region the match is hosted in. (Matching Only) (EVR)
	IPinfo        *ipinfo.Core `json:"ip_info,omitempty"`        // The IPinfo of the broadcaster.
	ServerID      uint64       `json:"server_id,omitempty"`      // The server id of the broadcaster. (EVR)
	PublisherLock bool         `json:"publisher_lock,omitempty"` // Publisher lock (EVR)
	Features      []string     `json:"features,omitempty"`       // The features of the broadcaster.
	Tags          []string     `json:"tags,omitempty"`           // The tags given on the urlparam for the match.
}

// The lobby state is used for the match label.
// Any changes to the lobby state should be reflected in the match label.
// This also makes it easier to update the match label, and query against it.
type EvrMatchState struct {
	ID          MatchID          `json:"id"`                    // The Session Id used by EVR (the same as match id)
	Open        bool             `json:"open"`                  // Whether the lobby is open to new players (Matching Only)
	LobbyType   LobbyType        `json:"lobby_type"`            // The type of lobby (Public, Private, Unassigned) (EVR)
	Broadcaster MatchBroadcaster `json:"broadcaster,omitempty"` // The broadcaster's data
	Started     bool             `json:"started"`               // Whether the match has started.
	StartTime   time.Time        `json:"start_time"`            // The time the match was started.
	SpawnedBy   string           `json:"spawned_by,omitempty"`  // The userId of the player that spawned this match.
	GroupID     *uuid.UUID       `json:"group_id,omitempty"`    // The channel id of the broadcaster. (EVR)
	GuildID     string           `json:"guild_id,omitempty"`    // The guild id of the broadcaster. (EVR)
	GuildName   string           `json:"guild_name,omitempty"`  // The guild name of the broadcaster. (EVR)

	Mode             evr.Symbol           `json:"mode,omitempty"`              // The mode of the lobby (Arena, Combat, Social, etc.) (EVR)
	Level            evr.Symbol           `json:"level,omitempty"`             // The level to play on (EVR).
	SessionSettings  *evr.SessionSettings `json:"session_settings,omitempty"`  // The session settings for the match (EVR).
	RequiredFeatures []string             `json:"required_features,omitempty"` // The required features for the match.

	MaxSize     uint8     `json:"limit,omitempty"`        // The total lobby size limit (players + specs)
	Size        int       `json:"size"`                   // The number of players (including spectators) in the match.
	PlayerCount int       `json:"player_count"`           // The number of participants (not including spectators) in the match.
	PlayerLimit int       `json:"player_limit,omitempty"` // The number of players in the match (not including spectators).
	TeamSize    int       `json:"team_size,omitempty"`    // The size of each team in arena/combat (either 4 or 5)
	TeamIndex   TeamIndex `json:"team,omitempty"`         // What team index a player prefers (Used by Matching only)

	Players        []PlayerInfo                `json:"players,omitempty"`         // The displayNames of the players (by team name) in the match.
	Reservations   map[string]SlotReservation  `json:"reservations",omitempty`    // map[sessionID]Reservation
	TeamAlignments map[string]int              `json:"team_alignments,omitempty"` // map[userID]TeamIndex
	presences      map[string]*EntrantPresence // [sessionId]EvrMatchPresence
	broadcaster    runtime.Presence            // The broadcaster's presence

	emptyTicks            int64 // The number of ticks the match has been empty.
	sessionStartExpiry    int64 // The tick count at which the match will be shut down if it has not started.
	broadcasterJoinExpiry int64 // The tick count at which the match will be shut down if the broadcaster has not joined.
	tickRate              int64 // The number of ticks per second.
}

func (s *EvrMatchState) String() string {
	return s.GetLabel()
}

func (s *EvrMatchState) GetLabel() string {
	labelJson, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return ""
	}
	return string(labelJson)
}

func (s *EvrMatchState) GetGroupID() uuid.UUID {
	if s.GroupID == nil {
		return uuid.Nil
	}
	return *s.GroupID
}

func (s *EvrMatchState) GetEndpoint() evr.Endpoint {
	return s.Broadcaster.Endpoint
}

func (s *EvrMatchState) PublicView() *EvrMatchState {
	ps := EvrMatchState{
		LobbyType:        s.LobbyType,
		ID:               s.ID,
		Open:             s.Open,
		Started:          s.Started,
		StartTime:        s.StartTime,
		GroupID:          s.GroupID,
		GuildID:          s.GuildID,
		SpawnedBy:        s.SpawnedBy,
		Mode:             s.Mode,
		Level:            s.Level,
		RequiredFeatures: s.RequiredFeatures,
		MaxSize:          s.MaxSize,
		Size:             s.Size,
		PlayerCount:      s.PlayerCount,
		PlayerLimit:      s.PlayerLimit,
		TeamSize:         s.TeamSize,
		Broadcaster: MatchBroadcaster{
			OperatorID:  s.Broadcaster.OperatorID,
			GroupIDs:    s.Broadcaster.GroupIDs,
			VersionLock: s.Broadcaster.VersionLock,
			Regions:     s.Broadcaster.Regions,
			Tags:        s.Broadcaster.Tags,
		},
		Players: make([]PlayerInfo, len(s.Players)),
	}
	if ps.LobbyType == PrivateLobby || ps.LobbyType == UnassignedLobby {
		ps.ID = MatchID{}
		ps.SpawnedBy = ""
	} else {
		for i := range s.Players {
			ps.Players[i] = PlayerInfo{
				UserID:      s.Players[i].UserID,
				Username:    s.Players[i].Username,
				DisplayName: s.Players[i].DisplayName,
				EvrID:       s.Players[i].EvrID,
				Team:        s.Players[i].Team,
				DiscordID:   s.Players[i].DiscordID,
				PartyID:     s.Players[i].PartyID,
			}
		}
	}
	return &ps
}

func (s *EvrMatchState) GetAvailablePlayerSlots() int {
	return s.PlayerLimit - s.PlayerCount
}

func (s *EvrMatchState) GetAvailableNonPlayerSlots() int {
	return int(s.MaxSize) - s.PlayerLimit
}

func MatchStateFromLabel(label string) (*EvrMatchState, error) {
	state := &EvrMatchState{}
	err := json.Unmarshal([]byte(label), state)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal match label: %v", err)
	}
	return state, nil
}

// rebuildCache is called after the presences map is updated.
func (s *EvrMatchState) rebuildCache() {
	// Rebuild the lookup tables.

	s.Players = make([]PlayerInfo, 0, len(s.presences))
	s.Size = len(s.presences)
	s.PlayerCount = 0
	// Construct Player list
	for _, presence := range s.presences {
		// Do not include spectators or moderators in player count
		if presence.RoleAlignment != evr.TeamSpectator && presence.RoleAlignment != evr.TeamModerator {
			s.PlayerCount++
		}
		playerinfo := PlayerInfo{
			UserID:      presence.UserID.String(),
			Username:    presence.Username,
			DisplayName: presence.DisplayName,
			EvrID:       presence.EvrID,
			Team:        TeamIndex(presence.RoleAlignment),
			ClientIP:    presence.ClientIP,
			DiscordID:   presence.DiscordID,
			PartyID:     presence.PartyID.String(),
		}

		s.Players = append(s.Players, playerinfo)
	}

	sort.SliceStable(s.Players, func(i, j int) bool {
		return s.Players[i].Team < s.Players[j].Team
	})
}

// This is the match handler for all matches.
// There always is one per broadcaster.
// The match is spawned and managed directly by nakama.
// The match can only be communicated with through MatchSignal() and MatchData messages.
type EvrMatch struct{}

// NewEvrMatch is called by the match handler when creating the match.
func NewEvrMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (m runtime.Match, err error) {
	return &EvrMatch{}, nil
}

// NewEvrMatchState is a helper function to create a new match state. It returns the state, params, label json, and err.
func NewEvrMatchState(endpoint evr.Endpoint, config *MatchBroadcaster) (state *EvrMatchState, params map[string]interface{}, configPayload string, err error) {

	var tickRate int64 = 10 // 10 ticks per second

	initialState := EvrMatchState{
		Broadcaster:      *config,
		Open:             false,
		LobbyType:        UnassignedLobby,
		Mode:             evr.ModeUnloaded,
		Level:            evr.LevelUnloaded,
		RequiredFeatures: make([]string, 0),
		Players:          make([]PlayerInfo, 0, MatchMaxSize),
		presences:        make(map[string]*EntrantPresence, MatchMaxSize),
		TeamAlignments:   make(map[string]int, MatchMaxSize),
		Reservations:     make(map[string]SlotReservation),

		emptyTicks: 0,
		tickRate:   tickRate,
	}

	stateJson, err := json.Marshal(initialState)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to marshal match config: %v", err)
	}

	params = map[string]interface{}{
		"initialState": stateJson,
	}

	return &initialState, params, string(stateJson), nil
}

// MatchIDFromContext is a helper function to extract the match id from the context.
func MatchIDFromContext(ctx context.Context) MatchID {
	matchIDStr, ok := ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string)
	if !ok {
		return MatchID{}
	}
	matchID := MatchIDFromStringOrNil(matchIDStr)
	return matchID
}

// MatchInit is called when the match is created.
func (m *EvrMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	state := EvrMatchState{}
	if err := json.Unmarshal(params["initialState"].([]byte), &state); err != nil {
		logger.Error("Failed to unmarshal match config. %s", err)
	}
	const (
		BroadcasterJoinTimeoutSecs = 45
	)

	state.ID = MatchIDFromContext(ctx)
	state.presences = make(map[string]*EntrantPresence)

	state.broadcasterJoinExpiry = state.tickRate * BroadcasterJoinTimeoutSecs

	state.rebuildCache()

	labelJson, err := json.Marshal(state)
	if err != nil {
		logger.WithField("err", err).Error("Match label marshal error.")
		return nil, 0, ""
	}
	if state.tickRate == 0 {
		state.tickRate = 10
	}

	return &state, int(state.tickRate), string(labelJson)
}

var (
	JoinRejectReasonUnassignedLobby    = "unassigned lobby"
	JoinRejectReasonDuplicateJoin      = "duplicate join"
	JoinRejectReasonLobbyFull          = "lobby full"
	JoinRejectReasonFailedToAssignTeam = "failed to assign team"
)

// MatchJoinAttempt decides whether to accept or deny the player session.
func (m *EvrMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	state, ok := state_.(*EvrMatchState)

	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil, false, ""
	}

	logger = logger.WithField("mid", state.ID.String())
	logger = logger.WithField("username", presence.GetUsername())

	if presence.GetSessionId() == state.Broadcaster.SessionID {

		logger.Debug("Broadcaster joining the match.")
		state.broadcaster = presence
		state.Open = true // Available

		if err := m.updateLabel(dispatcher, state); err != nil {
			return state, false, fmt.Sprintf("failed to update label: %q", err)
		}

		return state, true, ""
	}

	// This is a player joining.
	md := JoinMetadata{}
	if err := md.UnmarshalMap(metadata); err != nil {
		return state, false, fmt.Sprintf("failed to unmarshal metadata: %v", err)
	}

	mp, reason := m.playerJoinAttempt(state, md.Presence, md.PartyPresences)
	if reason != "" {
		return state, false, reason
	}
	entrantID := uuid.NewV5(uuid.Nil, mp.GetEvrID())

	state.presences[mp.GetSessionId()] = mp

	logger.WithFields(map[string]interface{}{
		"evr_id":      mp.EvrID.String(),
		"team":        mp.RoleAlignment,
		"sid":         mp.GetSessionId(),
		"uid":         mp.GetUserId(),
		"entrant_sid": entrantID.String()}).Debug("Player joining the match.")

	// Tell the broadcaster to load the match if it's not already started
	if !state.Started {
		var err error
		if state, err = m.StartSession(ctx, logger, nk, dispatcher, state); err != nil {
			return state, false, fmt.Sprintf("failed to start session: %v", err)
		}
	}

	if err := m.updateLabel(dispatcher, state); err != nil {
		return state, false, fmt.Sprintf("failed to update label: %v", err)
	}

	return state, true, mp.String()
}

func (m *EvrMatch) playerJoinAttempt(state *EvrMatchState, mp EntrantPresence, partyPresences []rtapi.UserPresence) (*EntrantPresence, string) {

	// If this is a parking match, reject the player
	if state.LobbyType == UnassignedLobby {
		return &mp, JoinRejectReasonUnassignedLobby
	}

	// If this EvrID is already in the match, reject the player
	for _, p := range state.presences {
		if p.GetSessionId() == mp.GetSessionId() || p.EvrID.Equals(mp.EvrID) {
			return &mp, JoinRejectReasonDuplicateJoin
		}
	}

	// Purge expired reservations
	for s, r := range state.Reservations {
		if r.Expiry.Before(time.Now()) {
			delete(state.Reservations, s)
		}
	}

	openSlots := state.GetAvailablePlayerSlots() - len(state.Reservations)
	partySize := 1 + len(partyPresences)

	// If the lobby is full, reject them
	if openSlots-partySize <= 0 || len(state.presences) >= MatchMaxSize {
		return &mp, JoinRejectReasonLobbyFull
	}

	// Only public matches need to assign teams.
	if state.LobbyType != PublicLobby {
		return &mp, ""
	}

	switch mp.RoleAlignment {
	case evr.TeamUnassigned:
		// Assign the player to a team.
		var allowed bool
		if mp.RoleAlignment, allowed = selectTeamForPlayer(&mp, state); !allowed {
			return &mp, JoinRejectReasonFailedToAssignTeam
		}
	case evr.TeamModerator, evr.TeamSpectator:
		nonPlayerSlots := int(state.MaxSize) - state.PlayerLimit
		nonPlayerCount := state.Size - state.PlayerCount
		if nonPlayerCount >= nonPlayerSlots {
			return &mp, JoinRejectReasonLobbyFull
		}
	case evr.TeamBlue, evr.TeamOrange, evr.TeamSocial:
		if len(state.Players) >= state.PlayerLimit {
			return &mp, JoinRejectReasonLobbyFull
		}
	}

	// Final sanity check to make sure that this isn't adding a player to a 4v4 public match.
	if mp.RoleAlignment == evr.TeamBlue || mp.RoleAlignment == evr.TeamOrange {
		if len(state.Players) >= state.PlayerLimit {
			return &mp, JoinRejectReasonLobbyFull
		}
	}

	// Create reservations for the party members (if any)
	// Only the leader can reserve slots for the party.
	// If this party already has any reservations, they will be purged.
	if partySize > 1 {
		for _, r := range state.Reservations {
			if r.PartyID == mp.PartyID.String() {
				delete(state.Reservations, r.SessionID)
			}
		}
		// only public lobbies need to reserve slots for the party
		if state.LobbyType == PublicLobby {
			// Reserve slots for the party members
			for _, p := range partyPresences {
				if p.GetSessionId() == mp.GetSessionId() {
					continue
				}
				state.Reservations[p.GetSessionId()] = SlotReservation{
					PartyID:   mp.PartyID.String(),
					SessionID: mp.GetSessionId(),
					Expiry:    time.Now().Add(2 * time.Minute),
				}
			}
		}
	}

	return &mp, ""
}

// MatchJoin is called after the join attempt.
// MatchJoin updates the match data, and should not have any decision logic.
func (m *EvrMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, presences []runtime.Presence) interface{} {
	state, ok := state_.(*EvrMatchState)
	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil
	}

	for _, p := range presences {
		if p.GetSessionId() == state.Broadcaster.SessionID {
			continue
		}

		if _, ok := state.presences[p.GetSessionId()]; !ok {
			logger.WithFields(map[string]interface{}{
				"username": p.GetUsername(),
				"uid":      p.GetUserId(),
			}).Error("Presence not found. this should never happen.")
			return nil
		}
	}

	// Remove this player from the reservation list
	for _, p := range presences {
		delete(state.Reservations, p.GetSessionId())
	}

	return state
}

// MatchLeave is called after a player leaves the match.
func (m *EvrMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, presences []runtime.Presence) interface{} {
	state, ok := state_.(*EvrMatchState)
	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil
	}

	// if the broadcaster is in the presences, then shut down.
	for _, p := range presences {
		if p.GetSessionId() == state.Broadcaster.SessionID {
			logger.Debug("Broadcaster left the match. Shutting down.")
			return nil
		}
	}

	for _, p := range presences {
		delete(state.presences, p.GetSessionId())
	}

	if len(state.presences) == 0 {
		// Lock the match
		state.Open = false
		logger.Debug("Match is empty. Closing it.")
	}

	// Update the label that includes the new player list.
	if err := m.updateLabel(dispatcher, state); err != nil {
		return nil
	}

	return state
}

// MatchLoop is called every tick of the match and handles state, plus messages from the client.
func (m *EvrMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, messages []runtime.MatchData) interface{} {
	state, ok := state_.(*EvrMatchState)
	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil
	}

	var err error

	if state.broadcaster == nil {
		if state.LobbyType != UnassignedLobby {
			logger.Error("Parking match has a lobby type. Shutting down.")
			return nil
		}
		if tick > 15*state.tickRate {
			logger.Error("Parking match join timeout expired. Shutting down.")
			return nil
		}
	}
	if state.Started {
		if len(state.presences) == 0 {
			state.emptyTicks++

			if state.emptyTicks > 20*state.tickRate {
				if state.Open {
					logger.Warn("Started match has been empty for too long. Shutting down.")
				}
				return nil
			}
		}
	} else if state.StartTime.Before(time.Now().Add(-10 * time.Minute)) {
		if state.LobbyType != UnassignedLobby {
			logger.Error("Match has not started on time. Shutting down.")
			// Tell the broadcaster to load the level, which it will immediately shut down.
			if state, err = m.StartSession(ctx, logger, nk, dispatcher, state); err != nil {
				logger.Error("failed to start session: %v", err)
				return nil
			}
			if err := m.updateLabel(dispatcher, state); err != nil {
				return nil
			}

		}
	}

	// Handle the messages, one by one
	for _, in := range messages {
		switch in.GetOpCode() {
		default:
			typ, found := evr.SymbolTypes[uint64(in.GetOpCode())]
			if !found {
				logger.Error("Unknown opcode: %v", in.GetOpCode())
				continue
			}
			logger.WithFields(map[string]interface{}{
				"username":   in.GetUsername(),
				"session_id": in.GetSessionId(),
			}).Debug("Received match message %T(%s)", typ, string(in.GetData()))

			// Unmarshal the message into an interface, then switch on the type.
			msg := reflect.New(reflect.TypeOf(typ).Elem()).Interface().(evr.Message)
			if err := json.Unmarshal(in.GetData(), &msg); err != nil {
				logger.Error("Failed to unmarshal message: %v", err)
			}

			var messageFn func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *EvrMatchState, in runtime.MatchData, msg evr.Message) (*EvrMatchState, error)

			// Switch on the message type. This is where the match logic is handled.
			switch msg := msg.(type) {
			default:
				logger.Warn("Unknown message type: %T", msg)
			}
			// Time the execution
			start := time.Now()
			// Execute the message function
			if messageFn != nil {
				state, err = messageFn(ctx, logger, db, nk, dispatcher, state, in, msg)
				if err != nil {
					logger.Error("match pipeline: %v", err)
				}
			}
			logger.Debug("Message %T took %dms", msg, time.Since(start)/time.Millisecond)
		}
	}
	return state
}

// MatchTerminate is called when the match is being terminated.
func (m *EvrMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, graceSeconds int) interface{} {
	state, ok := state_.(*EvrMatchState)
	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil
	}
	logger.Info("MatchTerminate called. %v", state)
	if state.broadcaster != nil {
		// Disconnect the players
		for _, presence := range state.presences {
			nk.SessionDisconnect(ctx, presence.GetSessionId(), runtime.PresenceReasonDisconnect)
		}
		// Disconnect the broadcasters session
		nk.SessionDisconnect(ctx, state.broadcaster.GetSessionId(), runtime.PresenceReasonDisconnect)
	}

	return state
}

type SignalResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Payload string `json:"payload"`
}

func (r SignalResponse) String() string {
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(b)
}

// MatchSignal is called when a signal is sent into the match.
func (m *EvrMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ interface{}, data string) (interface{}, string) {
	state, ok := state_.(*EvrMatchState)
	if !ok {
		logger.Error("state not a valid lobby state object")
		return nil, SignalResponse{Message: "invalid match state"}.String()
	}

	// TODO protobuf's would be nice here.
	signal := &EvrSignal{}
	err := json.Unmarshal([]byte(data), signal)
	if err != nil {
		return state, SignalResponse{Message: fmt.Sprintf("failed to unmarshal signal: %v", err)}.String()
	}

	switch signal.Signal {
	case SignalTerminate:

		return m.MatchTerminate(ctx, logger, db, nk, dispatcher, tick, state, 10), SignalResponse{Success: true}.String()

	case SignalPruneUnderutilized:
		// Prune this match if it's utilization is low.
		if len(state.presences) <= 3 {
			// Free the resources.
			return nil, SignalResponse{Success: true}.String()
		}
	case SignalGetEndpoint:
		jsonData, err := json.Marshal(state.Broadcaster.Endpoint)
		if err != nil {
			return state, fmt.Sprintf("failed to marshal endpoint: %v", err)
		}
		return state, SignalResponse{Success: true, Payload: string(jsonData)}.String()

	case SignalGetPresences:
		// Return the presences in the match.

		jsonData, err := json.Marshal(state.presences)
		if err != nil {
			return state, fmt.Sprintf("failed to marshal presences: %v", err)
		}
		return state, SignalResponse{Success: true, Payload: string(jsonData)}.String()

	case SignalPrepareSession:

		// if the match is already started, return an error.
		if state.LobbyType != UnassignedLobby {
			logger.Error("Failed to prepare session: session already prepared")
			return state, SignalResponse{Message: "session already prepared"}.String()
		}

		var newState = EvrMatchState{}
		if err := json.Unmarshal(signal.Payload, &newState); err != nil {
			return state, SignalResponse{Message: fmt.Sprintf("failed to unmarshal match label: %v", err)}.String()
		}

		state.Started = false
		state.Open = true

		state.Mode = newState.Mode
		switch newState.Mode {
		case evr.ModeArenaPublic, evr.ModeSocialPublic, evr.ModeCombatPublic:
			state.LobbyType = PublicLobby
		default:
			state.LobbyType = PrivateLobby
		}

		state.Level = newState.Level
		// validate the mode
		if levels, ok := evr.LevelsByMode[state.Mode]; !ok {
			return state, SignalResponse{Message: fmt.Sprintf("invalid mode: %v", state.Mode)}.String()
		} else {
			if state.Level == 0xffffffffffffffff || state.Level == 0 {
				state.Level = levels[rand.Intn(len(levels))]
			}
		}

		if newState.SpawnedBy != "" {
			state.SpawnedBy = newState.SpawnedBy
		} else {
			state.SpawnedBy = signal.UserId
		}

		state.GroupID = newState.GroupID
		if state.GroupID == nil {
			state.GroupID = &uuid.Nil
		}

		state.RequiredFeatures = newState.RequiredFeatures
		for _, f := range state.RequiredFeatures {
			if !slices.Contains(state.Broadcaster.Features, f) {
				return state, SignalResponse{Message: fmt.Sprintf("feature not supported: %v", f)}.String()
			}
		}

		settings := evr.NewSessionSettings(strconv.FormatUint(PcvrAppId, 10), state.Mode, state.Level, state.RequiredFeatures)
		state.SessionSettings = &settings

		state.MaxSize = newState.MaxSize
		if state.MaxSize < 2 || state.MaxSize > MatchMaxSize {
			state.MaxSize = MatchMaxSize
		}

		state.TeamSize = newState.TeamSize
		if state.TeamSize <= 0 || state.TeamSize > 5 {
			state.TeamSize = 5
		}

		state.StartTime = newState.StartTime
		if state.StartTime.IsZero() || state.StartTime.Before(time.Now()) {
			state.StartTime = time.Now()
		}
		state.sessionStartExpiry = tick + (15 * 60 * state.tickRate)

		if state.Mode == evr.ModeSocialPrivate || state.Mode == evr.ModeSocialPublic {
			state.PlayerLimit = int(state.MaxSize)
		} else {
			state.PlayerLimit = state.TeamSize * 2
		}

		state.TeamAlignments = make(map[string]int, MatchMaxSize)
		if newState.TeamAlignments != nil {
			for userID, role := range newState.TeamAlignments {
				if userID != "" {
					state.TeamAlignments[userID] = int(role)
				}
			}
		}

	case SignalStartSession:

		state, err := m.StartSession(ctx, logger, nk, dispatcher, state)
		if err != nil {
			return state, SignalResponse{Message: fmt.Sprintf("failed to start session: %v", err)}.String()
		}

	case SignalEndSession:
		state.Open = false

	case SignalLockSession:
		state.Open = false

	case SignalUnlockSession:
		logger.Debug("ignoring unlock session request")
		//state.Open = true

	default:
		logger.Warn("Unknown signal: %v", signal.Signal)
		return state, SignalResponse{Success: false, Message: "unknown signal"}.String()
	}

	if err := m.updateLabel(dispatcher, state); err != nil {
		logger.Error("failed to update label: %v", err)
		return state, SignalResponse{Message: fmt.Sprintf("failed to update label: %v", err)}.String()
	}

	return state, SignalResponse{Success: true, Payload: state.String()}.String()

}

func (m *EvrMatch) StartSession(ctx context.Context, logger runtime.Logger, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *EvrMatchState) (*EvrMatchState, error) {
	channel := uuid.Nil
	if state.GroupID != nil {
		channel = *state.GroupID
	}
	state.Started = true
	state.StartTime = time.Now()
	entrants := make([]evr.EvrID, 0)
	message := evr.NewBroadcasterStartSession(state.ID.UUID(), channel, state.MaxSize, uint8(state.LobbyType), state.Broadcaster.AppId, state.Mode, state.Level, state.RequiredFeatures, entrants)
	logger.Info("Starting session. %v", message)
	messages := []evr.Message{
		message,
	}

	// Dispatch the message for delivery.
	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.broadcaster}, nil); err != nil {
		return state, fmt.Errorf("failed to dispatch message: %v", err)
	}
	return state, nil
}

// SignalMatch is a helper function to send a signal to a match.
func SignalMatch(ctx context.Context, matchRegistry MatchRegistry, matchID MatchID, signalID int64, data interface{}) (string, error) {
	dataJson, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal match label: %v", err)
	}
	signal := EvrSignal{
		Signal:  signalID,
		Payload: dataJson,
	}
	signalJson, err := json.Marshal(signal)
	if err != nil {
		return "", fmt.Errorf("failed to marshal match signal: %v", err)
	}
	responseJSON, err := matchRegistry.Signal(ctx, matchID.String(), string(signalJson))
	if err != nil {
		return "", fmt.Errorf("failed to signal match: %v", err)
	}
	response := SignalResponse{}
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %v", err)
	}
	if !response.Success {
		return "", fmt.Errorf("match signal response: %v", response.Message)
	}
	return response.Payload, nil
}

func (m *EvrMatch) dispatchMessages(_ context.Context, logger runtime.Logger, dispatcher runtime.MatchDispatcher, messages []evr.Message, presences []runtime.Presence, sender runtime.Presence) error {
	bytes := []byte{}
	for _, message := range messages {

		logger.Debug("Sending message from match: %v", message)
		payload, err := evr.Marshal(message)
		if err != nil {
			return fmt.Errorf("could not marshal message: %v", err)
		}
		bytes = append(bytes, payload...)
	}
	if err := dispatcher.BroadcastMessageDeferred(OpCodeEVRPacketData, bytes, presences, sender, true); err != nil {
		return fmt.Errorf("could not broadcast message: %v", err)
	}
	return nil
}

func (m *EvrMatch) updateLabel(dispatcher runtime.MatchDispatcher, state *EvrMatchState) error {
	state.rebuildCache()
	if err := dispatcher.MatchLabelUpdate(state.GetLabel()); err != nil {
		return fmt.Errorf("could not update label: %v", err)
	}
	return nil
}

func checkIfGlobalDeveloper(ctx context.Context, nk runtime.NakamaModule, userID uuid.UUID) (bool, error) {
	return checkGroupMembershipByName(ctx, nk, userID.String(), GroupGlobalDevelopers, SystemGroupLangTag)
}

func checkIfGlobalBot(ctx context.Context, nk runtime.NakamaModule, userID uuid.UUID) (bool, error) {
	return checkGroupMembershipByName(ctx, nk, userID.String(), GroupGlobalBots, SystemGroupLangTag)
}

func checkIfGlobalModerator(ctx context.Context, nk runtime.NakamaModule, userID uuid.UUID) (bool, error) {
	// Developers are moderators
	ok, err := checkGroupMembershipByName(ctx, nk, userID.String(), GroupGlobalDevelopers, SystemGroupLangTag)
	if err != nil {
		return false, fmt.Errorf("error getting user groups: %w", err)
	}
	if ok {
		return true, nil
	}
	return checkGroupMembershipByName(ctx, nk, userID.String(), GroupGlobalModerators, SystemGroupLangTag)
}

func checkGroupMembershipByName(ctx context.Context, nk runtime.NakamaModule, userID, groupName, langtag string) (bool, error) {
	var err error
	var groups []*api.UserGroupList_UserGroup
	var cursor string
	for {
		if groups, cursor, err = nk.UserGroupsList(ctx, userID, 100, nil, cursor); err != nil {
			return false, fmt.Errorf("error getting user groups: %w", err)
		}
		for _, g := range groups {
			if g.Group.Name == groupName && g.State.GetValue() <= int32(api.UserGroupList_UserGroup_MEMBER) && g.Group.LangTag == langtag {
				return true, nil
			}
		}
		if cursor == "" {
			break
		}
	}
	return false, nil
}
