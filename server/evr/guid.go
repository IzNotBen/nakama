package evr

import (
	"encoding"
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
)

var _ = encoding.BinaryMarshaler(&GUID{})

// GUID is a 16-byte globally unique identifier, represented as a UUID.
// It is used in the game to identify channels and sessions.
// It is serialized as a 32-character uppercase hexadecimal string.
// It is also serialized as a 16-byte binary blob, with the bytes reordered to match the UUID representation.
type GUID uuid.UUID

// String returns the GUID as an uppercase string
func (g GUID) String() string {
	return strings.ToUpper(uuid.UUID(g).String())
}

func (g GUID) MarshalText() ([]byte, error) {
	return []byte(g.String()), nil
}

func (g *GUID) UnmarshalText(data []byte) error {
	u, err := uuid.FromString(string(data))
	if err != nil {
		return err
	}
	*g = GUID(u)
	return nil
}

func (g GUID) MarshalBinary() (b []byte, err error) {
	b = make([]byte, 16)
	copy(b, g[:])
	b[0], b[1], b[2], b[3] = b[3], b[2], b[1], b[0]
	b[4], b[5] = b[5], b[4]
	b[6], b[7] = b[7], b[6]
	return b, nil
}

func (g *GUID) UnmarshalBinary(b []byte) error {
	if len(b) != 16 {
		return fmt.Errorf("GUID must be 16 bytes long")
	}
	copy(g[:], b)
	g[0], g[1], g[2], g[3] = g[3], g[2], g[1], g[0]
	g[4], g[5] = g[5], g[4]
	g[6], g[7] = g[7], g[6]
	return nil
}
