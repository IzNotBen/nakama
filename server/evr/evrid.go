package evr

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofrs/uuid/v5"
)

const (
	STM     uint64 = iota // Steam
	PSN                   // Playstation
	XBX                   // Xbox
	OVR_ORG               // Oculus VR user
	OVR                   // Oculus VR
	BOT                   // Bot/AI
	DMO                   // Demo (no ovr)
	TEN                   // Tencent
)

var (
	ErrInvalidEvrID = fmt.Errorf("invalid EvrID")

	codeMap = map[uint64]string{
		STM:     "STM",
		PSN:     "PSN",
		XBX:     "XBX",
		OVR_ORG: "OVR_ORG",
		OVR:     "OVR",
		BOT:     "BOT",
		DMO:     "DMO",
		TEN:     "TEN",
	}

	codeMapReverse = map[string]uint64{
		"STM":     STM,
		"PSN":     PSN,
		"XBX":     XBX,
		"OVR_ORG": OVR_ORG,
		"OVR":     OVR,
		"BOT":     BOT,
		"DMO":     DMO,
		"TEN":     TEN,
	}
)

// EvrID represents an identifier for a user on the platform.
type EvrID struct {
	PlatformCode uint64
	AccountID    uint64
}

func NewEvrID(platformCode uint64, accountId uint64) *EvrID {
	return &EvrID{PlatformCode: platformCode, AccountID: accountId}
}

func (e EvrID) MarshalText() ([]byte, error) {
	if e.PlatformCode == 0 && e.AccountID == 0 {
		return []byte{}, nil
	}
	return []byte(e.Token()), nil
}

func (e *EvrID) UnmarshalText(b []byte) (err error) {
	s := string(b)
	if s == "" {
		*e = EvrID{}
	}

	dashIndex := strings.LastIndex(s, "-")
	if dashIndex < 0 {
		return ErrInvalidEvrID
	}
	platformCodeStr := strings.ReplaceAll(s[:dashIndex], "_", "-")
	accountIdStr := s[dashIndex+1:]

	for code, name := range codeMap {
		if name == platformCodeStr {
			e.PlatformCode = code
			break
		}
	}
	if e.PlatformCode == 0 {
		return ErrInvalidEvrID
	}

	if e.AccountID, err = strconv.ParseUint(accountIdStr, 10, 64); err != nil {
		return fmt.Errorf("failed to parse account identifier: %v", err)
	}

	return nil
}

func (id EvrID) UUID() uuid.UUID {
	if id.PlatformCode == 0 || id.AccountID == 0 {
		return uuid.Nil
	}
	return uuid.NewV5(uuid.Nil, id.Token())
}

func (id EvrID) String() string {
	s := codeMap[id.PlatformCode]
	return fmt.Sprintf("%s-%d", s, id.AccountID)
}

func (id EvrID) Token() string {
	return id.String()
}

func (id EvrID) Equals(other EvrID) bool {
	return id.PlatformCode == other.PlatformCode && id.AccountID == other.AccountID
}

func (id EvrID) IsNil() bool {
	return id.PlatformCode == 0 && id.AccountID == 0
}

func (id EvrID) Valid() bool {
	return id.PlatformCode >= STM && id.PlatformCode <= TEN && id.AccountID > 0
}

func (id *EvrID) Stream(s *Stream) error {
	return RunErrorFunctions([]func() error{
		func() error { return s.Stream(&id.PlatformCode) },
		func() error { return s.Stream(&id.AccountID) },
	})
}

func EvrIDFromString(s string) (EvrID, error) {
	var id EvrID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		return id, err
	}
	return id, nil
}

func EvrIDFromStringOrNil(s string) EvrID {
	id, _ := EvrIDFromString(s)
	return id
}
