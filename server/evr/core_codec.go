package evr

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

var (
	MessageMarker     = []byte{246, 64, 187, 120, 162, 231, 140, 187}
	MaxPacketLength   = 1024 * 1024 * 10 // 10MB
	MaxMessageLength  = 0x8000           // 32KB
	ErrInvalidPacket  = errors.New("invalid packet")
	ErrSymbolNotFound = errors.New("symbol not found")
	ErrParseError     = errors.New("parse error")
	codec             = NewCodec(nil)
)

type Symbol uint64

func (s Symbol) IsNil() bool {
	return s == 0xffffffffffffffff || s == 0
}

func (s Symbol) IsZero() bool {
	return s == 0
}

func (s Symbol) IsNotNil() bool {
	return s != 0xffffffffffffffff && s != 0
}

// A symbol token is a symbol converted to a string.
// It either uses the cache to convert back to a string,
// or returns the hex string representation of the token.
// ToSymbol will detect 0x prefixed hex strings.
func (s Symbol) Token() SymbolToken {
	t, ok := SymbolCache[uint64(s)]
	if !ok {
		// If it's not found, just return the number as a hex string
		str := strconv.FormatUint(uint64(s), 16)
		t = SymbolToken(fmt.Sprintf("0x%016s", str))
	}
	return t
}

func (s Symbol) MarshalBytes() (b []byte) {
	// Encode little-endian uint64
	binary.LittleEndian.PutUint64(b, uint64(s))
	return
}

func (s *Symbol) UnmarshalBytes(b []byte) {
	// Decode little-endian uint64
	*s = Symbol(binary.LittleEndian.Uint64(b))
}

func (s Symbol) MarshalJSON() ([]byte, error) {
	v := s.Token().String()
	return json.Marshal(v)
}

func (s *Symbol) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = ToSymbol(v)
	return nil
}

func (s Symbol) String() string {
	return s.Token().String()
}

// A symbol token is a symbol converted to a string.
// It either uses the cache to convert back to a string,
// or returns the hex string representation of the token.
// ToSymbol will detect 0x prefixed hex strings.
type SymbolToken string

func (t SymbolToken) Symbol() Symbol {
	return ToSymbol(t)
}

func (t SymbolToken) String() string {
	return string(t)
}

// ToSymbol converts a string value to a symbol.
func ToSymbol(v any) Symbol {
	// ia it's a number, return it as an uint64
	switch t := v.(type) {
	case Symbol:
		return t
	case int:
		return Symbol(t)
	case int64:
		return Symbol(t)
	case uint64:
		return Symbol(t)
	case SymbolToken:
		return ToSymbol(string(t))
	case string:
		str := t
		// Empty string returns 0
		if len(str) == 0 {
			return Symbol(0)
		}
		// if it's a hex represenatation, return it's value
		if str[:2] == "0x" && len(str) > 2 {
			if s, err := strconv.ParseUint(string(str[2:]), 16, 64); err == nil {
				return Symbol(s)
			}
		}
		// Convert it to a symbol
		symbol := uint64(0xffffffffffffffff)
		// lowercase the string
		for i := range str {
			a := str[i] + ' '
			if str[i] < 'A' || str[i] >= '[' {
				a = str[i]
			}
			symbol = uint64(a) ^ symbolSeed[symbol>>0x38&0xff] ^ symbol<<8
		}
		return Symbol(symbol)
	case []byte:
		if len(t) == 8 {
			return Symbol(binary.LittleEndian.Uint64(t))
		}
	default:
		panic(fmt.Errorf("invalid type: %T", v))
	}
	return Symbol(0)
}

// Message is a Evr message that can be sent over the network.
type Message interface {
	Stream(s *EasyStream) error
}

type Codec struct {
	magic           []byte // []byte{246, 64, 187, 120, 162, 231, 140, 187}
	symbolMap       map[uint64]any
	symbolLookup    map[string]uint64
	ignoredSymbols  []uint64
	ignoredPatterns [][]byte
}

