package evr

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMarshal(t *testing.T) {

	codec := NewCodec(nil)

	unrequirePacket := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Header
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}

	sessionEndPacket := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Magic
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.BroadcasterSessionEnd)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}

	type fields struct {
		codec *Codec
	}

	tests := []struct {
		name     string
		fields   fields
		messages []Message
		want     []byte
		wantErr  bool
	}{
		{
			"Single message",
			fields{
				codec: NewCodec(nil),
			},
			[]Message{
				&STcpConnectionUnrequireEvent{},
			},
			unrequirePacket,
			false,
		},
		{
			"Multiple message",
			fields{
				codec: NewCodec(nil),
			},
			[]Message{
				&STcpConnectionUnrequireEvent{},
				&BroadcasterSessionEnded{},
			},
			append(unrequirePacket, sessionEndPacket...),
			false,
		},
	}
	for _, tt := range tests {

		got, err := codec.Marshal(tt.messages...)
		if err != nil && !tt.wantErr {
			t.Errorf("Unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("test `%s` (got, want): %v", tt.name, cmp.Diff(got, tt.want))
		}
	}
}

func TestUnmarshal(t *testing.T) {

	codec := NewCodec(nil)

	unrequireChunk := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Header
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}

	//
	sessionEndChunk := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Magic
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}

	tests := []struct {
		name    string
		packet  []byte
		want    []Message
		wantErr bool
	}{
		{
			"Valid byte slice, multiple messages",
			append(unrequireChunk, sessionEndChunk...),
			[]Message{
				&STcpConnectionUnrequireEvent{},
				&BroadcasterSessionEnded{},
			},
			false,
		},
		{
			"Two packets, Unknown Symbol in first message",
			func() []byte {
				d := append(unrequireChunk, sessionEndChunk...)
				d[10] = 0x00
				return d
			}(),
			[]Message{
				&BroadcasterSessionEnded{},
			},
			true,
		},
		{
			"Two packets, Unknown Symbol in both messages",
			func() (b []byte) {
				b = append(unrequireChunk, sessionEndChunk...)
				b[10] = 0x00
				b[len(unrequireChunk)+10] = 0x00
				return b
			}(),
			[]Message{},
			true,
		},
		{
			"Short packet in first message, valid second",
			func() (b []byte) {
				b = append(unrequireChunk[:len(unrequireChunk)-8], sessionEndChunk...)
				b[len(unrequireChunk)+10] = 0x00
				return b
			}(),
			[]Message{
				&BroadcasterSessionEnded{},
			},
			true,
		},
		{
			"Short packet in second message, valid first",
			func() []byte {
				d := append(unrequireChunk, unrequireChunk[:len(sessionEndChunk)-8]...)
				d[10] = 0x00
				return d
			}(),
			[]Message{
				&STcpConnectionUnrequireEvent{},
			},
			true,
		},
	}
	for _, tt := range tests {

		messages, err := codec.Unmarshal(tt.packet)
		if err != nil && !tt.wantErr {
			t.Errorf("Unexpected error: %v", err)
		}
		if !reflect.DeepEqual(messages, tt.want) {
			t.Errorf(cmp.Diff(messages, tt.want))
		}

		// Don't encode if the packet is invalid
		if !tt.wantErr {
			// Encode the messages
			encoded, err := codec.Marshal(tt.want...)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Compare the encoded byte slice with the original
			if !bytes.Equal(encoded, tt.packet) {
				t.Errorf(cmp.Diff(encoded, tt.packet))
			}
		}

	}
}

