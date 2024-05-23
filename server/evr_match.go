package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
)

const (
	VersionLock       uint64 = 0xc62f01d78f77910d // The game build version.
	MatchmakingModule        = "evr"              // The module used for matchmaking

	MatchMaxSize = 12 // The total max players (not including the broadcaster) for a EVR lobby.

	LevelSelectionFirst  MatchLevelSelection = "first"
	LevelSelectionRandom MatchLevelSelection = "random"

	StatGroupArena  MatchStatGroup = "arena"
	StatGroupCombat MatchStatGroup = "combat"
	
	SignalCodePrepareSession = iota
	SignalCodeGetEndpoint
	SignalCodeGetPresences
	SignalCodePruneUnderutilized
	SignalCodeTerminateMatch
)

type MatchStatGroup string
type MatchLevelSelection string

const (
	MatchModule = "evrmatch"
)

var _ runtime.Presence = &PlayerPresence{}

// Represents identity information for a single match participant.
type PlayerPresence struct {
	UserID       uuid.UUID       `json:"userid,omitempty"`
	SessionID    uuid.UUID       `json:"session_id,omitempty"`
	Username     string          `json:"username,omitempty"`
	DisplayName  string          `json:"display_name,omitempty"`
	ClientIP     string          `json:"client_ip,omitempty"`
	EvrID        evr.EvrId       `json:"evr_id,omitempty"`
	PartyID      uuid.UUID       `json:"party_id,omitempty"`
	DiscordID    string          `json:"discord_id,omitempty"`
	SessionFlags SessionFlags    `json:"session_flags,omitempty"`
	VersionLock  evr.Symbol      `json:"version_lock,omitempty"`
	Alignment    evr.Role        `json:"alignment,omitempty"`
	Memberships  []ChannelMember `json:"memberships,omitempty"`
	Node         string          `json:"node,omitempty"`
}

func (p *PlayerPresence) GetUserId() string    { return p.UserID.String() }
func (p *PlayerPresence) GetSessionId() string { return p.SessionID.String() }
func (p *PlayerPresence) GetNodeId() string    { return p.Node }
func (p *PlayerPresence) GetHidden() bool      { return false }
func (p *PlayerPresence) GetPersistence() bool { return false }
func (p *PlayerPresence) GetUsername() string  { return p.Username }
func (p *PlayerPresence) GetStatus() string    { return "" }
func (p *PlayerPresence) GetReason() runtime.PresenceReason {
	return runtime.PresenceReasonUnknown
}
func (p *PlayerPresence) GetEvrId() string { return p.EvrID.Token() }

func (p *PlayerPresence) GetChannels() []uuid.UUID {
	return lo.Map(p.Memberships, func(m ChannelMember, _ int) uuid.UUID { return m.ChannelID })
}