func NewCodec(ignoredSymbols []uint64) *Codec {
	ignoredPatterns := make([][]byte, len(ignoredSymbols))
	for i, s := range ignoredSymbols {
		binary.LittleEndian.PutUint64(ignoredPatterns[i], s)
	}

	symbolMap := map[uint64]any{
		// This is the complete list of implemented message types.
		0x4c1fed6cb4d96c64: (*SNSLobbySmiteEntrant)(nil),
		0x013e99cb47eb3669: (*GenericMessage)(nil),
		0x35d810572a230837: (*GenericMessageNotify)(nil),
		0x80119c19ac72d695: (*MatchEnded)(nil),
		0x0dabc24265508a82: (*ReconcileIAPResult)(nil),
		0x1225133828150da3: (*OtherUserProfileFailure)(nil),
		0x1230073227050cb5: (*OtherUserProfileSuccess)(nil),
		0x1231172031050cb2: (*OtherUserProfileRequest)(nil),
		0x128b777ae0ebb650: (*LobbyMatchmakerStatusRequest)(nil),
		0x1bd0fc454c85573c: (*ReconcileIAP)(nil),
		0x244b47685187eae1: (*RemoteLogSet)(nil),
		0x2f03468f77ffb211: (*LobbyJoinSessionRequest)(nil),
		0x312c2a01819aa3f5: (*LobbyFindSessionRequest)(nil),
		0x43e6963ac76beee4: (*STcpConnectionUnrequireEvent)(nil),
		0xb99f11d6ea5cb1f1: (*LobbySessionFailurev1)(nil),
		0x4ae8365ebc45f96a: (*LobbySessionFailurev2)(nil),
		0x4ae8365ebc45f96b: (*LobbySessionFailurev3)(nil),
		0x4ae8365ebc45f96c: (*LobbySessionFailurev4)(nil),
		0x599a6b1bbda3cc13: (*LobbyCreateSessionRequest)(nil),
		0xfabf5f8719bfebf3: (*LobbyPingRequest)(nil),
		0x6047d0043033ae4f: (*LobbyPingResponse)(nil),
		0x6c8f16cd9f8964c5: (*ChannelInfoResponse)(nil),
		0x6d4de3650ee3110e: (*LobbySessionSuccessv4)(nil),
		0x6d4de3650ee3110f: (*LobbySessionSuccessv5)(nil),
		0x6d54a19a3ff24415: (*UpdateClientProfile)(nil),
		0x7777777777770000: (*BroadcasterStartSession)(nil),
		0x7777777777770100: (*BroadcasterSessionStarted)(nil),
		0x7777777777770200: (*BroadcasterSessionEnded)(nil),
		0x7777777777770300: (*BroadcasterPlayerSessionsLocked)(nil),
		0x7777777777770400: (*BroadcasterPlayerSessionsUnlocked)(nil),
		0x7777777777770500: (*BroadcasterPlayersAccept)(nil),
		0x7777777777770600: (*BroadcasterPlayersAccepted)(nil),
		0x7777777777770700: (*BroadcasterPlayersRejected)(nil),
		0x7777777777770800: (*BroadcasterPlayerRemoved)(nil),
		0x7777777777770900: (*BroadcasterChallengeRequest)(nil),
		0x7777777777770a00: (*GameServerChallengeResponse)(nil),
		0x7777777777777777: (*BroadcasterRegistrationRequest)(nil),
		0x82869f0b37eb4378: (*ConfigRequest)(nil),
		0xb9cdaf586f7bd012: (*ConfigSuccess)(nil),
		0x9e687a63dddd3870: (*ConfigFailure)(nil),
		0x8d5ad3c4f2166c6c: (*FindServerRegionInfo)(nil),
		0x8da9eb83ffee9fd6: (*LobbyPendingSessionCancel)(nil),
		0x8f28cf33dabfbecb: (*LobbyMatchmakerStatus)(nil),
		0x90758e58515724e0: (*ChannelInfoRequest)(nil),
		0x9af2fab2a0c81a05: (*LobbyPlayerSessionsRequest)(nil),
		0xa1b9cae1f8588968: (*LobbyPlayerSessionsSuccessv2)(nil),
		0xa1b9cae1f8588969: (*LobbyPlayerSessionsSuccessv3)(nil),
		0xbdb41ea9e67b200a: (*LoginRequestV2)(nil),
		0xa5add1bb1b0cce40: (*LoginRequestV1)(nil),
		0xa5acc1a90d0cce47: (*LoginSuccess)(nil),
		0xa5b9d5a3021ccf51: (*LoginFailure)(nil),
		0xb56f25c7dfe6ffc9: (*BroadcasterRegistrationFailure)(nil),
		0xb57a31cdd0f6fedf: (*BroadcasterRegistrationSuccess)(nil),
		0xd06ae97220a7b41f: (*DocumentFailure)(nil),
		0xd07ffd782fb7b509: (*DocumentSuccess)(nil),
		0xd2986849b36b9c72: (*UserServerProfileUpdateRequest)(nil),
		0xd299785ba56b9c75: (*UserServerProfileUpdateSuccess)(nil),
		0xe4b9b1cab57e8988: (*LobbyStatusNotify)(nil),
		0xed5be2c3632155f1: (*LoginSettings)(nil),
		0xf24185da0edef641: (*UpdateProfileFailure)(nil),
		0xf25491d001cef757: (*UpdateProfileSuccess)(nil),
		0xfb632e5a38ec8c61: (*LoggedInUserProfileFailure)(nil),
		0xfb763a5037fc8d77: (*LoggedInUserProfileSuccess)(nil),
		0xfb772a4221fc8d70: (*LoggedInUserProfileRequest)(nil),
		0xfcced6f169822bb8: (*DocumentRequest)(nil),
		0xff71856af7e0fbd9: (*LobbyPlayerSessionsSuccessUnk1)(nil),
	}

	symbolLookup := make(map[string]uint64, len(symbolMap))
	// Populate the new map
	for key, value := range symbolMap {
		typeName := reflect.TypeOf(value).String()
		symbolLookup[typeName] = key
	}

	return &Codec{
		magic:           MessageMarker,
		symbolMap:       symbolMap,
		ignoredSymbols:  ignoredSymbols,
		ignoredPatterns: ignoredPatterns,
	}
}

