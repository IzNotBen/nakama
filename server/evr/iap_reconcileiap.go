package evr

import (
	"fmt"
)

// IAPReconcile represents an in-app purchase related request.
type IAPReconcile struct {
	Message
	Session GUID
	EvrID   EvrId
}

func (m IAPReconcile) Token() string {
	return "SNSReconcileIAP"
}

func (m IAPReconcile) Symbol() Symbol {
	return ToSymbol(m.Token())
}

func (m IAPReconcile) String() string {
	return fmt.Sprintf("%s()", m.Token())
}

func NewIAPReconcile(userID EvrId, session GUID) *IAPReconcile {
	return &IAPReconcile{
		EvrID:   userID,
		Session: session,
	}
}

func (r *IAPReconcile) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(r.Session) },
		func() error { return s.StreamStruct(&r.EvrID) },
	})
}

func (r *IAPReconcile) ToString() string {
	return fmt.Sprintf("%s(user_id=%s, session=%v)", r.Token(), r.EvrID.Token(), r.Session)
}

func (m *IAPReconcile) GetSessionID() GUID {
	return m.Session
}

func (m *IAPReconcile) GetEvrID() EvrId {
	return m.EvrID
}
