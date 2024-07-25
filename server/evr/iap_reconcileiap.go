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
	return fmt.Sprintf("%T(user_id=%s, session=%v)", r, r.EvrID.String(), r.Session)
}

func (m *ReconcileIAP) GetLoginSessionID() uuid.UUID { return m.Session }

func (m *ReconcileIAP) GetEvrID() EvrID { return m.EvrID }

type ReconcileIAPResult struct {
	EvrID   EvrID
	IAPData IAPData
}

type IAPData struct {
	Balance       IAPBalance `json:"balance"`
	TransactionId int64      `json:"transactionid"`
}

type IAPBalance struct {
	Currency IAPCurrency `json:"currency"`
}

type IAPCurrency struct {
	EchoPoints IAPEchoPoints `json:"echopoints"`
}

type IAPEchoPoints struct {
	Value int64 `json:"val"`
}

// ReconcileIAPResult represents a response related to in-app purchases.

func NewReconcileIAPResult(userID EvrID) *ReconcileIAPResult {
	return &ReconcileIAPResult{
		EvrID: userID,
		IAPData: IAPData{
			Balance: IAPBalance{
				Currency: IAPCurrency{
					EchoPoints: IAPEchoPoints{
						Value: 0,
					},
				},
			},
			TransactionId: 1,
		},
	}
}

func (r *ReconcileIAPResult) String() string {
	return fmt.Sprintf("%T(user_id=%v, iap_data=%v)", r, r.EvrID, r.IAPData)
}

func (r *ReconcileIAPResult) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&r.EvrID) },
		func() error { return s.StreamJSON(&r.IAPData, true, NoCompression) },
	})
}