func (p *Codec) FromSymbol(symbol Symbol) (any, error) {
	typ, ok := p.symbolMap[uint64(symbol)]
	if !ok {
		return nil, ErrSymbolNotFound
	}
	return typ, nil
}
func (p *Codec) depacketize(packet []byte) (chunks [][]byte) {
	if !bytes.HasPrefix(packet, MessageMarker) {
		return nil
	}
	chunks = bytes.Split(packet, MessageMarker)
	for i, chunk := range chunks {
		for _, pattern := range p.ignoredPatterns {
			if bytes.HasPrefix(chunk, pattern) {
				chunks = append(chunks[:i], chunks[i+1:]...)
				break
			}
		}
	}
	return chunks
}

func (p *Codec) unwrap(chunk []byte) (symbol uint64, data []byte, err error) {
	buf := bytes.NewBuffer(chunk)
	// Verify packet length.
	if buf.Len() < 16 {
		return 0, nil, fmt.Errorf("packet too short: %d bytes", buf.Len())
	}
	// Read the message type
	sym := dUint64(buf.Next(8))
	// Read the payload length
	l := int(dUint64(buf.Next(8)))
	// Verify the message data can be read from the rest of the packet.
	if buf.Len() != l {
		return 0, nil, fmt.Errorf("truncated packet (expected %d bytes, got %d)", l, buf.Len())
	}
	// Read the payload.
	return sym, buf.Next(l), nil
}

func (p *Codec) decode(data []byte, message Message) (err error) {
	return message.Stream(NewEasyStream(DecodeMode, data))
}

func (p *Codec) encode(data []byte, message Message) (symbol uint64, err error) {
	return uint64(SymbolOf(message)), message.Stream(NewEasyStream(EncodeMode, data))
}

