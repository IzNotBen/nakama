package evr

import (
	"encoding/binary"
	"fmt"
)

type UpdateClientProfile struct {
	SessionID GUID
	EvrID     EvrId
	Profile   ClientProfile
}

func (m *UpdateClientProfile) Token() string {
	return "SNSUpdateProfile"
}

func (m *UpdateClientProfile) Symbol() Symbol {
	return ToSymbol(m.Token())
}

func (lr *UpdateClientProfile) String() string {
	return fmt.Sprintf("UpdateProfile(session=%s, evr_id=%s)", lr.SessionID.String(), lr.EvrID.String())
}

func (m *UpdateClientProfile) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGUID(&m.SessionID) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.AccountId) },
		func() error { return s.StreamJSON(&m.Profile, true, NoCompression) },
	})
}
