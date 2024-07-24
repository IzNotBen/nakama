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

func NewReconcileIAP(userID EvrID, session uuid.UUID) *ReconcileIAP {
	return &ReconcileIAP{
		EvrID:   userID,
		Session: session,
	}
}

func (r *ReconcileIAP) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&r.Session) },
		func() error { return s.Stream(&r.EvrID) },
	})
}

func (r *ReconcileIAP) String() string {
	return fmt.Sprintf("%s(user_id=%s, session=%v)", r, r.EvrID.String(), r.Session)
}

func (m *ReconcileIAP) GetLoginSessionID() uuid.UUID {
	return m.Session
}

func (m *ReconcileIAP) GetEvrID() EvrID {
	return m.EvrID
}
