package evr

import (
	"encoding/binary"
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

func (m LoginSuccess) Token() string {
	return "SNSLogInSuccess"
}

func (m *LoginSuccess) Symbol() Symbol {
	return SymbolOf(m)
}

func (m LoginSuccess) String() string {
	return fmt.Sprintf("%s(session=%v, user_id=%s)",
		m.Token(), m.Session, m.EvrID.String())
}

func (m *LoginSuccess) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(&m.Session) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrID.AccountID) },
	})
}

func (m *LoginSuccess) GetSessionID() uuid.UUID {
	return m.Session
}

func (m *LoginSuccess) GetEvrID() EvrID {
	return m.EvrID
}
