package evr

import (
	"fmt"
	"strings"
)

// Broadcaster -> Nakama: player sessions that the broadcaster intends to accept.

type BroadcasterSessionEntrants struct {
	LobbyID    GUID
	EntrantIDs []GUID
}

func (m *BroadcasterSessionEntrants) String() string {
	entrantIDs := make([]string, len(m.EntrantIDs))
	for i, s := range m.EntrantIDs {
		entrantIDs[i] = s.String()
	}
	return fmt.Sprintf("%T(entrant_ids=[%s])", m, strings.Join(entrantIDs, ", "))
}

func NewBroadcasterEntrants(entrantIDs []GUID) *BroadcasterSessionEntrants {
	return &BroadcasterSessionEntrants{
		EntrantIDs: entrantIDs,
	}
}

type BroadcasterSessionEntrantsV1 struct {
	BroadcasterSessionEntrants
}

func (m *BroadcasterSessionEntrantsV1) Stream(s *Stream) error {
	if s.Mode == DecodeMode {
		m.EntrantIDs = make([]GUID, s.Len()/16)
	}
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGUIDs(m.EntrantIDs) },
	})
}

type BroadcasterSessionEntrantsV2 struct {
	BroadcasterSessionEntrants
}

func (m *BroadcasterSessionEntrantsV2) Stream(s *Stream) error {
	entrantCount := uint64(len(m.EntrantIDs))

	return RunErrorFunctions([]func() error{
		func() error {
			return s.Stream([]Streamer{
				&m.LobbyID,
				&entrantCount,
			})
		},
		func() error {
			if s.Mode == DecodeMode {
				m.EntrantIDs = make([]GUID, entrantCount)
			}
			return s.Stream(m.EntrantIDs)
		},
	})
}
