package evr

import (
	"fmt"
)

// SNSPurchaseItems
type IAPPurchaseItems struct {
}

func (m IAPPurchaseItems) Token() string {
	return "SNSIAPPurchaseItem"
}

func (m IAPPurchaseItems) Symbol() Symbol {
	return ToSymbol(m.Token())
}

func (m IAPPurchaseItems) String() string {
	return fmt.Sprintf("%s()", m.Token())
}

func (r *IAPPurchaseItems) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Skip(1) },
	})
}

func (r *IAPPurchaseItems) ToString() string {
	return fmt.Sprintf("%s()", r.Token())
}
