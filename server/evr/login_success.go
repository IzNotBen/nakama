package evr

import (
	"encoding/binary"
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type LoginSuccess struct {
	Session uuid.UUID
	EvrId   EvrId
}

func NewLoginSuccess(session uuid.UUID, evrId EvrId) *LoginSuccess {
	return &LoginSuccess{
		Session: session,
		EvrId:   evrId,
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
		m.Token(), m.Session, m.EvrId.String())
}

func (m *LoginSuccess) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(&m.Session) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrId.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &m.EvrId.AccountId) },
	})
}

func (m *LoginSuccess) GetSessionID() uuid.UUID {
	return m.Session
}

func (m *LoginSuccess) EvrID() EvrId {
	return m.EvrId
}
