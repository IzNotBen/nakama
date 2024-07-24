package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type UpdateClientProfile struct {
	Session       uuid.UUID
	EvrID         EvrID
	ClientProfile ClientProfile
}

func (m *UpdateClientProfile) String() string {
	return fmt.Sprintf("%T(session=%s, evr_id=%s)", m, m.Session.String(), m.EvrID.String())
}

func (m *UpdateClientProfile) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Session) },
		func() error { return s.Stream(&m.EvrID) },
		func() error { return s.StreamJSON(&m.ClientProfile, true, NoCompression) },
	})
}
