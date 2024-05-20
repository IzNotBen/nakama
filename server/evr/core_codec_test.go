package evr

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMultipleUnmarshal(t *testing.T) {

	codec := NewCodec(nil)

	unrequireChunk := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Header
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}

	//
	sessionEndChunk := []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Header
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
