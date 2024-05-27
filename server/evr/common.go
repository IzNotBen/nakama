package evr

import (
	"encoding/json"
	"strconv"
	"strings"
)

// A message that can be used to validate the session.

var (
	ModeUnloaded       Symbol = ToSymbol("")                      // Unloaded Lobby
	ModeSocialPublic   Symbol = ToSymbol("social_2.0")            // Public Social Lobby
	ModeSocialPrivate  Symbol = ToSymbol("social_2.0_private")    // Private Social Lobby
	ModeSocialNPE      Symbol = ToSymbol("social_2.0_npe")        // Social Lobby NPE
	ModeArenaPublic    Symbol = ToSymbol("echo_arena")            // Public Echo Arena
	ModeArenaPrivate   Symbol = ToSymbol("echo_arena_private")    // Private Echo Arena
	ModeArenaTournment Symbol = ToSymbol("echo_arena_tournament") // Echo Arena Tournament
	ModeArenaPublicAI  Symbol = ToSymbol("echo_arena_public_ai")  // Public Echo Arena AI

	ModeEchoCombatTournament Symbol = ToSymbol("echo_combat_tournament") // Echo Combat Tournament
	ModeCombatPublic         Symbol = ToSymbol("echo_combat")            // Echo Combat
	ModeCombatPrivate        Symbol = ToSymbol("echo_combat_private")    // Private Echo Combat

	LevelUnloaded     Symbol = Symbol(0)                          // Unloaded Lobby
	LevelSocial       Symbol = ToSymbol("mpl_lobby_b2")           // Social Lobby
	LevelUnspecified  Symbol = Symbol(0xffffffffffffffff)         // Unspecified Level
	LevelArena        Symbol = ToSymbol("mpl_arena_a")            // Echo Arena
	ModeArenaTutorial Symbol = ToSymbol("mpl_tutorial_arena")     // Echo Arena Tutorial
	LevelFission      Symbol = ToSymbol("mpl_combat_fission")     // Echo Combat
	LevelCombustion   Symbol = ToSymbol("mpl_combat_combustion")  // Echo Combat
	LevelDyson        Symbol = ToSymbol("mpl_combat_dyson")       // Echo Combat
	LevelGauss        Symbol = ToSymbol("mpl_combat_gauss")       // Echo Combat
	LevelPebbles      Symbol = ToSymbol("mpl_combat_pebbles")     // Echo Combat
	LevelPtyPebbles   Symbol = ToSymbol("pty_mpl_combat_pebbles") // Echo Combat

	BuildVersions = map[string]Symbol{ // VersionLock's
		"goldmaster 631547": Symbol(0xc62f01d78f77910d),
	}

	AppIDs = map[string]string{
		"1369078409873402": "Echo VR",
	}

	ModeLevels = map[Symbol][]Symbol{
		ModeArenaPublic:          {LevelArena},
		ModeArenaPrivate:         {LevelArena},
		ModeArenaTournment:       {LevelArena},
		ModeArenaPublicAI:        {LevelArena},
		ModeArenaTutorial:        {LevelArena},
		ModeSocialPublic:         {LevelSocial},
		ModeSocialPrivate:        {LevelSocial},
		ModeSocialNPE:            {LevelSocial},
		ModeCombatPublic:         {LevelCombustion, LevelDyson, LevelFission, LevelGauss},
		ModeCombatPrivate:        {LevelCombustion, LevelDyson, LevelFission, LevelGauss},
		ModeEchoCombatTournament: {LevelCombustion, LevelDyson, LevelFission, LevelGauss},
	}
	ModeTypes = map[Symbol]LobbyType{
		ModeArenaPublic:          PublicLobby,
		ModeArenaPrivate:         PrivateLobby,
		ModeArenaTournment:       PublicLobby,
		ModeArenaPublicAI:        PublicLobby,
		ModeArenaTutorial:        PublicLobby,
		ModeSocialPublic:         PublicLobby,
		ModeSocialPrivate:        PrivateLobby,
		ModeSocialNPE:            PublicLobby,
		ModeCombatPublic:         PublicLobby,
		ModeCombatPrivate:        PrivateLobby,
		ModeEchoCombatTournament: PrivateLobby,
	}
)

type IdentifyingMessage interface {
	GetSessionID() GUID
	GetEvrID() EvrId
}

type SessionSettings struct {
	AppID int64
	Mode  Symbol
	Level Symbol
}

func NewSessionSettings(appID int64, mode Symbol, level Symbol) SessionSettings {
	return SessionSettings{
		AppID: appID,
		Mode:  mode,
		Level: level,
	}
}

func (s *SessionSettings) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (s *SessionSettings) MarshalJSON() ([]byte, error) {

	var settings struct {
		AppID string `json:"appid"`
		Mode  int64  `json:"gametype"`
		Level int64  `json:"level,omitempty"`
	}
	settings.AppID = strconv.FormatInt(s.AppID, 10)
	settings.Mode = int64(s.Mode)

	if s.Level != LevelUnspecified {
		settings.Level = int64(s.Level)
	}

	return json.Marshal(settings)
}

