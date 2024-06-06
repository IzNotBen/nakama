package evr

import (
	"fmt"
	"net"
)

// BroadcasterRegistrationSuccess is a message from server to game server, indicating a game server registration request had succeeded.
type BroadcasterRegistrationSuccess struct {
	ServerID        uint64
	ExternalAddress net.IP
	Unk0            uint64
}

func (m *BroadcasterRegistrationSuccess) String() string {
	return fmt.Sprintf("%T(server_id=%d, external_ip=%s)", m, m.ServerID, m.ExternalAddress)
}

func NewBroadcasterRegistrationSuccess(serverID uint64, extAddr net.IP) *BroadcasterRegistrationSuccess {
	return &BroadcasterRegistrationSuccess{
		ServerID:        serverID,
		ExternalAddress: extAddr,
		Unk0:            0,
	}
}

func (m *BroadcasterRegistrationSuccess) Stream(s *Stream) error {
	return s.Stream([]Streamer{
		&m.ServerID,
		&m.ExternalAddress,
		&m.Unk0,
	})
}