type EvrMatchMeta struct {
	MatchBroadcaster
	Players []EvrMatchPresence `json:"players,omitempty"` // The displayNames of the players (by team name) in the match.
	// Stats
}
type PlayerInfo struct {
	UserID      string    `json:"user_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	EvrID       evr.EvrId `json:"evr_id,omitempty"`
	Team        TeamIndex `json:"team"`
	ClientIP    string    `json:"client_ip,omitempty"`
	DiscordID   string    `json:"discord_id,omitempty"`
	PartyID     string    `json:"party_id,omitempty"`
func NewPlayerPresence(session *sessionWS, versionLock evr.Symbol, alignment evr.Role) PlayerPresence {
	ctx := session.Context()

	flags := ctx.Value(ctxFlagsKey{}).(SessionFlags)
	evrID := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	channels := ctx.Value(ctxChannelsKey{}).([]ChannelMember)
	discordID := ctx.Value(ctxDiscordIDKey{}).(string)

	return PlayerPresence{
		UserID:       session.UserID(),
		SessionID:    session.ID(),
		Username:     session.Username(),
		DisplayName:  channels[0].DisplayName,
		ClientIP:     session.ClientIP(),
		EvrID:        evrID,
		VersionLock:  versionLock,
		DiscordID:    discordID,
		SessionFlags: flags,
		Alignment:    alignment,
		Node:         session.pipeline.node,
		Memberships:  channels,
	}
}

type MatchSettings struct {
	VersionLock      evr.Symbol             `json:"version_lock,omitempty"`      // The game build version. (EVR)
	LobbyType        evr.LobbyType          `json:"lobby_type,omitempty"`        // The type of lobby (Public, Private, Unassigned) (EVR)
	Mode             evr.Symbol             `json:"mode,omitempty"`              // The mode of the lobby (Arena, Combat, Social, etc.) (EVR)
	Level            evr.Symbol             `json:"level,omitempty"`             // The level to play on (EVR).
	Levels           []evr.Symbol           `json:"levels,omitempty"`            // The levels to choose from (EVR).
	LevelSelection   MatchLevelSelection    `json:"level_selection,omitempty"`   // The level selection method (EVR).
	ParticipantLimit int                    `json:"participant_limit,omitempty"` // The total lobby size limit (players + specs)
	RoleCapacities   RoleCapacities         `json:"role_capacities,omitempty"`   // The role limits for the match.
	TeamAlignments   map[uuid.UUID]evr.Role `json:"team_alignments,omitempty"`   // The team alignments for the match.
}

func NewMatchSettingsFromMode(mode evr.Symbol, versionLock evr.Symbol) MatchSettings {
	s := MatchSettings{
		VersionLock:      versionLock,
		LobbyType:        evr.PrivateLobby,
		ParticipantLimit: MatchMaxSize,
		Level:            evr.LevelArena,
		Levels:           nil,
		Mode:             mode,
		LevelSelection:   "",
		RoleCapacities: RoleCapacities{
			BlueTeam:   5,
			OrangeTeam: 5,
			Spectator:  MatchMaxSize - 8,
		},
		TeamAlignments: make(map[uuid.UUID]evr.Role),
	}
	// Set the mode and appropriate settings related to the mode.

	switch mode {

	case evr.ModeSocialNPE:

		s.Level = evr.LevelSocial

		s.RoleCapacities = RoleCapacities{
			Social: 1,
		}

	case evr.ModeArenaTutorial:

		s.Level = evr.LevelArena

		s.RoleCapacities = RoleCapacities{
			BlueTeam: 1,
		}

	case evr.ModeSocialPublic:

		s.LobbyType = evr.PublicLobby

		fallthrough
	case evr.ModeSocialPrivate:

		s.Level = evr.LevelSocial

		s.RoleCapacities = RoleCapacities{
			Social:    MatchMaxSize - 1,
			Moderator: MatchMaxSize - 1,
		}

	case evr.ModeCombatPublic:

		s.LobbyType = evr.PublicLobby

		s.RoleCapacities = RoleCapacities{
			BlueTeam:   4,
			OrangeTeam: 4,
			Spectator:  MatchMaxSize - 8,
		}

		fallthrough
	case evr.ModeCombatPrivate:

		s.Level = evr.LevelUnspecified
		s.LevelSelection = LevelSelectionRandom

	case evr.ModeArenaPublicAI, evr.ModeArenaPublic:

		s.LobbyType = evr.PublicLobby

		s.RoleCapacities = RoleCapacities{
			BlueTeam:   4,
			OrangeTeam: 4,
			Spectator:  MatchMaxSize - 8,
		}
	}

	return s
}

type MatchMetadata struct {
	SpawnedBy string    `json:"spawned_by,omitempty"` // The userId of the player that spawned this match.
	Channel   uuid.UUID `json:"channel_id,omitempty"` // The channel id of the session. (EVR)
	GuildID   string    `json:"guild_id,omitempty"`   // The guild id of the channel. (EVR)
	GuildName string    `json:"guild_name,omitempty"` // The guild name of the channel. (EVR)
	StartedAt time.Time `json:"started_at,omitempty"` // The time the match was started.
}

type MatchLabel struct {
	matchState
}

func (s *MatchLabel) UnmarshalLabel(l string) error {
	return json.Unmarshal([]byte(l), s)
}

func (s *MatchLabel) MarshalLabel() (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func (s *MatchLabel) GetParticipantCount() int {
	return len(s.Presences)
}

func (s *MatchLabel) GetPlayerCount() int {
	return len(lo.Filter(lo.Values(s.Presences), func(p *PlayerPresence, _ int) bool {
		return p.Alignment != evr.SpectatorRole && p.Alignment != evr.ModeratorRole
	}))
}

// The lobby state is used for the match label.
// Any changes to the lobby state should be reflected in the match label.
// This also makes it easier to update the match label, and query against it.
type EvrMatchState struct {
	MatchID     uuid.UUID        `json:"id,omitempty"`          // The Session Id used by EVR (the same as match id)
	Open        bool             `json:"open,omitempty"`        // Whether the lobby is open to new players (Matching Only)
	Node        string           `json:"node,omitempty"`        // The node the match is running on.
	LobbyType   LobbyType        `json:"lobby_type"`            // The type of lobby (Public, Private, Unassigned) (EVR)
	Broadcaster MatchBroadcaster `json:"broadcaster,omitempty"` // The broadcaster's data
	Started     bool             `json:"started"`               // Whether the match has started.
	StartedAt   time.Time        `json:"started_at,omitempty"`  // The time the match was started.
	SpawnedBy   string           `json:"spawned_by,omitempty"`  // The userId of the player that spawned this match.
	Channel     *uuid.UUID       `json:"channel,omitempty"`     // The channel id of the broadcaster. (EVR)
	GuildID     string           `json:"guild_id,omitempty"`    // The guild id of the broadcaster. (EVR)
	GuildName   string           `json:"guild_name,omitempty"`  // The guild name of the broadcaster. (EVR)

	Mode             evr.Symbol           `json:"mode,omitempty"`              // The mode of the lobby (Arena, Combat, Social, etc.) (EVR)
	Level            evr.Symbol           `json:"level,omitempty"`             // The level to play on (EVR).
	LevelSelection   MatchLevelSelection  `json:"level_selection,omitempty"`   // The level selection method (EVR).
	SessionSettings  *evr.SessionSettings `json:"session_settings,omitempty"`  // The session settings for the match (EVR).
	RequiredFeatures []string             `json:"required_features,omitempty"` // The required features for the match.

	Size           int       `json:"size,omitempty"`             // The total lobby size limit (players + specs)
	MaxSize        int       `json:"max_size,omitempty"`         // The total lobby size limit (players + specs)
	PlayerCount    int       `json:"player_count,omitempty"`     // The number of players (not including spectators) in the match.
	MaxPlayerCount int       `json:"max_player_count,omitempty"` // The maximum number of players (not including spectators) in the match.
	TeamSize       int       `json:"team_size,omitempty"`        // The size of each team in arena/combat (either 4 or 5)
	TeamIndex      TeamIndex `json:"team,omitempty"`             // What team index a player prefers (Used by Matching only)

	Players        []PlayerInfo                    `json:"players,omitempty"` // The displayNames of the players (by team name) in the match.
	teamAlignments map[evr.EvrId]int               // [evrID]TeamIndex
	presences      map[uuid.UUID]*EvrMatchPresence // [sessionId]EvrMatchPresence
	broadcaster    runtime.Presence                // The broadcaster's presence
	presenceCache  map[uuid.UUID]*EvrMatchPresence // [sessionId]PlayerMeta cache for all players that have attempted to join the match.
	emptyTicks     int                             // The number of ticks the match has been empty.
	tickRate       int                             // The number of ticks per second.
type matchState struct {
	ID     MatchID `json:"id,omitempty"` // The Session Id used by EVR (the same as match id)
	Open   bool    `json:"open"`         // Whether the lobby is open to new players (Matching Only)
	Locked bool    `json:"locked"`       // Whether the lobby is locked to new players (EVR)

	Settings MatchSettings `json:"settings,omitempty"` // The settings for the match (EVR)
	Metadata MatchMetadata `json:"metadata,omitempty"` // The metadata for the match (EVR)

	Broadcaster BroadcasterPresence           `json:"broadcaster,omitempty"`  // The broadcaster's data
	Presences   map[uuid.UUID]*PlayerPresence `json:"participants,omitempty"` // map[playerSession]*PlayerPresence The participants in the match.

	emptyTicks int // The number of ticks the match has been empty.
	tickRate   int // The tick rate of the match.
}

func (s *matchState) MatchID() string {
	return s.ID.String()
}

func (s *matchState) UUID() uuid.UUID {
	return s.ID.UUID()
}

func (s *matchState) Node() string {
	return s.ID.Node()
}

func (s *matchState) String() string {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *matchState) GetLabel() string {

	labelJson, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return ""
	}
	return string(labelJson)
}
func (s *matchState) Started() bool {
	return !s.Metadata.StartedAt.IsZero()
}

func (s *matchState) getRoleOpenings() map[evr.Role]int {
	roles := make(map[evr.Role]int)

	for _, p := range s.Presences {
		roles[p.Alignment]++
	}
	for r, v := range roles {
		roles[r] = s.Settings.RoleCapacities.Get(r) - v
	}
	return roles
}

type RoleCapacities struct {
	BlueTeam   int `json:"blue_team"`
	OrangeTeam int `json:"orange_team"`
	Social     int `json:"social"`
	Spectator  int `json:"spectator"`
	Moderator  int `json:"moderator"`
}

func (c RoleCapacities) AsMap() map[evr.Role]int {
	return map[evr.Role]int{
		evr.BlueTeamRole:   c.BlueTeam,
		evr.OrangeTeamRole: c.OrangeTeam,
		evr.SocialRole:     c.Social,
		evr.SpectatorRole:  c.Spectator,
		evr.ModeratorRole:  c.Moderator,
	}
}

func (c RoleCapacities) Count() int {
	return c.BlueTeam + c.OrangeTeam + c.Social + c.Spectator + c.Moderator
}

func (s RoleCapacities) Get(role evr.Role) int {
	switch role {
	case evr.BlueTeamRole:
		return s.BlueTeam
	case evr.OrangeTeamRole:
		return s.OrangeTeam
	case evr.SocialRole:
		return s.Social
	case evr.SpectatorRole:
		return s.Spectator
	case evr.ModeratorRole:
		return s.Moderator
	default:
		return 0
	}
}

func (s *RoleCapacities) Set(role evr.Role, capacity int) {
	switch role {
	case evr.BlueTeamRole:
		s.BlueTeam = capacity
	case evr.OrangeTeamRole:
		s.OrangeTeam = capacity
	case evr.SocialRole:
		s.Social = capacity
	case evr.SpectatorRole:
		s.Spectator = capacity
	case evr.ModeratorRole:
		s.Moderator = capacity
	}
}

func (s *RoleCapacities) String() string {
	return fmt.Sprintf("Blu/Orn/Soc/Spc/Mod: %d/%d/%d/%d/%d", s.BlueTeam, s.OrangeTeam, s.Social, s.Spectator, s.Moderator)
}

// This is the match handler for all matches.
// The match is spawned and managed directly by nakama.
// The match can only be communicated with through MatchSignal() and MatchData messages.
type EVRMatchHandler struct {
	codec *evr.Codec
}

// NewEvrMatch is called by the match handler when creating the match.
func NewEvrMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (m runtime.Match, err error) {

	return &EVRMatchHandler{
		codec: evr.NewCodec(nil),
	}, nil
}

// NewEvrMatchState is a helper function to create a new match state. It returns the state, params, label json, and err.
func NewEvrMatchState(endpoint evr.Endpoint, config *MatchBroadcaster, sessionId string, node string) (state *EvrMatchState, params map[string]interface{}, configPayload string, err error) {
	// TODO It might be better to have the broadcaster just waiting on a stream, and be constantly matchmaking along with users,
	// Then when the matchmaker decides spawning a new server is nominal approach, the broadcaster joins the match along with
	// the players. It would have to join first though.  - @thesprockee
	initState := &EvrMatchState{
		Node:           node,
		StartedAt:      time.Now(),
		Broadcaster:    *config,
		SpawnedBy:      config.OperatorID,
		Open:           false,
		LobbyType:      UnassignedLobby,
		Mode:           evr.ModeUnloaded,
		Level:          evr.LevelUnloaded,
		Players:        make([]PlayerInfo, 0, MatchMaxSize),
		presences:      make(map[uuid.UUID]*EvrMatchPresence, MatchMaxSize),
		presenceCache:  make(map[uuid.UUID]*EvrMatchPresence, MatchMaxSize),
		teamAlignments: make(map[evr.EvrId]int, MatchMaxSize),
		emptyTicks:     0,
		tickRate:       10,
// MatchIdFromContext is a helper function to extract the match id from the context.
func MatchIdFromContext(ctx context.Context) MatchID {
	return MatchIDFromStringOrNil(ctx.Value(runtime.RUNTIME_CTX_MATCH_ID).(string))
}

type MatchParameters struct {
	Broadcaster BroadcasterPresence `json:"broadcaster"`
	Settings    MatchSettings       `json:"settings"`
	Metadata    MatchMetadata       `json:"metadata"`
	TickRate    int                 `json:"tick_rate"`
}

func (p MatchParameters) AsMap() map[string]any {
	return map[string]any{
		"settings":    p.Settings,
		"metadata":    p.Metadata,
		"broadcaster": p.Broadcaster,
		"tick_rate":   p.TickRate,
	}
}

func (p *MatchParameters) FromMap(m map[string]any) error {
	var ok bool
	if p.Broadcaster, ok = m["broadcaster"].(BroadcasterPresence); !ok {
		p.TickRate = m["tick_rate"].(int)
		return fmt.Errorf("failed to parse broadcaster: %v", m["broadcaster"])
	}
	if p.TickRate, ok = m["tick_rate"].(int); !ok {
		return fmt.Errorf("failed to parse tick_rate: %v", m["tick_rate"])
	}
	if p.Settings, ok = m["settings"].(MatchSettings); !ok {
		return fmt.Errorf("failed to parse settings: %v", m["settings"])
	}
	if p.Metadata, ok = m["metadata"].(MatchMetadata); !ok {
		return fmt.Errorf("failed to parse metadata: %v", m["metadata"])
	}
	return nil
}

// MatchInit is called when the match is created.
func (m *EVRMatchHandler) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]any) (any, int, string) {

	state := &matchState{
		ID:        MatchIdFromContext(ctx),
		Presences: make(map[uuid.UUID]*PlayerPresence, MatchMaxSize),
	}

	var ok bool
	if state.Broadcaster, ok = params["broadcaster"].(BroadcasterPresence); !ok {
		return nil, 0, "failed to parse broadcaster"
	}
	if state.tickRate, ok = params["tick_rate"].(int); !ok {
		return nil, 0, "failed to parse tick_rate"
	}
	if state.Settings, ok = params["settings"].(MatchSettings); !ok {
		return nil, 0, "failed to parse settings"
	}
	if state.Metadata, ok = params["metadata"].(MatchMetadata); !ok {
		return nil, 0, "failed to parse metadata"
	}

	// Get the broadcaster's context
	session, ok := nk.(*RuntimeGoNakamaModule).sessionRegistry.Get(state.Broadcaster.SessionID).(*sessionWS)
	if !ok {
		logger.Error("Broadcaster session not found.")
		return nil, 0, ""
	}
	state.Broadcaster.session = session

	return state, state.tickRate, state.GetLabel()
}

var (
	ErrJoinRejectedDuplicateJoin  = errors.New("duplicate join")
	ErrJoinRejectedLobbyFull      = errors.New("lobby full")
	ErrJoinRejectedLobbyLocked    = errors.New("lobby locked")
	ErrUpdatingLabel              = errors.New("internal error updating label")
	ErrFailedToStartSession       = errors.New("failed to start session")
	JoinMetadataPlayerPresenceKey = "playerPresence"
)

// MatchJoinAttempt decides whether to accept or deny the player session.
func (m *EVRMatchHandler) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, presence runtime.Presence, metadata map[string]string) (any, bool, string) {
	state, allow := state_.(*matchState)
	if !allow {
		return nil, false, ""
	}

	// If the lobby is full, reject them
	if len(state.Presences) >= MatchMaxSize {
		return state, false, ErrJoinRejectedLobbyFull.Error()
	}

	// Unmarshal the player metadata
	mp := &PlayerPresence{}
	if err := json.Unmarshal([]byte(metadata[JoinMetadataPlayerPresenceKey]), &mp); err != nil {
		return state, false, fmt.Sprintf("failed to unmarshal metadata: %q", err)
	}

	// Reject the player if they are a duplicate.
	for _, p := range state.Presences {
		if p.EvrID.Equals(mp.EvrID) || p.GetSessionId() == presence.GetSessionId() {
			return state, false, ErrJoinRejectedDuplicateJoin.Error()
		}
	}

	if mp.Alignment, allow = selectPlayerRole(logger, mp.Alignment, state); !allow {
		// The lobby is full, reject the player.
		return state, false, ErrJoinRejectedLobbyFull.Error()
	}

	state.Presences[uuid.Must(uuid.NewV4())] = mp

	// Update the label
	if err := dispatcher.MatchLabelUpdate(state.GetLabel()); err != nil {
		return state, false, errors.Join(ErrUpdatingLabel, err).Error()
	}

	// If the match is already started, send the player start message
	if !state.Started() {
		// If the match has not started yet, instruct the broadcaster to start the session and load the level.
		// The broadcaster will then send a SessionStarted message, which will trigger sending messages to any players who have previously joined.
		if err := m.StartSession(ctx, logger, nk, dispatcher, state); err != nil {
			logger.Error("Failed to start session, disconnecting broadcaster: %v", err)
			if err := nk.SessionDisconnect(ctx, state.Broadcaster.GetSessionId(), runtime.PresenceReasonDisconnect); err != nil {
				logger.Error("Failed to disconnect broadcaster: %v", err)
			}
			return nil, false, errors.Join(ErrFailedToStartSession, err).Error()
		}
	} else {
		// If the match has already started, send the player start message.
		if err := m.sendPlayerStart(ctx, logger, dispatcher, state, mp); err != nil {
			return state, false, fmt.Sprintf("Failed to send player start: %q", err)
		}
	}

	return state, true, ""
}

// selectPlayerRole decides which team to assign a player to.
func selectPlayerRole(logger runtime.Logger, t evr.Role, state *matchState) (evr.Role, bool) {

	roles := lo.GroupBy(lo.Values(state.Presences), func(p *PlayerPresence) evr.Role { return p.Alignment })

	c := state.Settings.RoleCapacities

	// Allow moderators, spectators, and social players to join if there is space, otherwise reject.
	if t == evr.ModeratorRole || t == evr.SpectatorRole || t == evr.SocialRole {
		return t, c.Get(t)-len(roles[t]) > 0
	}

	// If the teams are balanced or the lobby is closed (just starting), then allow them to join the team they requested (if it's not full)
	if len(roles[evr.BlueTeamRole]) == len(roles[evr.OrangeTeamRole]) || !state.Open && c.Get(t)-len(roles[t]) > 0 {
		return t, true
	}

	// If the teams are unbalanced, then assign them to the team with fewer players.
	if len(roles[evr.BlueTeamRole]) > len(roles[evr.OrangeTeamRole]) {
		t = evr.OrangeTeamRole
	} else if len(roles[evr.OrangeTeamRole]) > len(roles[evr.BlueTeamRole]) {
		t = evr.BlueTeamRole
	} else if state.Settings.LobbyType == evr.PrivateLobby {
		// If the teams are full (balanced), and this is a private match, then assign them to the spectator role.
		t = evr.SpectatorRole
	}

	// If their team is full, reject them
	return t, c.Get(t)-len(roles[t]) <= 0
}

// MatchJoin is called after the join attempt.
// MatchJoin updates the match data, and should not have any decision logic.
func (m *EVRMatchHandler) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, presences []runtime.Presence) any {
	state, ok := state_.(*matchState)
	if !ok {
		return nil
	}

	for _, p := range presences {

		logger = logger.WithFields(map[string]any{
			"sid":      p.GetSessionId(),
			"username": p.GetUsername(),
			"uid":      p.GetUserId(),
		})

		// Update the player's status to include the match ID
		if err := nk.StreamUserUpdate(StreamModeEvr, p.GetUserId(), StreamContextMatch.String(), "", p.GetUserId(), p.GetSessionId(), false, false, state.ID.String()); err != nil {
			logger.Warn("Failed to update user status: %v", err)
		}

		// Update the player's status to include the match ID
		if err := nk.StreamUserUpdate(StreamModeEvr, p.GetUserId(), StreamContextMatch.String(), "", p.GetUserId(), p.GetSessionId(), false, false, state.ID()); err != nil {
			logger.Warn("Failed to update user status: %v", err)
		}

		// Send this after the function returns to ensure the match is ready to receive the player.
		err := m.sendPlayerStart(ctx, logger, dispatcher, state, matchPresence)
		if err != nil {
			logger.Error("failed to send player start: %v", err)
		}

	}

	state.rebuildCache()
	// Update the label that includes the new player list.
	err := m.updateLabel(dispatcher, state)
	if err != nil {
		logger.Error("failed to update label: %v", err)
	}

	// Update the label
	if err := dispatcher.MatchLabelUpdate(state.GetLabel()); err != nil {
		logger.Warn("Failed to update label: %v", err)
	}

	return state
}

func (m *EVRMatchHandler) sendPlayerStart(ctx context.Context, logger runtime.Logger, dispatcher runtime.MatchDispatcher, state *matchState, player *PlayerPresence) error {

	disableSecurity := player.SessionFlags.DisableProtocolSecurity || state.Broadcaster.SessionFlags.DisableProtocolSecurity
	gameMode := state.Settings.Mode
	teamIndex := player.Alignment
	channel := state.Metadata.Channel
	matchID := state.ID.uuid
	endpoint := state.Broadcaster.Endpoint

	success := evr.NewLobbySessionSuccess(gameMode, matchID, channel, endpoint, teamIndex, disableSecurity)
	successV4 := success.Version4()
	successV5 := success.Version5()
	messages := []evr.Message{
		successV4,
		successV5,
		evr.NewSTcpConnectionUnrequireEvent(),
	}

	// Dispatch the message for delivery.
	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.Broadcaster, player}, nil); err != nil {
		logger.Error("failed to dispatch success message to broadcaster: %v", err)
	}
	return nil

}

// MatchLeave is called after a player leaves the match.
func (m *EVRMatchHandler) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, presences []runtime.Presence) any {
	state, ok := state_.(*matchState)
	if !ok {
		return nil
	}

	// if the broadcaster is in the presences, then shut down.
	for _, p := range presences {
		if p.GetSessionId() == state.Broadcaster.SessionID {
			logger.Debug("Broadcaster left the match. Shutting down.")
			return nil
		}
	}

	// Create a list of sessions to remove
	rejects := lo.Map(presences, func(p runtime.Presence, _ int) string {
		// Get the match presence for this user
		matchPresence, ok := state.presences[p.GetSessionId()]
		if !ok || matchPresence == nil {
			return ""
		}
		return matchPresence.GetPlayerSession()
	})

	// Filter out the uuid.Nil's
	rejects = lo.Filter(rejects, func(u string, _ int) bool {
		return u != ""
	})

	for _, p := range presences {
		// Update the player's status to remove the match ID
		// Check if the session exists.

		sessionID := uuid.FromStringOrNil(p.GetSessionId())
		matchPresence, ok := state.presences[sessionID]
		if !ok || matchPresence == nil {
			continue
		}
		delete(state.presences, sessionID)
		// Update the player's status to remove the match ID
		err := nk.StreamUserUpdate(StreamModeEvr, p.GetUserId(), StreamContextMatch.String(), "", p.GetUserId(), p.GetSessionId(), false, false, "")
		if err != nil {
			logger.Warn("Failed to update user status for %v: %v", p, err)
		}
	}
	// Delete the each user from the match.
	if len(rejects) > 0 {

		go func(rejects []string) {
			// Inform players (if they are still in the match) that the broadcaster has disconnected.
			uuids := lo.Map(rejects, func(s string, _ int) uuid.UUID { return uuid.FromStringOrNil(s) })
			messages := []evr.Message{
				evr.NewBroadcasterPlayersRejected(evr.PlayerRejectionReasonDisconnected, uuids...),
			}
			err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.broadcaster}, nil)
			if err != nil {
				logger.Error("failed to dispatch broadcaster disconnected message: %v", err)
			}
		}(rejects)
	}
	state.rebuildCache()
	// Update the label that includes the new player list.
	err := m.updateLabel(dispatcher, state)
	if err != nil {
		logger.Error("failed to update label: %v", err)
	removals := make([]uuid.UUID, 0, len(presences))
	for scopedID, p := range state.Presences {
		for _, pp := range presences {
			if p.GetSessionId() == pp.GetSessionId() {
				removals = append(removals, scopedID)
				break
			}
		}
	}

	messages := []evr.Message{
		evr.NewBroadcasterPlayersRejected(evr.PlayerRejectionReasonDisconnected, removals...),
	}
	err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.Broadcaster}, nil)
	if err != nil {
		logger.Error("failed to dispatch broadcaster disconnected message: %v", err)
		return nil
	}

	return state
}

// MatchLoop is called every tick of the match and handles state, plus messages from the client.
func (m *EVRMatchHandler) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, messages []runtime.MatchData) any {
	state, ok := state_.(*matchState)
	if !ok {
		return nil
	}

	select {
	case <-state.Broadcaster.session.Context().Done():
		logger.Error("Broadcaster disconnected. Shutting down.")
		return nil
	default:
	}

	// Keep track of how many ticks the match has been empty.
	if len(state.Presences) == 0 && state.Started() {
		state.emptyTicks++
	} else {
		state.emptyTicks = 0
	}

	if state.emptyTicks/state.tickRate > 15 {
		logger.Error("Match is empty for %d seconds. Shutting down.", state.emptyTicks/state.tickRate)
		return nil
	}

	// Handle the messages, one by one
	var start time.Time

	for _, in := range messages {
		switch in.GetOpCode() {
		default:
			start = time.Now()

			typ, err := m.codec.FromSymbol(evr.Symbol(in.GetOpCode()))
			if err != nil {
				logger.Warn("Unknown opcode: %v", in.GetOpCode())
				continue
			}
			// Unmarshal the message into an interface, then switch on the type.
			msg := reflect.New(reflect.TypeOf(typ).Elem()).Interface().(evr.Message)
			if err := json.Unmarshal(in.GetData(), &msg); err != nil {
				logger.Error("Failed to unmarshal message: %v", err)
			}

			var messageFn func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error)

			// Switch on the message type. This is where the match logic is handled.
			switch msg := msg.(type) {
			// TODO consider using a go routine for any/all of these that do not modify state.
			// FIXME modify the state only here in the main loop, do not pass it to the functions.
			case *evr.LobbyPlayerSessionsRequest:
				// The client is requesting player sessions.
				// TODO consider pushing this into a go routine.
				messageFn = m.lobbyPlayerSessionsRequest
			case *evr.BroadcasterPlayersAccept:
				// The client has connected to the broadcaster, and the broadcaster has accepted the connection.
				messageFn = m.broadcasterPlayersAccept
			case *evr.BroadcasterPlayerRemoved:
				// The client has disconnected from the broadcaster.
				messageFn = m.broadcasterPlayerRemoved
			case *evr.BroadcasterPlayerSessionsLocked:
				// The server has locked the player sessions.
				messageFn = m.broadcasterPlayerSessionsLocked
			case *evr.BroadcasterPlayerSessionsUnlocked:
				// The server has locked the player sessions.
				messageFn = m.broadcasterPlayerSessionsUnlocked
			case *evr.BroadcasterSessionStarted:
				// The broadcaster has started the session.
				messageFn = m.broadcasterSessionStarted
			case *evr.BroadcasterSessionEnded:
				// The broadcaster has ended the session.
				return m.MatchTerminate(ctx, logger, db, nk, dispatcher, tick, state, 0)

			default:
				logger.Warn("Unknown message type: %T", msg)
			}

			// Execute the message function
			if messageFn != nil {
				logger = logger.WithFields(map[string]any{
					"function": reflect.TypeOf(messageFn).Name(),
				})
				state, err = messageFn(ctx, logger, db, nk, dispatcher, state, in, msg)
				if err != nil {
					logger.Error("match pipeline: %v", err)
				}
			}
			tags := map[string]string{
				"type": evr.SymbolOf(msg).String(),
			}

			nk.MetricsTimerRecord("match_message_processing_duration", tags, time.Since(start))
		}
	}
	return state
}

// MatchTerminate is called when the match is being terminated.
func (m *EVRMatchHandler) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, graceSeconds int) any {
	state, ok := state_.(*matchState)
	if !ok {
		return nil
	}
	logger.Info("MatchTerminate called. %v", state)
	// Tell the broadcaster to send all the players back to the lobby.
	messages := []evr.Message{
		evr.NewBroadcasterPlayersRejected(evr.PlayerRejectionReasonLobbyEnding, lo.Keys(state.Presences)...),
	}

	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.Broadcaster}, nil); err != nil {
		logger.Error("failed to dispatch broadcaster disconnected message: %v", err)
	}

	return nil
}

const (
	SignalCodePrepareSession = iota
	SignalCodeGetEndpoint
	SignalCodeGetPresences
	SignalCodePruneUnderutilized
	SignalCodeTerminateMatch
)
const (
	OpCodeBroadcasterDisconnected int64 = iota
	OpCodeEvrPacketData
)

type EvrMatchSignal interface {
	GetOpCode() int64
}

type SignalData struct {
	OpCode  int64
	Payload []byte
}

func NewSignalData(signal EvrMatchSignal) SignalData {
	payload, err := json.Marshal(signal)
	if err != nil {
		return SignalData{}
	}

	return SignalData{
		OpCode:  signal.GetOpCode(),
		Payload: payload,
	}
}

func (s SignalData) String() string {
	data, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s SignalData) GetOpCode() int64 {
	return s.OpCode
}

type SignalPrepareSession struct {
	State matchState
	Start bool
}

func (s SignalPrepareSession) GetOpCode() int64 {
	return SignalCodePrepareSession
}

type SignalTerminateMatch struct{}

func (s SignalTerminateMatch) GetOpCode() int64 {
	return SignalCodeTerminateMatch
}

// MatchSignal is called when a signal is sent into the match.
func (m *EVRMatchHandler) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state_ any, data string) (any, string) {
	state, ok := state_.(*matchState)
	if !ok {
		return nil, "state not a valid lobby state object"
	}

	signal := SignalData{}
	err := json.Unmarshal([]byte(data), &signal)
	if err != nil {
		return state, fmt.Sprintf("failed to unmarshal signal: %v", err)
	}

	switch signal.OpCode {
	case SignalCodeTerminateMatch:
		m.MatchTerminate(ctx, logger, db, nk, dispatcher, tick, state, 0)
		return nil, "terminating match"

=======
		m.MatchTerminate(ctx, logger, db, nk, dispatcher, tick, state, 0)
		return nil, "terminating match"

>>>>>>> 88007d03 (Use custom GUID type for EVR IDs)
	default:
		logger.Warn("Unknown signal: %s", signal)
		return state, "unknown signal"
	}
}

// StartSession is called when the match is ready to start. It instructs the broadcaster to start the session.
func (m *EVRMatchHandler) StartSession(ctx context.Context, logger runtime.Logger, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState) error {

	messages := []evr.Message{
		evr.NewBroadcasterStartSession(state.ID.uuid, state.Metadata.Channel, state.Settings.ParticipantLimit, state.Settings.LobbyType, state.Broadcaster.AppID, state.Settings.Mode, state.Settings.Level, []evr.EvrId{}),
	}

	// Dispatch the message for delivery.
	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.Broadcaster}, nil); err != nil {
		return fmt.Errorf("failed to dispatch message: %v", err)
	}

	return nil
}

// SignalMatch is a helper function to send a signal to a match.
func SignalMatch(ctx context.Context, matchRegistry MatchRegistry, matchId string, data EvrMatchSignal) (string, error) {
	return matchRegistry.Signal(ctx, matchId, NewSignalData(data).String())
}

func (m *EVRMatchHandler) dispatchMessages(ctx context.Context, logger runtime.Logger, dispatcher runtime.MatchDispatcher, messages []evr.Message, presences []runtime.Presence, sender runtime.Presence) error {

	payload, err := m.codec.Marshal(messages...)
	if err != nil {
		return fmt.Errorf("could not marshal message: %v", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled: %v", ctx.Err())
	default:
	}

	return dispatcher.BroadcastMessage(OpCodeEvrPacketData, payload, presences, sender, true)
}

// lobbyPlayerSessionsRequest is called when a client requests the player sessions for a list of EchoVR IDs.
func (m *EVRMatchHandler) lobbyPlayerSessionsRequest(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	message := msg.(*evr.LobbyPlayerSessionsRequest)

	logger = logger.WithFields(map[string]any{
		"evr_id":    message.GetEvrID().String(),
		"login_sid": message.LoginSessionID.String(),
	})

	if message.MatchID != state.ID.uuid {
		logger.Warn("match ID mismatch: %v != %v", message.MatchID, state.ID.uuid)
	}

	// Get the playerSession of the sender
	var playerSessionID uuid.UUID
	var presence *PlayerPresence

	for playerSessionID, presence = range state.Presences {
		if presence.GetSessionId() == in.GetSessionId() {
			break
		}
	}

	if presence == nil || playerSessionID == uuid.Nil {
		logger.Warn("player session not found in match")
	}

	logger = logger.WithFields(map[string]any{
		"player_sid": presence.GetSessionId(),
		"player_uid": presence.GetUserId(),
		"player_evr": presence.GetEvrId(),
	})

	playerSessions := make([]uuid.UUID, len(message.PlayerEvrIDs))
	for _, e := range message.PlayerEvrIDs {
		for k, p := range state.Presences {
			if p.EvrID == e {
				playerSessions = append(playerSessions, k)
				break
			}
		}
	}

	if len(playerSessions) == 0 {
		logger.Warn("no player sessions found")
	}

	success := evr.NewLobbyPlayerSessionsSuccess(message.GetEvrID(), state.ID.uuid, playerSessionID, playerSessions, presence.Alignment)
	messages := []evr.Message{
		success.VersionU(),
		success.Version2(),
		success.Version3(),
	}
	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{in}, nil); err != nil {
		logger.Error("lobbyPlayerSessionsRequest: failed to dispatch message: %v", err)
	}

	return state, nil
}

// broadcasterPlayersAccept is called when the broadcaster has accepted or rejected player sessions.
func (m *EVRMatchHandler) broadcasterPlayersAccept(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	message := msg.(*evr.BroadcasterPlayersAccept)
	var err error
	// validate the player sessions.
	logger = logger.WithFields(map[string]any{
		"player_scoped_ids": message.PlayerSessions,
		"function":          "broadcasterPlayersAccept",
	})

	accepted := make([]uuid.UUID, 0)
	rejected := make([]uuid.UUID, 0)

	for _, s := range message.PlayerSessions {
		p, ok := state.Presences[s]
		if !ok {
			logger.WithField("player_scoped_id", s).Warn("player session not found in match")
			rejected = append(rejected, s)
			continue
		}
		logger = logger.WithFields(map[string]any{
			"player_sid": p.GetSessionId(),
			"player_uid": p.GetUserId(),
			"player_evr": p.GetEvrId(),
		})

		accepted = append(accepted, s)

		// Trigger the MatchJoin event.
		nk.StreamUserJoin(StreamModeMatchAuthoritative, state.ID.String(), "", state.Node(), p.GetUserId(), p.GetSessionId(), false, false, "")
		// Update the player's status to remove the match ID
		if err = nk.StreamUserUpdate(StreamModeEvr, p.GetUserId(), StreamContextMatch.String(), "", p.GetUserId(), p.GetSessionId(), false, false, state.ID.String()); err != nil {
			logger.Warn("Failed to update user status for %v: %v", p, err)
		}
	}

	// Only include the message if there are players to accept or reject.
	messages := make([]evr.Message, len(accepted)+len(rejected))
	if len(accepted) > 0 {
		messages = append(messages, evr.NewBroadcasterPlayersAccepted(accepted...))
	}

	if len(rejected) > 0 {
		messages = append(messages, evr.NewBroadcasterPlayersRejected(evr.PlayerRejectionReasonBadRequest, rejected...))
	}

	// Dispatch the message for delivery.
	if err := m.dispatchMessages(ctx, logger, dispatcher, messages, []runtime.Presence{state.Broadcaster}, nil); err != nil {
		return nil, fmt.Errorf("failed to dispatch message: %v", err)
	}

	return state, nil
}

// broadcasterPlayerRemoved is called when a player has been removed from the match.
func (m *EVRMatchHandler) broadcasterPlayerRemoved(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	message := msg.(*evr.BroadcasterPlayerRemoved)
	var err error

	logger = logger.WithFields(map[string]any{
		"playerSession": message.PlayerSession,
		"function":      "broadcasterPlayerRemoved",
	})

	presence, ok := state.Presences[message.PlayerSession]
	if !ok {
		logger.Warn("player not found in match")
		return state, nil
	}

	// Update the player's status to remove the match ID
	if err = nk.StreamUserUpdate(StreamModeEvr, presence.GetUserId(), StreamContextMatch.String(), "", presence.GetUserId(), presence.GetSessionId(), false, false, ""); err != nil {
		logger.Warn("Failed to update user status for %v: %v", presence, err)
	}

	// Delete the presence
	delete(state.Presences, message.PlayerSession)
	nk.StreamUserKick(StreamModeMatchAuthoritative, state.ID.String(), "", state.Node(), presence)

	// Update the label
	return state, dispatcher.MatchLabelUpdate(state.GetLabel())
}

func (m *EVRMatchHandler) broadcasterPlayerSessionsLocked(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	// Verify that the update is coming from the broadcaster.
	state.Locked = false
	return state, nil
}

func (m *EVRMatchHandler) broadcasterPlayerSessionsUnlocked(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	// Verify that the update is coming from the broadcaster.
	state.Locked = true
	return state, nil
}

func (m *EVRMatchHandler) broadcasterSessionStarted(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *matchState, in runtime.MatchData, msg evr.Message) (*matchState, error) {
	logger = logger.WithFields(map[string]any{
		"function":  "broadcasterSessionStarted",
		"sessionID": in.GetSessionId(),
	})
	// Verify that the update is coming from the broadcaster.
	if in.GetSessionId() != state.Broadcaster.GetSessionId() {
		logger.Error("broadcasterSessionStarted: invalid session ID")
		return state, nil
	}

	// Send the player start message to all the players.
	for _, p := range state.Presences {
		if err := m.sendPlayerStart(ctx, logger, dispatcher, state, p); err != nil {
			return state, fmt.Errorf("failed to send player start: %v", err)
		}
	}

	return state, nil
}
