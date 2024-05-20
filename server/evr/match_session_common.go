package evr

import "github.com/gofrs/uuid/v5"

type LobbySessionRequest interface {
	GetChannel() uuid.UUID
	GetMode() Symbol
}
