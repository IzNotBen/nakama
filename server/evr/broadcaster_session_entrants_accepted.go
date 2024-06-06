package evr

import (
	"fmt"
	"strings"
)

// Game Server -> Nakama: player sessions that have been accepted.
type BroadcasterEntrantsAccepted struct {
	PlayerSessions []GUID
}

func NewBroadcasterEntrantsAccepted(playerSessions ...GUID) *BroadcasterEntrantsAccepted {
	return &BroadcasterEntrantsAccepted{PlayerSessions: playerSessions}
}

func (m *BroadcasterEntrantsAccepted) Stream(s *Stream) error {
	skip := byte(0)
	if s.Mode == DecodeMode {
		m.PlayerSessions = make([]GUID, (s.Len()-1)/16)
	}
	return s.Stream([]Streamer{
		&skip,
		m.PlayerSessions,
	})
}

func (m *BroadcasterEntrantsAccepted) String() string {
	sessions := make([]string, len(m.PlayerSessions))
	for i, session := range m.PlayerSessions {
		sessions[i] = session.String()
	}
	return fmt.Sprintf("%T(player_sessions=[%s])", strings.Join(sessions, ", "))
}
