package evr

import (
	"fmt"
	"net"
)

// BroadcasterRegistrationRequest is a message from game server to server, requesting game server registration so clients can match with it.
// NOTE: This is an unofficial message created for Echo Relay.
type BroadcasterRegistrationRequest struct {
	ServerId    uint64
	InternalIP  net.IP
	Port        uint16
	Region      Symbol
	VersionLock uint64
	TickRate    uint32 // V2
}

func NewBroadcasterRegistrationRequest(serverId uint64, internalAddress net.IP, port uint16, regionSymbol Symbol, versionLock uint64, tickRate uint32) *BroadcasterRegistrationRequest {
	return &BroadcasterRegistrationRequest{
		ServerId:    serverId,
		InternalIP:  internalAddress,
		Port:        port,
		Region:      regionSymbol,
		VersionLock: versionLock,
		TickRate:    tickRate,
	}
}

func (m BroadcasterRegistrationRequest) String() string {
	return fmt.Sprintf("%T(server_id=%d, internal_ip=%s, port=%d, region=%d, version_lock=%d, tick_rate=%d)",
		m,
		m.ServerId,
		m.InternalIP,
		m.Port,
		m.Region,
		m.VersionLock,
		m.TickRate,
	)
}

type BroadcasterRegistrationRequestV1 struct {
	BroadcasterRegistrationRequest
}

type BroadcasterRegistrationRequestV2 struct {
	BroadcasterRegistrationRequest
}

func (m BroadcasterRegistrationRequest) Version1() *BroadcasterRegistrationRequestV1 {
	return &BroadcasterRegistrationRequestV1{
		BroadcasterRegistrationRequest: m,
	}
}

func (m BroadcasterRegistrationRequest) Version2() *BroadcasterRegistrationRequestV2 {
	return &BroadcasterRegistrationRequestV2{
		BroadcasterRegistrationRequest: m,
	}
}

func (m *BroadcasterRegistrationRequestV1) Stream(s *Stream) error {
	skipped := make([]byte, 10)
	return RunErrorFunctions([]func() error{
		func() error {
			return s.Stream([]Streamer{
				&m.ServerId,
				&m.InternalIP,
				&m.Port,
				&skipped,
				&m.Region,
				&m.VersionLock,
			})
		},
	})
}

func (m *BroadcasterRegistrationRequestV2) Stream(s *Stream) error {
	skipped := make([]byte, 10)
	return s.Stream([]Streamer{
		&m.ServerId,
		&m.InternalIP,
		&m.Port,
		&skipped,
		&m.Region,
		&m.VersionLock,
		&m.TickRate,
	})
}