func (s *SessionSettings) UnmarshalJSON(data []byte) error {
	var settings struct {
		AppID string `json:"appid"`
		Mode  int64  `json:"gametype"`
		Level int64  `json:"level,omitempty"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	s.AppID, _ = strconv.ParseInt(settings.AppID, 10, 64)
	s.Mode = ToSymbol(settings.Mode)

	if settings.Level == 0 {
		s.Level = LevelUnspecified
	} else {
		s.Level = ToSymbol(settings.Level)
	}

	return nil
}

type Role int16

const (
	UnassignedRole Role = iota - 1
	BlueTeamRole
	OrangeTeamRole
	SpectatorRole
	SocialRole
	ModeratorRole
)

func (t Role) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

func (t *Role) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch strings.ToLower(s) {
	default:
		i, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*t = Role(i)
	case "any":
		*t = UnassignedRole

	case "orange":
		*t = OrangeTeamRole
	case "blue":
		*t = BlueTeamRole
	case "spectator":
		*t = SpectatorRole
	case "social":
		*t = SocialRole
	case "moderator":
		*t = ModeratorRole
	}
	return nil
}

func (t Role) String() string {

	switch t {
	default:
		return "unk"
	case UnassignedRole:
		return "any"
	case OrangeTeamRole:
		return "orange"
	case BlueTeamRole:
		return "blue"
	case SpectatorRole:
		return "spectator"
	case SocialRole:
		return "social"
	case ModeratorRole:
		return "moderator"

	}
}

func (l LobbyType) String() string {
	switch l {
	case PublicLobby:
		return "public"
	case PrivateLobby:
		return "private"
	case UnassignedLobby:
		return "unassigned"
	default:
		return "unk"
	}
}

type (
	LobbyType uint32
)

const (
	PublicLobby     LobbyType = iota // An active public lobby
	PrivateLobby                     // An active private lobby
	UnassignedLobby                  // An unloaded lobby
)

func (l LobbyType) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

func (l *LobbyType) UnmarshalText(b []byte) error {
	s := string(b)
	switch strings.ToLower(s) {
	default:
		i, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*l = LobbyType(i)
	case "public":
		*l = PublicLobby
	case "private":
		*l = PrivateLobby
	case "unassigned":
		*l = UnassignedLobby

	}
	return nil
}

/*

SNSActivityDailyListRequest
SNSActivityDailyListResponse
SNSActivityDailyRewardFailure
SNSActivityDailyRewardRequest
SNSActivityDailyRewardSuccess
SNSActivityEventListRequest
SNSActivityEventListResponse
SNSActivityEventRewardFailure
SNSActivityEventRewardRequest
SNSActivityEventRewardSuccess
SNSActivityWeeklyListRequest
SNSActivityWeeklyListResponse
SNSActivityWeeklyRewardFailure
SNSActivityWeeklyRewardRequest
SNSActivityWeeklyRewardSuccess
SNSAddTournament

SNSEarlyQuitConfig
SNSEarlyQuitFeatureFlags
SNSEarlyQuitUpdateNotification

SNSFriendAcceptFailure
SNSFriendAcceptNotify
SNSFriendAcceptRequest
SNSFriendAcceptSuccess
SNSFriendInviteFailure
SNSFriendInviteNotify
SNSFriendInviteRequest
SNSFriendInviteSuccess
SNSFriendListRefreshRequest
SNSFriendListResponse
SNSFriendListSubscribeRequest
SNSFriendListUnsubscribeRequest
SNSFriendRejectNotify
SNSFriendRemoveNotify
SNSFriendRemoveRequest
SNSFriendRemoveResponse
SNSFriendStatusNotify
SNSFriendWithdrawnNotify
SNSGenericMessage
SNSGenericMessageNotify
SNSLeaderboardRequestv2
SNSLeaderboardResponse

SNSLobbyDirectoryJson
SNSLobbyDirectoryRequestJsonv2

SNSLobbyPlayerSessionsFailurev3

SNSLoginRemovedNotify

SNSLogOut
SNSMatchEndedv5
SNSNewUnlocksNotification

SNSPartyCreateFailure
SNSPartyCreateRequest
SNSPartyCreateSuccess
SNSPartyInviteListRefreshRequest
SNSPartyInviteListResponse
SNSPartyInviteNotify
SNSPartyInviteRequest
SNSPartyInviteResponse
SNSPartyJoinFailure
SNSPartyJoinNotify
SNSPartyJoinRequest
SNSPartyJoinSuccess
SNSPartyKickFailure
SNSPartyKickNotify
SNSPartyKickRequest
SNSPartyKickSuccess
SNSPartyLeaveFailure
SNSPartyLeaveNotify
SNSPartyLeaveRequest
SNSPartyLeaveSuccess
SNSPartyLockFailure
SNSPartyLockNotify
SNSPartyLockRequest
SNSPartyLockSuccess
SNSPartyPassFailure
SNSPartyPassNotify
SNSPartyPassRequest
SNSPartyPassSuccess
SNSPartyUnlockFailure
SNSPartyUnlockNotify
SNSPartyUnlockRequest
SNSPartyUnlockSuccess
SNSPartyUpdateFailure
SNSPartyUpdateMemberFailure
SNSPartyUpdateMemberNotify
SNSPartyUpdateMemberRequest
SNSPartyUpdateMemberSuccess
SNSPartyUpdateNotify
SNSPartyUpdateRequest
SNSPartyUpdateSuccess
SNSPurchaseItems
SNSPurchaseItemsResult

SNSRemoveTournament
SNSRewardsSettings
SNSServerSettingsResponsev2
SNSTelemetryEvent
SNSTelemetryNotify

SNSUserServerProfileUpdateFailure

*/
