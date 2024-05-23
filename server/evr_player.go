package server

import (
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
)

type (
	CombatWeapon  string
	CombatGrenade string
	DominateHand  uint8
	CombatAbility string
)

const (
	AssaultRifle CombatWeapon = "assault"
	Blaster      CombatWeapon = "blaster"
	Rocket       CombatWeapon = "rocket"
	ScoutRifle   CombatWeapon = "scout"
	Magnum       CombatWeapon = "magnum"
	SMG          CombatWeapon = "smg"
	ChainRifle   CombatWeapon = "chain"

	ArcGrenade   CombatGrenade = "arc"
	BurstGrenade CombatGrenade = "burst"
	DetGrenade   CombatGrenade = "det"
	StunGrenade  CombatGrenade = "stun"
	LocGrenade   CombatGrenade = "loc"

	LeftHand  DominateHand = 0
	RightHand DominateHand = 1

	BuffAbility   CombatAbility = "buff"
	HealAbility   CombatAbility = "heal"
	SensorAbility CombatAbility = "sensor"
	ShieldAbility CombatAbility = "shield"
	WraithAbility CombatAbility = "wraith"
)

type AccountMetadata struct {
	TeamName           string      `json:"teamname,omitempty" validate:"omitempty,ascii"`
	CombatWeapon       string      `json:"weapon" validate:"omitnil,oneof=assault blaster rocket scout magnum smg chain rifle"`
	CombatGrenade      string      `json:"grenade" validate:"omitnil,oneof=arc burst det stun loc"`
	CombatDominantHand uint8       `json:"weaponarm" validate:"eq=0|eq=1"`
	CombatAbility      string      `json:"ability" validate:"oneof=buff heal sensor shield wraith"`
	MutedPlayers       []uuid.UUID `json:"mute,omitempty"`
	GhostedPlayers     []uuid.UUID `json:"ghost,omitempty"`
	DefaultGuildID     uuid.UUID   `json:"guild_id,omitempty"`
}

func (a *AccountMetadata) IsMuted(playerID uuid.UUID) bool {
	for _, id := range a.MutedPlayers {
		if id == playerID {
			return true
		}
	}
	return false
}

func (a *AccountMetadata) IsGhosted(playerID uuid.UUID) bool {
	for _, id := range a.GhostedPlayers {
		if id == playerID {
			return true
		}
	}
	return false
}

func (a *AccountMetadata) MutePlayer(playerID uuid.UUID) {
	a.MutedPlayers = append(a.MutedPlayers, playerID)
}

func (a *AccountMetadata) UnmutePlayer(playerID uuid.UUID) {
	for i, id := range a.MutedPlayers {
		if id == playerID {
			a.MutedPlayers = append(a.MutedPlayers[:i], a.MutedPlayers[i+1:]...)
			return
		}
	}
}

func (a *AccountMetadata) UnghostPlayer(playerID uuid.UUID) {
	for i, id := range a.GhostedPlayers {
		if id == playerID {
			a.GhostedPlayers = append(a.GhostedPlayers[:i], a.GhostedPlayers[i+1:]...)
			return
		}
	}
}

func (a *AccountMetadata) GhostPlayer(playerID uuid.UUID) {
	a.GhostedPlayers = append(a.GhostedPlayers, playerID)
}

func (a *AccountMetadata) SetDefaultGuild(guildID uuid.UUID) {
	a.DefaultGuildID = guildID
}

func (a *AccountMetadata) GetDefaultGuild() uuid.UUID {
	return a.DefaultGuildID
}

func (a *AccountMetadata) SetTeamName(name string) {
	// Limit to 20 characters
	if len(name) > 20 {
		name = name[:20]
	}

	a.TeamName = name
}

func (a *AccountMetadata) ClientProfile() evr.ClientProfile {
	return evr.ClientProfile{}
}

type Social struct {
	GhostedUserIDs []uuid.UUID `json:"ghosted,omitempty"`
	MutedUserIDs   []uuid.UUID `json:"mute,omitempty"`

	NewUnlocks []evr.Symbol `json:"newunlocks,omitempty"`
}

type ProfileVersions struct {
	CommunityValuesVersion     int64  `json:"community_values_version,omitempty"`
	SetupVersion               int64  `json:"setup_version,omitempty"`
	PointsPolicyVersion        uint64 `json:"points_policy_version,omitempty"`
	EulaVersion                uint64 `json:"eula_version,omitempty"`
	GameAdminVersion           uint64 `json:"game_admin_version,omitempty"`
	SplashScreenVersion        uint64 `json:"splash_screen_version,omitempty"`
	GroupsLegalVersion         uint64 `json:"groups_legal_version,omitempty"`
	BattlePassSeasonPoiVersion uint16 `json:"battlepass_season_poi_version,omitempty"` // Battle pass season point of interest version (manually set to 3246)
	NewUnlocksPoiVersion       uint16 `json:"new_unlocks_poi_version,omitempty"`       // New unlocks point of interest version
	StoreEntryPoiVersion       uint16 `json:"store_entry_poi_version,omitempty"`       // Store entry point of interest version
	ClearNewUnlocksVersion     uint16 `json:"clear_new_unlocks_version,omitempty"`     // Clear new unlocks version
}

var _ runtime.Presence = &PlayerPresence{}

// Represents identity information for a single match participant.
type PlayerPresence struct {
	UserID       uuid.UUID    `json:"userid,omitempty"`
	SessionID    uuid.UUID    `json:"session_id,omitempty"`
	Username     string       `json:"username,omitempty"`
	DisplayName  string       `json:"display_name,omitempty"`
	ClientIP     string       `json:"client_ip,omitempty"`
	EvrID        evr.EvrId    `json:"evr_id,omitempty"`
	PartyID      uuid.UUID    `json:"party_id,omitempty"`
	DiscordID    string       `json:"discord_id,omitempty"`
	SessionFlags SessionFlags `json:"session_flags,omitempty"`
	VersionLock  evr.Symbol   `json:"version_lock,omitempty"`
	Alignment    evr.Role     `json:"alignment,omitempty"`
	memberships  Memberships  `json:"memberships,omitempty"`
	Node         string       `json:"node,omitempty"`
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
	return lo.Map(p.memberships, func(m Membership, _ int) uuid.UUID { return m.GroupID })
}

func NewPlayerPresence(session *sessionWS, alignment evr.Role) PlayerPresence {
	ctx := session.Context()

	flags := ctx.Value(ctxFlagsKey{}).(SessionFlags)
	evrID := ctx.Value(ctxEvrIDKey{}).(evr.EvrId)
	channels := ctx.Value(ctxChannelsKey{}).(Memberships)
	discordID := ctx.Value(ctxDiscordIDKey{}).(string)

	return PlayerPresence{
		UserID:       session.UserID(),
		SessionID:    session.ID(),
		Username:     session.Username(),
		DisplayName:  channels[0].DisplayName,
		ClientIP:     session.ClientIP(),
		EvrID:        evrID,
		DiscordID:    discordID,
		SessionFlags: flags,
		Alignment:    alignment,
		Node:         session.pipeline.node,
		memberships:  channels,
	}
}
