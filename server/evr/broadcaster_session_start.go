package evr

import (
	"fmt"
	"math/rand"

	"github.com/gofrs/uuid/v5"
)

type BroadcasterStartSession struct {
	LobbyID          GUID                // The identifier for the game server session to start.
	ChannelID        GUID                // TODO: Unverified, suspected to be channel UUID.
	ParticipantLimit byte                // The maximum amount of players allowed to join the lobby.
	LobbyType        byte                // The type of lobby
	Settings         SessionSettings     // The JSON settings associated with the session.
	Entrants         []EntrantDescriptor // Information regarding entrants (e.g. including offline/local player ids, or AI bot platform ids).
}

func (s *BroadcasterStartSession) String() string {
	return fmt.Sprintf("BroadcasterStartSession(session_id=%s, player_limit=%d, lobby_type=%d, settings=%s, entrant_descriptors=%v)",
		s.LobbyID, s.ParticipantLimit, s.LobbyType, s.Settings.String(), s.Entrants)
}

func NewBroadcasterStartSession(sessionID GUID, channel GUID, playerLimit int, lobbyType LobbyType, appID int64, mode Symbol, level Symbol, entrants []EvrId) *BroadcasterStartSession {
	descriptors := make([]EntrantDescriptor, len(entrants))
	for i, entrant := range entrants {
		descriptors[i] = *NewEntrantDescriptor(entrant)
	}

	return &BroadcasterStartSession{
		LobbyID:          sessionID,
		ChannelID:        channel,
		ParticipantLimit: byte(playerLimit),
		LobbyType:        byte(lobbyType),
		Settings:         NewSessionSettings(appID, mode, level),
		Entrants:         descriptors,
	}
}

type EntrantDescriptor struct {
	entrantID GUID
	EvrID     EvrId
	Flags     uint64
}

func (m *EntrantDescriptor) String() string {
	return fmt.Sprintf("EREntrantDescriptor(unk0=%s, player_id=%s, flags=%d)", m.entrantID, m.EvrID.String(), m.Flags)
}

func (m *EntrantDescriptor) Stream(s *Stream) error {
	return s.Stream([]Streamer{
		&m.entrantID,
		&m.EvrID,
		&m.Flags,
	})
}

func NewEntrantDescriptor(playerId EvrId) *EntrantDescriptor {
	return &EntrantDescriptor{
		entrantID: GUID(uuid.Must(uuid.NewV4())),
		EvrID:     playerId,
		Flags:     0x0044BB8000,
	}
}

func RandomBotEntrantDescriptor() EntrantDescriptor {
	botuuid := GUID(uuid.Must(uuid.NewV4()))
	return EntrantDescriptor{
		entrantID: botuuid,
		EvrID:     EvrId{PlatformCode: 5, AccountId: rand.Uint64()},
		Flags:     0x0044BB8000,
	}
}

func (m *BroadcasterStartSession) Stream(s *Stream) error {
	entrantCount := byte(len(m.Entrants))
	skipped := byte(0)
	return RunErrorFunctions([]func() error{
		func() error {
			return s.Stream([]Streamer{
				&m.LobbyID,
				&m.ChannelID,
				&m.ParticipantLimit,
				&entrantCount,
				&m.LobbyType,
				&skipped,
				&m.Settings,
			})
		},
		func() error {
			if s.Mode == DecodeMode {
				m.Entrants = make([]EntrantDescriptor, entrantCount)
			}
			return s.Stream(m.Entrants)
		},
	})
}
