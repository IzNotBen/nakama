package evr

import "fmt"

// BroadcasterSessionPlayerRemoved is a message from broadcaster to nakama,
// indicating a player was removed by the game server.
type BroadcasterSessionPlayerRemoved struct {
	LobbyID   GUID // V2
	EntrantID GUID
}

func (m *BroadcasterSessionPlayerRemoved) String() string {
	return fmt.Sprintf("BroadcasterPlayerRemoved(player_session=%s)", m.EntrantID.String())
}

func NewBroadcasterSessionPlayerRemoved(lobbyID GUID, entrantID GUID) *BroadcasterSessionPlayerRemoved {
	return &BroadcasterSessionPlayerRemoved{
		LobbyID:   lobbyID,
		EntrantID: entrantID,
	}
}

type BroadcasterSessionPlayerRemovedV1 struct {
	BroadcasterSessionPlayerRemoved
}
type BroadcasterSessionPlayerRemovedV2 struct {
	BroadcasterSessionPlayerRemoved
}

func (m *BroadcasterSessionPlayerRemovedV1) Stream(s *Stream) error {
	return s.Stream(&m.EntrantID)
}

func (m *BroadcasterSessionPlayerRemovedV2) Stream(s *Stream) error {
	return s.Stream([]Streamer{
		&m.LobbyID,
		&m.EntrantID,
	})
}
