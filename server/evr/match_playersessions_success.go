package evr

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type LobbyPlayerSessionsSuccess struct {
	lobbyID    GUID   // Unk1, The matching-related session token for the current matchmaker operation.
	entrantIDs []GUID // Unk1, The player session token obtained for the requested player user identifier.

	Unk0          byte   // V2, V3
	EvrID         EvrId  // V2, V3
	PlayerSession GUID   // V2, V3
	Role          int16  // V3
	Unk1          uint16 // V3
	Unk2          uint32 // V3
}

func NewLobbyPlayerSessionsSuccess(evrID EvrId, lobbyID GUID, entrantID GUID, entrantIDs []GUID, Role Role) *LobbyPlayerSessionsSuccess {
	return &LobbyPlayerSessionsSuccess{
		lobbyID:       lobbyID,
		entrantIDs:    entrantIDs,
		Unk0:          0xFF,
		EvrID:         evrID,
		PlayerSession: entrantID,
		Role:          int16(Role),
		Unk1:          0,
		Unk2:          0,
	}
}

func (m LobbyPlayerSessionsSuccess) VersionU() *LobbyPlayerSessionsSuccessUnk1 {
	s := LobbyPlayerSessionsSuccessUnk1(m)
	return &s
}

func (m LobbyPlayerSessionsSuccess) Version2() *LobbyPlayerSessionsSuccessv2 {
	s := LobbyPlayerSessionsSuccessv2(m)
	return &s
}

func (m LobbyPlayerSessionsSuccess) Version3() *LobbyPlayerSessionsSuccessv3 {
	s := LobbyPlayerSessionsSuccessv3(m)
	return &s
}

type LobbyPlayerSessionsSuccessUnk1 LobbyPlayerSessionsSuccess

func (m *LobbyPlayerSessionsSuccessUnk1) Token() string {
	return "ERLobbyPlayerSessionsSuccessUnk1"
}

func (m *LobbyPlayerSessionsSuccessUnk1) Symbol() Symbol {
	return SymbolOf(m)
}

func (m *LobbyPlayerSessionsSuccessUnk1) Stream(s *Stream) error {
	count := uint64(len(m.entrantIDs))

	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &count) },
		func() error { return s.StreamGUID(&m.lobbyID) },
		func() error {
			if s.Mode == DecodeMode {
				m.entrantIDs = make([]GUID, s.Len()/16)
			}
			return s.StreamGUIDs(m.entrantIDs)
		},
	})
}

func (m *LobbyPlayerSessionsSuccessUnk1) String() string {
	sessions := make([]string, len(m.entrantIDs))
	for i, session := range m.entrantIDs {
		sessions[i] = session.String()
	}
	return fmt.Sprintf("%s(matching_session=%s, player_sessions=[%s])", m.Token(), m.lobbyID, strings.Join(sessions, ", "))
}

type LobbyPlayerSessionsSuccessv3 LobbyPlayerSessionsSuccess

func (m LobbyPlayerSessionsSuccessv3) Token() string {
	return "SNSLobbyPlayerSessionsSuccessv3"
}

func (m *LobbyPlayerSessionsSuccessv3) Symbol() Symbol {
	return SymbolOf(m)
}

func (m *LobbyPlayerSessionsSuccessv3) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk0) },
		func() error { return s.StreamStruct(&m.EvrID) },
		func() error { return s.StreamGUID(&m.PlayerSession) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Role) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk1) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk2) },
	})

}

func (m *LobbyPlayerSessionsSuccessv3) String() string {
	return fmt.Sprintf("%s(unk0=%d, user_id=%v, player_session=%s, team_index=%d, unk1=%d, unk2=%d)",
		m.Token(), m.Unk0, m.EvrID, m.PlayerSession, m.Role, m.Unk1, m.Unk2)
}

type LobbyPlayerSessionsSuccessv2 LobbyPlayerSessionsSuccess

func (m LobbyPlayerSessionsSuccessv2) Token() string {
	return "SNSLobbyPlayerSessionsSuccessv2"
}

func (m *LobbyPlayerSessionsSuccessv2) Symbol() Symbol {
	return SymbolOf(m)
}

// Stream streams the message data in/out based on the streaming mode set.
func (m *LobbyPlayerSessionsSuccessv2) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &m.Unk0) },
		func() error { return s.StreamStruct(&m.EvrID) },
		func() error { return s.StreamGUID(&m.PlayerSession) },
	})
}

// String returns a string representation of the LobbyPlayerSessionsSuccessv2 message.
func (m LobbyPlayerSessionsSuccessv2) String() string {
	return fmt.Sprintf("%s(unk0=%d, user_id=%v, player_session=%s)", m.Token(), m.Unk0, m.EvrID, m.PlayerSession)
}
