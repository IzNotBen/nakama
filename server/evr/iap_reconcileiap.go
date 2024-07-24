package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

// ReconcileIAP represents an in-app purchase related request.
type ReconcileIAP struct {
	Message
	Session uuid.UUID
	EvrID   EvrID
}

func (m ReconcileIAP) Token() string {
	return "SNSReconcileIAP"
}

func (m ReconcileIAP) Symbol() Symbol {
	return ToSymbol(m.Token())
}

func (m ReconcileIAP) String() string {
	return fmt.Sprintf("%s()", m.Token())
}

func NewReconcileIAP(userID EvrID, session uuid.UUID) *ReconcileIAP {
	return &ReconcileIAP{
		EvrID:   userID,
		Session: session,
	}
}

func (r *ReconcileIAP) Stream(s *EasyStream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamGuid(&r.Session) },
		func() error { return s.StreamStruct(&r.EvrID) },
	})
}

func (r *ReconcileIAP) ToString() string {
	return fmt.Sprintf("%s(user_id=%s, session=%v)", r.Token(), r.EvrID.String(), r.Session)
}

func (m *ReconcileIAP) GetSessionID() uuid.UUID {
	return m.Session
}

func (m *ReconcileIAP) GetEvrID() EvrID {
	return m.EvrID
}
