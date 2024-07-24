package evr

import (
	"fmt"

	"github.com/gofrs/uuid/v5"
)

type LobbySessionRequestRequest interface {
	GetAlignment() int8
	GetChannel() uuid.UUID
	GetMode() Symbol
}

type Entrant struct {
	EvrID EvrID
	Role  int8 // -1 for any team
}

func (e Entrant) String() string {
	return fmt.Sprintf("Entrant(evr_id=%s, role=%d)", e.EvrID, e.Role)
}
