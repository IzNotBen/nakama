package evr

import (
	"fmt"
	"net"
)

const (
	BroadcasterRegistration_InvalidRequest BroadcasterRegistrationFailureCode = iota
	BroadcasterRegistration_Timeout
	BroadcasterRegistration_CryptographyError
	BroadcasterRegistration_DatabaseError
	BroadcasterRegistration_AccountDoesNotExist
	BroadcasterRegistration_ConnectionFailed
	BroadcasterRegistration_ConnectionLost
	BroadcasterRegistration_ProviderError
	BroadcasterRegistration_Restricted
	BroadcasterRegistration_Unknown
	BroadcasterRegistration_Failure
	BroadcasterRegistration_Success
)

type BroadcasterRegistrationFailureCode byte

type GameServerRegistrationRequest struct {
	ServerID    uint64
	InternalIP  net.IP
	Port        uint16
	Region      Symbol
	VersionLock uint64
}

func NewBroadcasterRegistrationRequest(serverId uint64, internalAddress net.IP, port uint16, regionSymbol Symbol, versionLock uint64) *GameServerRegistrationRequest {
	return &GameServerRegistrationRequest{
		ServerID:    serverId,
		InternalIP:  internalAddress,
		Port:        port,
		Region:      regionSymbol,
		VersionLock: versionLock,
	}
}

func (m *GameServerRegistrationRequest) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.ServerID) },
		func() error { return s.StreamIP(&m.InternalIP) },
		func() error { return s.Stream(&m.Port) },
		func() error { return s.Skip(10) },
		func() error { return s.Stream(&m.Region) },
		func() error { return s.Stream(&m.VersionLock) },
	})
}
func (m GameServerRegistrationRequest) String() string {
	return fmt.Sprintf("%T(server_id=%d, internal_ip=%s, port=%d, region=%d, version_lock=%d)", m, m.ServerID, m.InternalIP, m.Port, m.Region, m.VersionLock)
}

type BroadcasterRegistrationSuccess struct {
	ServerID   uint64
	ExternalIP net.IP
	Unk0       uint64
}

func NewBroadcasterRegistrationSuccess(serverId uint64, externalAddr net.IP) *BroadcasterRegistrationSuccess {
	return &BroadcasterRegistrationSuccess{
		ServerID:   serverId,
		ExternalIP: externalAddr,
		Unk0:       0,
	}
}

func (m *BroadcasterRegistrationSuccess) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.ServerID) },
		func() error { return s.StreamIP(&m.ExternalIP) },
		func() error { return s.Stream(&m.Unk0) },
	})
}

func (m *BroadcasterRegistrationSuccess) String() string {
	return fmt.Sprintf("%T(server_id=%d, external_ip=%s)", m, m.ServerID, m.ExternalIP)
}

type BroadcasterRegistrationFailure struct {
	Code BroadcasterRegistrationFailureCode
}

func NewBroadcasterRegistrationFailure(code BroadcasterRegistrationFailureCode) *BroadcasterRegistrationFailure {
	return &BroadcasterRegistrationFailure{
		Code: code,
	}
}

func (m BroadcasterRegistrationFailure) String() string {
	return fmt.Sprintf("%T(result=%v)", m, m.Code)
}

func (m *BroadcasterRegistrationFailure) Stream(s *Stream) error {
	b := byte(m.Code)
	return s.StreamByte(&b)
}