func (p *Codec) wrap(symbol uint64, data []byte) (chunk []byte, err error) {
	// Write the Header (Marker + Symbol + Data Length)
	chunk = appendUint64(chunk, symbol)
	chunk = appendUint64(chunk, uint64(len(data)))
	// Write the message data.
	chunk = append(chunk, data...)
	return chunk, nil
}

func (p *Codec) Wrap(symbol Symbol, data []byte) (chunk []byte, err error) {
	return p.wrap(uint64(symbol), data)
}

func (p *Codec) packetize(chunks [][]byte) (packet []byte) {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) == 1 {
		return append(MessageMarker, chunks[0]...)
	}
	if bytes.HasPrefix(chunks[0], MessageMarker) {
		return bytes.Join(chunks, MessageMarker)
	}
	return append(MessageMarker, bytes.Join(chunks, MessageMarker)...)
}

// Marshal returns the wire-format encoding of multiple messages.
func (p *Codec) Marshal(msgs ...Message) (packet []byte, errs error) {
	var err error
	var symbol uint64
	chunks := make([][]byte, 0, len(msgs))

	for _, m := range msgs {
		data := make([]byte, 0)
		symbol, err = p.encode(data, m)
		if err != nil {
			errs = errors.Join(err, errs)
			continue
		}
		chunk, err := p.wrap(symbol, data)
		if err != nil {
			errs = errors.Join(err, errs)
			continue
		}
		chunks = append(chunks, chunk)
	}

	packet = p.packetize(chunks)

	return packet, errs
}

func (p *Codec) Unmarshal(data []byte) (messages []Message, err error) {
	var errs error
	for _, chunk := range p.depacketize(data) {
		if len(chunk) == 0 {
			// Skip empty messages.
			continue
		}

		// Parse the message.
		symbol, data, err := p.unwrap(data)
		if err != nil {
			errs = errors.Join(err, errs)
			continue
		}
		// Get the message type.
		typ, ok := codec.symbolMap[symbol]
		if !ok {
			errs = errors.Join(fmt.Errorf("Symbol not found: symbol=0x%016x", errs))
			continue
		}
		// Create a new message of the correct type and unmarshal the data into it.
		message := reflect.New(reflect.TypeOf(typ).Elem()).Interface().(Message)
		if err := p.decode(data, message); err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		messages = append(messages, message)
	}

	return nil, errs
}

func (p *Codec) AddHeader(symbol uint64, message Message) (packet []byte, err error) {
	data := make([]byte, 0)
	if _, err = p.encode(data, message); err != nil {
		return nil, err
	}
	return p.wrap(symbol, data)
}

// AppendUint64 appends the (little-endian) byte representation of v to b and returns the resulting slice.
func appendUint64(b []byte, v uint64) []byte {
	return append(b,
		byte(v),
		byte(v>>8),
		byte(v>>16),
		byte(v>>24),
		byte(v>>32),
		byte(v>>40),
		byte(v>>48),
		byte(v>>56),
	)
}

// Uint64 decodes a little-endian uint64 from the provided byte slice.
func dUint64(b []byte) uint64 {
	_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808
	return uint64(b[0]) |
		uint64(b[1])<<8 |
		uint64(b[2])<<16 |
		uint64(b[3])<<24 |
		uint64(b[4])<<32 |
		uint64(b[5])<<40 |
		uint64(b[6])<<48 |
		uint64(b[7])<<56
}

// SymbolOf returns the type symbol of the message.
func SymbolOf(m Message) Symbol {
	typ := reflect.TypeOf(m).String()
	sym, ok := codec.symbolLookup[typ]
	if !ok {
		panic(fmt.Errorf("Symbol not found: %T", m))
	}
	return Symbol(sym)
}

// MessageTypeOf returns a new instance of the message type.
func MessageTypeOf(s Symbol) Message {
	if m, ok := codec.symbolMap[uint64(s)]; ok {
		// return a new instance of the message type
		return reflect.New(reflect.TypeOf(m).Elem()).Interface().(Message)
	}
	return nil
}
