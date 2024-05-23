package evr

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/go-restruct/restruct"
	"github.com/google/go-cmp/cmp"
)

var (
	//
	testMessageData = []byte{
		0xf6, 0x40, 0xbb, 0x78, 0xa2, 0xe7, 0x8c, 0xbb, // Header
		0xe4, 0xee, 0x6b, 0xc7, 0x3a, 0x96, 0xe6, 0x43, // Symbol (*evr.STCPConnectionUnrequireEvent)
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Length
		0x00, // Data
	}
)

func TestMultipleEnvelopeUnmarshal(t *testing.T) {

	// Test case 2: Valid byte slice
	data := append(testMessageData, testMessageData...)
	restruct.EnableExprBeta()
	packet := &Packet{}
	if err := restruct.Unpack(data, binary.LittleEndian, &packet); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	packet, err := Unpack(data)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	encoded, err := Pack(packet)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	tests := []struct {
		got  []byte
		want []byte
	}{
		{encoded, data},
	}
	for _, tt := range tests {
		if got := tt.got; !bytes.Equal(got, tt.want) {
			t.Errorf(string(tt.got))
			t.Errorf("got %v, want %v", got, tt.want)
			t.Errorf(cmp.Diff(got, tt.want))
		}
	}
}

func TestMultipleEnvelopeMarshal(t *testing.T) {

	data := append(testMessageData, testMessageData...)
	restruct.EnableExprBeta()
	packet := &Packet{}
	if err := restruct.Unpack(data, binary.LittleEndian, &packet); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	encoded, err := Pack(packet)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	tests := []struct {
		got  []byte
		want []byte
	}{
		{encoded, data},
	}
	for _, tt := range tests {
		if got := tt.got; !bytes.Equal(got, tt.want) {
			t.Errorf("got %v, want %v", got, tt.want)
			t.Errorf(cmp.Diff(got, tt.want))
		}
	}
}

func TestPack(t *testing.T) {

	tests := []struct {
		name      string
		wantValue any
		wantData  []byte
		wantErr   bool
	}{
		{
			name:      "TCPUnrequireEvent",
			wantValue: &STcpConnectionUnrequireEvent{},
			wantData:  []byte{0x00},
			wantErr:   false,
		},
		{
			name: "ConfigRequestv2",
			wantValue: &ConfigRequest{
				Type: "active_store_featured_entry",
				ID:   "active_store_featured_entry",
			},
			wantData: []byte{
				0x31, 0x7b, 0x22, 0x74, 0x79, 0x70, 0x65, 0x22, 0x3a, 0x22, 0x61, 0x63, 0x74, 0x69, 0x76, 0x65,
				0x5f, 0x73, 0x74, 0x6f, 0x72, 0x65, 0x5f, 0x66, 0x65, 0x61, 0x74, 0x75, 0x72, 0x65, 0x64, 0x5f,
				0x65, 0x6e, 0x74, 0x72, 0x79, 0x22, 0x2c, 0x22, 0x69, 0x64, 0x22, 0x3a, 0x22, 0x61, 0x63, 0x74,
				0x69, 0x76, 0x65, 0x5f, 0x73, 0x74, 0x6f, 0x72, 0x65, 0x5f, 0x66, 0x65, 0x61, 0x74, 0x75, 0x72,
				0x65, 0x64, 0x5f, 0x65, 0x6e, 0x74, 0x72, 0x79, 0x22, 0x7d, 0x00,
			},

			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the unpack
			// Create a new instance of the message type
			gotValue := reflect.New(reflect.TypeOf(tt.wantValue).Elem()).Interface()
			// Unpack the data into the new instance
			err := restruct.Unpack(tt.wantData, binary.LittleEndian, gotValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unpack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(gotValue, tt.wantValue) {
				t.Errorf("Unpack() = %v, want %v", gotValue, tt.wantValue)
			}

			// Test the packing
			envelope, err := NewEnvelope(tt.wantValue)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			gotData, err := restruct.Pack(binary.LittleEndian, envelope)
			if (err != nil) != tt.wantErr {
				t.Errorf("Pack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotData, tt.wantData) {
				t.Errorf("Pack() error: got %v, want %v", gotData, tt.wantData)
				t.Errorf(cmp.Diff(gotData, tt.wantData))
			}
		})
	}
}
