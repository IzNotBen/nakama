package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type LoginSuccess struct {
	Session uuid.UUID
	EvrID   EvrID
}

func NewLoginSuccess(session uuid.UUID, evrID EvrID) *LoginSuccess {
	return &LoginSuccess{
		Session: session,
		EvrID:   evrID,
	}
}

func (m LoginSuccess) String() string {
	return fmt.Sprintf("%T(session=%v, user_id=%s)", m, m.Session, m.EvrID.String())
}

func (m *LoginSuccess) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&m.Session) },
		func() error { return s.Stream(&m.EvrID.PlatformCode) },
		func() error { return s.Stream(&m.EvrID.AccountID) },
	})
}

func (m *LoginSuccess) GetLoginSessionID() uuid.UUID {
	return m.Session
}

func (m *LoginSuccess) GetEvrID() EvrID {
	return m.EvrID
}
