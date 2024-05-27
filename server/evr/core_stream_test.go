package evr

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/google/go-cmp/cmp"
)

func TestEasyStream_StreamNumber_Write(t *testing.T) {

	// Create a buffer for testing
	buf := new(bytes.Buffer)

	// Create an EasyStream instance with DecodeMode
	stream := &Stream{
		Mode: EncodeMode,
		w:    buf,
	}

	// Test uint64

	// Create a variable to hold the expected value
	uintv := uint64(1234567)
	want := []byte{0x87, 0xD6, 0x12, 0x00, 0x00, 0x00, 0x00, 0x00}
	if err := stream.StreamNumber(binary.LittleEndian, &uintv); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	if got := stream.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("uint64(%d) = %v, want %v", uintv, got, want)
	}

	stream.w = new(bytes.Buffer)
	// Test int64
	intv := int64(-1234567890)
	want = []byte{0x2e, 0xfd, 0x69, 0xb6, 0xff, 0xff, 0xff, 0xff}
	if err := stream.StreamNumber(binary.LittleEndian, &intv); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	if got := stream.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("int64(%d) = %v, want %v", intv, got, want)
	}

	stream.w = new(bytes.Buffer)
	// Test int64
	uintv = uint64(0xFFFFFFFFB669FD2E)
	want = []byte{0x2e, 0xfd, 0x69, 0xb6, 0xff, 0xff, 0xff, 0xff}
	if err := stream.StreamNumber(binary.LittleEndian, &uintv); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	if got := stream.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("int64(%d) = %v, want %v", uintv, got, want)
	}
}

func TestEasyStream_StreamNumber_Read(t *testing.T) {

	b := []byte{0x2e, 0xfd, 0x69, 0xb6, 0xff, 0xff, 0xff, 0xff}
	// Create an EasyStream instance with DecodeMode
	stream := &Stream{
		Mode: DecodeMode,
		r:    bytes.NewReader(b),
	}

	// Create a variable to hold the expected value
	want := int64(-1234567890)
	got := int64(0)
	if err := stream.StreamNumber(binary.LittleEndian, &got); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	if want != got {
		t.Errorf("[]byte(%d) = %v, want %v", b, got, want)
	}
}

func TestEasyStream_StreamNumber_WriteThenRead(t *testing.T) {

	// Create a buffer for testing
	// Create a buffer for testing
	buf := new(bytes.Buffer)

	// Create an EasyStream instance with DecodeMode
	stream := &Stream{
		Mode: EncodeMode,
		w:    buf,
	}

	// Test uint64

	stream.w = new(bytes.Buffer)
	// Test int64
	intv := int64(-1234567890)
	want := []byte{0x2e, 0xfd, 0x69, 0xb6, 0xff, 0xff, 0xff, 0xff}
	if err := stream.StreamNumber(binary.LittleEndian, &intv); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	encodedint := stream.Bytes()
	if got := encodedint; !bytes.Equal(got, want) {
		t.Errorf("int64(%d) = %v, want %v", intv, got, want)
	}

	stream.w = new(bytes.Buffer)
	// Test int64
	uintv := uint64(18446742839186183726)
	want = []byte{0x2e, 0x56, 0xac, 0x90, 0xe0, 0xfe, 0xff, 0xff}
	if err := stream.StreamNumber(binary.LittleEndian, &uintv); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	encodeduint := stream.Bytes()
	if got := encodeduint; !bytes.Equal(got, want) {
		t.Errorf("int64(%d) = %s, want %s", uintv, hex.Dump(got), hex.Dump(want))
	}

	// Create an EasyStream instance with DecodeMode
	stream = &Stream{
		Mode: DecodeMode,
		r:    bytes.NewReader(want),
	}

	// Create a variable to hold the expected value
	in := int64(-1234523367890)
	got := int64(0)
	if err := stream.StreamNumber(binary.LittleEndian, &got); err != nil {
		t.Fatalf("failed to stream number: %v", err)
	}
	if got != in {
		t.Errorf("[]byte(%d) = %v, want %v", want, got, in)
	}

}
func TestReadBytes(t *testing.T) {
	// Create a buffer for testing
	buf := bytes.NewBuffer([]byte("Hello, World!\x00"))
	// Test case 1: Read bytes until null termination
	dst := new(bytes.Buffer)
	err := ReadBytes(buf, dst, true)
	if err != nil {
		t.Fatalf("failed to read bytes: %v", err)
	}
	want := []byte("Hello, World!")
	if got := dst.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("ReadBytes() = %s, want %s", got, want)
	}
	// Test case 2: Read bytes without null termination
	buf = bytes.NewBuffer([]byte("Hello, World!"))
	dst.Reset()
	err = ReadBytes(buf, dst, false)
	if err != io.EOF {
		t.Fatalf("failed to read bytes: %v", err)
	}
	want = []byte("Hello, World!")
	if got := dst.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("ReadBytes() = %s, want %s", got, want)
	}
}

