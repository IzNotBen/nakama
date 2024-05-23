package evr

import (
	"encoding/binary"
	"fmt"
	"math/rand"

	"github.com/gofrs/uuid/v5"
)

type BroadcasterStartSession struct {
	MatchID     GUID                // The identifier for the game server session to start.
	Channel     GUID                // TODO: Unverified, suspected to be channel UUID.
	PlayerLimit byte                // The maximum amount of players allowed to join the lobby.
	LobbyType   byte                // The type of lobby
	Settings    SessionSettings     // The JSON settings associated with the session.
	Entrants    []EntrantDescriptor // Information regarding entrants (e.g. including offline/local player ids, or AI bot platform ids).
}

func (s *BroadcasterStartSession) String() string {
	return fmt.Sprintf("BroadcasterStartSession(session_id=%s, player_limit=%d, lobby_type=%d, settings=%s, entrant_descriptors=%v)",
		s.MatchID, s.PlayerLimit, s.LobbyType, s.Settings.String(), s.Entrants)
}

func NewBroadcasterStartSession(sessionID GUID, channel GUID, playerLimit int, lobbyType LobbyType, appID int64, mode Symbol, level Symbol, entrants []EvrId) *BroadcasterStartSession {
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

type EntrantDescriptor struct {
	PlayerSessionID GUID
	PlayerId        EvrId
	Flags           uint64
}

func (m *EntrantDescriptor) String() string {
	return fmt.Sprintf("EREntrantDescriptor(unk0=%s, player_id=%s, flags=%d)", m.PlayerSessionID, m.PlayerId.String(), m.Flags)
}

func NewEntrantDescriptor(playerId EvrId) *EntrantDescriptor {
	return &EntrantDescriptor{
		PlayerSessionID: GUID(uuid.Must(uuid.NewV4())),
		PlayerId:        playerId,
		Flags:           0x0044BB8000,
	}
}

func RandomBotEntrantDescriptor() EntrantDescriptor {
	botuuid := GUID(uuid.Must(uuid.NewV4()))
	return EntrantDescriptor{
		PlayerSessionID: botuuid,
		PlayerId:        EvrId{PlatformCode: BOT, AccountId: rand.Uint64()},
		Flags:           0x0044BB8000,
	}
}

func (m *BroadcasterStartSession) Stream(s *EasyStream) error {
	finalStructCount := byte(len(m.Entrants))
	pad1 := byte(0)
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(m.MatchID) },
		func() error { return s.StreamGuid(m.Channel) },
		func() error { return s.StreamByte(&m.PlayerLimit) },
		func() error { return s.StreamNumber(binary.LittleEndian, &finalStructCount) },
		func() error { return s.StreamByte(&m.LobbyType) },
		func() error { return s.StreamByte(&pad1) },
		func() error { return s.StreamJson(&m.Settings, true, NoCompression) },
		func() error {
			if s.Mode == DecodeMode {
				m.Entrants = make([]EntrantDescriptor, finalStructCount)
			}
			for _, entrant := range m.Entrants {
				err := RunErrorFunctions([]func() error{
					func() error { return s.StreamGuid(entrant.PlayerSessionID) },
					func() error { return s.StreamNumber(binary.LittleEndian, &entrant.PlayerId.PlatformCode) },
					func() error { return s.StreamNumber(binary.LittleEndian, &entrant.PlayerId.AccountId) },
					func() error { return s.StreamNumber(binary.LittleEndian, &entrant.Flags) },
				})
				if err != nil {
					return err
				}
			}
			return nil
		},
	})

}
