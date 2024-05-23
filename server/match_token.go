package server

import (
	"errors"

	"strings"

	"github.com/gofrs/uuid/v5"
)

var (
	ErrInvalidMatchTokenFormat = errors.New("invalid match token format")
	ErrInvalidMatchID          = errors.New("invalid match ID")
	ErrInvalidMatchNode        = errors.New("invalid match node")
	ErrInvalidMatchToken       = errors.New("invalid match token")
)

// MatchID represents a unique identifier for a match, consisting of a uuid.UUID and a node name.
type MatchID struct {
	uuid uuid.UUID
	node string
}

func (t MatchID) UUID() uuid.UUID {
	return t.uuid
}

func (t MatchID) Node() string {
	return t.node
}

func (MatchID) Nil() MatchID {
	return MatchID{}
}

func (t MatchID) IsNil() bool {
	return MatchID{} == t
}

func (t MatchID) IsNotNil() bool {
	return MatchID{} != t
}

func NewMatchToken(id uuid.UUID, node string) (t MatchID, err error) {
	switch {
	case id == uuid.Nil:
		err = ErrInvalidMatchID
	case node == "":
		err = ErrInvalidMatchNode
	default:
		t.uuid = id
		t.node = node
	}
	return
}

func (t MatchID) String() string {
	return t.uuid.String() + "." + t.node
}

func MatchIDFromStringOrNil(s string) (t MatchID) {
	if s == "" {
		return
	}
	t, err := MatchTokenFromString(s)
	if err != nil {
		return MatchID{}
	}
	return
}

func (t MatchID) IsValid() bool {
	if t.uuid == uuid.Nil || t.node == "" {
		return false
	}
	return true
}

func (t MatchID) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *MatchID) UnmarshalText(data []byte) error {
	token, err := MatchTokenFromString(string(data))
	if err != nil {
		return err
	}
	*t = token
	return nil
}

func MatchTokenFromString(s string) (t MatchID, err error) {
	if len(s) < 38 || s[36] != '.' {
		return t, ErrInvalidMatchToken
	}

	components := strings.SplitN(s, ".", 2)
	t.uuid = uuid.FromStringOrNil(components[0])
	t.node = components[1]

	switch {
	case t.uuid == uuid.Nil:
		err = ErrInvalidMatchID
	case t.node == "":
		err = ErrInvalidMatchNode
	}
	return
}