func TestEasyStream_StreamIpAddress(t *testing.T) {
	// Test data
	ip := net.ParseIP("192.168.0.1")

	// Create a buffer for testing
	buf := new(bytes.Buffer)

	// Create an EasyStream instance with EncodeMode
	stream := &Stream{
		Mode: EncodeMode,
		w:    buf,
	}

	// Stream the IP address
	if err := stream.StreamIPAddress(&ip); err != nil {
		t.Fatalf("failed to stream IP address: %v", err)
	}

	// Create a new IP variable to hold the decoded value
	decodedIP := net.IP{}

	// Create a new EasyStream instance with DecodeMode
	stream = &Stream{
		Mode: DecodeMode,
		r:    bytes.NewReader(buf.Bytes()),
	}

	// Stream the IP address back
	if err := stream.StreamIPAddress(&decodedIP); err != nil {
		t.Fatalf("failed to stream IP address: %v", err)
	}

	// Compare the original IP with the decoded IP
	if !ip.Equal(decodedIP) {
		t.Errorf("StreamIpAddress() = %s, want %s", decodedIP, ip)
	}
}

func TestEasyStream_StreamString(t *testing.T) {
	// Test data
	value := "Hello, World!"
	length := len(value) + 3

	// Create a buffer for testing
	buf := new(bytes.Buffer)

	// Create an EasyStream instance with EncodeMode
	stream := &Stream{
		Mode: EncodeMode,
		w:    buf,
	}

	// Stream the string
	if err := stream.StreamString(&value, length); err != nil {
		t.Fatalf("failed to stream string: %v", err)
	}

	// Create a new string variable to hold the decoded value
	decodedValue := ""

	// Create a new EasyStream instance with DecodeMode
	stream = &Stream{
		Mode: DecodeMode,
		r:    bytes.NewReader(buf.Bytes()),
	}

	// Stream the string back
	if err := stream.StreamString(&decodedValue, length); err != nil {
		t.Fatalf("failed to stream string: %v", err)
	}

	// Compare the original string with the decoded string
	if value != decodedValue {
		t.Errorf("StreamString() = %s, want %s", decodedValue, value)
	}
}

func TestStream_StreamCompressedBytes(t *testing.T) {

	want := []byte("Hello, World!")
	for _, termination := range []bool{true, false} {
		for _, compressionMode := range []CompressionMode{NoCompression, ZlibCompression, ZstdCompression} {
			t.Logf("nullTerminated: %v, compressionMode: %v", termination, compressionMode)

			stream := NewStreamBuffer()
			b := want[:]
			if err := stream.StreamCompressedBytes(&b, termination, compressionMode); err != nil {
				t.Fatalf("failed to stream compressed bytes: %v", err)
			}
			t.Logf(string(stream.Bytes()))
			// Create an EasyStream
			stream = NewStreamReader(stream.Bytes())

			got := []byte{}
			if err := stream.StreamCompressedBytes(&got, termination, compressionMode); err != nil {
				t.Fatalf("failed to stream compressed bytes: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Errorf("(- want, + got): %s", cmp.Diff(got, want))
			}
		}
	}
}

func TestStream_StreamGUID(t *testing.T) {
	wantGUID := GUID(uuid.FromStringOrNil("01020304-0506-0708-090a-0b0c0d0e0f10"))
	wantBytes := []byte{0x04, 0x03, 0x02, 0x01, 0x06, 0x05, 0x08, 0x07, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	stream := NewStreamBuffer()
	if err := stream.StreamGUID(&wantGUID); err != nil {
		t.Fatalf("failed to stream GUID: %v", err)
	}
	got := stream.Bytes()
	if !bytes.Equal(got, wantBytes) {
		t.Errorf("- want, + got: %s", cmp.Diff(got, wantBytes))
	}

	stream = NewStreamReader(wantBytes)
	gotGUID := GUID{}
	if err := stream.StreamGUID(&gotGUID); err != nil {
		t.Fatalf("failed to stream GUID: %v", err)
	}
	if gotGUID != wantGUID {
		t.Errorf("- want, + got: %s", cmp.Diff(gotGUID, wantGUID))
	}

}
