package evr

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/gofrs/uuid/v5"
)

type BroadcasterStartSession struct {
	MatchID     uuid.UUID           // The identifier for the game server session to start.
	Channel     uuid.UUID           // TODO: Unverified, suspected to be channel UUID.
	PlayerLimit byte                // The maximum amount of players allowed to join the lobby.
	LobbyType   byte                // The type of lobby
	Settings    SessionSettings     // The JSON settings associated with the session.
	Entrants    []EntrantDescriptor // Information regarding entrants (e.g. including offline/local player ids, or AI bot platform ids).
}

func (s *BroadcasterStartSession) String() string {
	return fmt.Sprintf("BroadcasterStartSession(session_id=%s, player_limit=%d, lobby_type=%d, settings=%s, entrant_descriptors=%v)",
		s.MatchID, s.PlayerLimit, s.LobbyType, s.Settings.String(), s.Entrants)
}

func NewBroadcasterStartSession(sessionID uuid.UUID, channel uuid.UUID, playerLimit uint8, lobbyType uint8, appID string, mode Symbol, level Symbol, features []string, entrants []EvrID) *BroadcasterStartSession {
	descriptors := make([]EntrantDescriptor, len(entrants))
	for i, entrant := range entrants {
		descriptors[i] = *NewEntrantDescriptor(entrant)
	}

	return &BroadcasterStartSession{
		MatchID:     sessionID,
		Channel:     channel,
		PlayerLimit: byte(playerLimit),
		LobbyType:   byte(lobbyType),
		Settings:    NewSessionSettings(appID, mode, level, features),
		Entrants:    descriptors,
	}
}

type SessionSettings struct {
	AppID    string   `json:"appid"`
	Mode     int64    `json:"gametype"`
	Level    *int64   `json:"level"`
	Features []string `json:"features,omitempty"`
}

func NewSessionSettings(appID string, mode Symbol, level Symbol, features []string) SessionSettings {

	settings := SessionSettings{
		AppID:    appID,
		Mode:     int64(mode),
		Level:    nil,
		Features: features,
	}
	if level != 0 {
		l := int64(level)
		settings.Level = &l
	}
	return settings
}

func (s *SessionSettings) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type EntrantDescriptor struct {
	Unk0     uuid.UUID
	PlayerId EvrID
	Flags    uint64
}

func (m *EntrantDescriptor) String() string {
	return fmt.Sprintf("EREntrantDescriptor(unk0=%s, player_id=%s, flags=%d)", m.Unk0, m.PlayerId.String(), m.Flags)
}

func NewEntrantDescriptor(playerId EvrID) *EntrantDescriptor {
	return &EntrantDescriptor{
		Unk0:     uuid.Must(uuid.NewV4()),
		PlayerId: playerId,
		Flags:    0x0044BB8000,
	}
}

func RandomBotEntrantDescriptor() EntrantDescriptor {
	botuuid, _ := uuid.NewV4()
	return EntrantDescriptor{
		Unk0:     botuuid,
		PlayerId: EvrID{PlatformCode: BOT, AccountID: rand.Uint64()},
		Flags:    0x0044BB8000,
	}
}

func (m *BroadcasterStartSession) Stream(s *Stream) error {
	finalStructCount := byte(len(m.Entrants))
	pad1 := byte(0)
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.MatchID) },
		func() error { return s.Stream(&m.Channel) },
		func() error { return s.StreamByte(&m.PlayerLimit) },
		func() error { return s.Stream(&finalStructCount) },
		func() error { return s.StreamByte(&m.LobbyType) },
		func() error { return s.StreamByte(&pad1) },
		func() error { return s.StreamJSON(&m.Settings, true, NoCompression) },
		func() error {
			if s.r != nil {
				m.Entrants = make([]EntrantDescriptor, finalStructCount)
			}
			for _, entrant := range m.Entrants {
				err := RunErrorFunctions([]func() error{
					func() error { return s.Stream(&entrant.Unk0) },
					func() error { return s.Stream(&entrant.PlayerId) },
					func() error { return s.Stream(&entrant.Flags) },
				})
				if err != nil {
					return err
				}
			}
			return nil
		},
	})

}

type GameServerSessionLocked struct{}

func (m *GameServerSessionLocked) Stream(s *Stream) error { return s.Skip(1) }
func (m *GameServerSessionLocked) String() string         { return fmt.Sprintf("%T()", m) }

type GameServerSessionUnlocked struct{}

func (m *GameServerSessionUnlocked) String() string { return fmt.Sprintf("%T()", m) }

func (m *GameServerSessionUnlocked) Stream(s *Stream) error { return s.Skip(1) }

type GameServerSessionStarted struct {
	LobbySessionID uuid.UUID
}

// Stream streams the message data in/out based on the streaming mode set.
func (m *GameServerSessionStarted) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.LobbySessionID) },
	})
}

func (m *GameServerSessionStarted) String() string {
	return fmt.Sprintf("%T()", m)
}