func TestToSymbol(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want Symbol
	}{
		{
			name: "Symbol",
			v:    Symbol(0x1234567890abcdef),
			want: Symbol(0x1234567890abcdef),
		},
		{
			name: "int64",
			v:    int64(123),
			want: Symbol(123),
		},
		{
			name: "uint64",
			v:    uint64(456),
			want: Symbol(456),
		},
		{
			name: "SymbolToken",
			v:    SymbolToken("0xabcdef"),
			want: Symbol(0xabcdef),
		},
		{
			name: "string",
			v:    "0x1234567890abcdef",
			want: Symbol(0x1234567890abcdef),
		},
		{
			name: "string",
			v:    "0x0000000000000001",
			want: Symbol(0x0000000000000001),
		},
		{
			name: "empty string",
			v:    "",
			want: Symbol(0),
		},
		{
			name: "unsupported type",
			v:    true,
			want: Symbol(0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToSymbol(tt.v); got != tt.want {
				t.Errorf("ToSymbol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSymbol_IsNotNil(t *testing.T) {
	tests := []struct {
		name string
		s    Symbol
		want bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsNotNil(); got != tt.want {
				t.Errorf("Symbol.IsNotNil() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCodec_packetize(t *testing.T) {
	type fields struct {
		magic           []byte
		symbolMap       map[uint64]any
		symbolLookup    map[string]uint64
		ignoredSymbols  []uint64
		ignoredPatterns [][]byte
	}
	type args struct {
		chunks [][]byte
	}
	tests := []struct {
		name       string
		fields     fields
		args       args
		wantPacket []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Codec{
				magic:           tt.fields.magic,
				symbolMap:       tt.fields.symbolMap,
				symbolLookup:    tt.fields.symbolLookup,
				ignoredSymbols:  tt.fields.ignoredSymbols,
				ignoredPatterns: tt.fields.ignoredPatterns,
			}
			if gotPacket := p.packetize(tt.args.chunks); !reflect.DeepEqual(gotPacket, tt.wantPacket) {
				t.Errorf("Codec.packetize() = %v, want %v", gotPacket, tt.wantPacket)
			}
		})
	}
}

func TestCodec_Packetize(t *testing.T) {

	message := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Magic
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size
		0x01, // Size
	}

	type fields struct {
	}
	type args struct {
		chunks [][]byte
	}
	tests := []struct {
		name       string
		fields     fields
		args       args
		wantPacket []byte
	}{
		{
			"Valid byte slice",
			fields{},
			args{
				[][]byte{
					message[24:],
				},
			},
			message,
		},
		{
			"Empty byte slice",
			fields{},
			args{
				[][]byte{},
			},
			nil,
		},
		{
			"Multiple byte slices",
			fields{},
			args{
				[][]byte{
					message,
					message,
				},
			},
			append(MessageMarker, message...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewCodec(nil)
			if gotPacket := p.Packetize(tt.args.chunks...); !reflect.DeepEqual(gotPacket, tt.wantPacket) {
				t.Errorf("Codec.Packetize() Test `%s` (- want, + got): %s", tt.name, cmp.Diff(gotPacket, tt.wantPacket))
			}
		})
	}
}
func TestSymbol_MarshalBytes(t *testing.T) {
	tests := []struct {
		name   string
		symbol Symbol
		want   []byte
	}{
		{
			name:   "Non-zero symbol",
			symbol: Symbol(0x1234567890abcdef),
			want:   []byte{0xef, 0xcd, 0xab, 0x90, 0x78, 0x56, 0x34, 0x12},
		},
		{
			name:   "Zero symbol",
			symbol: Symbol(0),
			want:   []byte{0, 0, 0, 0, 0, 0, 0, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.symbol.MarshalBytes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf(cmp.Diff(got, tt.want))
			}
		})
	}
}
func TestSymbol_UnmarshalBytes(t *testing.T) {
	tests := []struct {
		name   string
		symbol Symbol
		bytes  []byte
		want   Symbol
	}{
		{
			name:   "Non-zero symbol",
			symbol: Symbol(0),
			bytes:  []byte{0xef, 0xcd, 0xab, 0x90, 0x78, 0x56, 0x34, 0x12},
			want:   Symbol(0x1234567890abcdef),
		},
		{
			name:   "Zero symbol",
			symbol: Symbol(0),
			bytes:  []byte{0, 0, 0, 0, 0, 0, 0, 0},
			want:   Symbol(0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.symbol
			s.UnmarshalBytes(tt.bytes)
			if s != tt.want {
				t.Errorf("Symbol.UnmarshalBytes() = %v, want %v", s, tt.want)
			}
		})
	}
}
func TestSymbol_MarshalText_NonNil(t *testing.T) {
	s := Symbol(123)
	expected := []byte(`"123"`)

	result, err := s.MarshalText()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !bytes.Equal(result, expected) {
		t.Errorf("Expected %s, but got %s", expected, result)
	}
}

func TestSymbol_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		s       Symbol
		want    []byte
		wantErr bool
	}{
		{
			name:    "Non-zero symbol, unknown symbol",
			s:       Symbol(123),
			want:    []byte(`0x000000000000007b`),
			wantErr: false,
		},
		{
			name:    "Zero symbol",
			s:       Symbol(0),
			want:    []byte(`0x0000000000000000`),
			wantErr: false,
		},
		{
			name: "Known Custom Symbol Token",
			s:    Symbol(0x7777777777770200),
			want: []byte(`ERGameServerEndSession`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("Symbol.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Symbol.MarshalText() = %v, want %v", string(got), string(tt.want))
			}
		})
	}
}
