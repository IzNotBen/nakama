package evr

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofrs/uuid/v5"
)

// PlatformCode represents the platforms on which a client may be operating.

// EvrId represents an identifier for a user on the platform.
type EvrId struct {
	PlatformCode PlatformCode
	AccountId    uint64
}

func (e EvrId) MarshalText() ([]byte, error) {
	if e.PlatformCode == 0 && e.AccountId == 0 {
		return nil, nil
	}
	return []byte(e.Token()), nil
}

func (e *EvrId) UnmarshalText(b []byte) error {
	s := string(b)
	if s == "" {
		*e = EvrId{}
	}
	parsed, err := ParseEvrID(s)
	if err != nil {
		return err
	}
	*e = *parsed
	return nil
}

func (e EvrId) MarshalJSON() ([]byte, error) {
	if e.PlatformCode == 0 && e.AccountId == 0 {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, e.Token())), nil
}

func (e EvrId) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf, uint64(e.PlatformCode))
	binary.LittleEndian.PutUint64(buf[8:], e.AccountId)
	return buf, nil
}

func (e *EvrId) UnmarshalBinary(data []byte) error {
	if len(data) != 16 {
		return fmt.Errorf("invalid data length: %d", len(data))
	}
	e.PlatformCode = PlatformCode(binary.LittleEndian.Uint64(data))
	e.AccountId = binary.LittleEndian.Uint64(data[8:])
	return nil
}

func (e EvrId) Valid() bool {
	return e.AccountId > 0 && e.PlatformCode > 0 && int(e.PlatformCode) < len(platforms)
}

func (e EvrId) Nil() bool {
	return e.PlatformCode == 0 && e.AccountId == 0
}

func (e EvrId) NotNil() bool {
	return e.PlatformCode != 0 && e.AccountId != 0
}

func (e EvrId) UUID() uuid.UUID {
	if e.PlatformCode == 0 || e.AccountId == 0 {
		return uuid.Nil
	}
	return uuid.NewV5(uuid.Nil, e.Token())
}

// Parse parses a string into a given platform identifier.
func ParseEvrID(s string) (*EvrId, error) {
	// The platform code might have a hyphen in it, so find the last hyphen.
	dashIndex := strings.LastIndex(s, "-")
	if dashIndex < 0 {
		return nil, fmt.Errorf("invalid format: %s", s)
	}

	platformCode := PlatformCode(0).Parse(s[:dashIndex])

	accountID, err := strconv.ParseUint(s[dashIndex+1:], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse account identifier: %v", err)
	}

	platformId := &EvrId{PlatformCode: platformCode, AccountId: accountID}
	return platformId, nil
}

func (e EvrId) String() string {
	return e.Token()
}

func (e EvrId) Token() string {
	return fmt.Sprintf("%s-%d", e.PlatformCode.String(), e.AccountId)
}

func (e EvrId) Equals(other EvrId) bool {
	return e.PlatformCode == other.PlatformCode && e.AccountId == other.AccountId
}

func (e EvrId) IsNil() bool {
	return e.PlatformCode == 0 && e.AccountId == 0
}

func (e EvrId) IsNotNil() bool {
	return e.PlatformCode != 0 && e.AccountId != 0
}

func (e *EvrId) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.StreamNumber(binary.LittleEndian, &e.PlatformCode) },
		func() error { return s.StreamNumber(binary.LittleEndian, &e.AccountId) },
	})
}

// The indexes of these platforms are used in the EvrId struct.
var platforms = []string{
	"STM",     // Steam
	"PSN",     // Playstation
	"XBX",     // Xbox
	"OVR-ORG", // Oculus VR user
	"OVR",     // Oculus VR
	"BOT",     // Bot/AI
	"DMO",     // Demo (no ovr)
	"TEN",     // Tencent
}

type PlatformCode uint16

func (c PlatformCode) Symbol() Symbol {
	// OVR_ORG is a special case, and must be converted to OVR-ORG.
	return ToSymbol(c.String())
}

// GetPrefix obtains a platform prefix string for a given PlatformCode.
func (c PlatformCode) String() string {
	if int(c) < len(platforms) {
		return platforms[c]
	}
	return "UNK"
}

// Parse parses a string generated from PlatformCode's String() method back into a PlatformCode.
func (c PlatformCode) Parse(s string) PlatformCode {
	// Convert any underscores in the string to dashes.
	for i, platform := range platforms {
		if platform == s {
			return PlatformCode(i)
		}
	}
	return 0
}
