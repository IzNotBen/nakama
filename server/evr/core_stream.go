package evr

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-playground/validator/v10"
	"github.com/go-restruct/restruct"
	"github.com/klauspost/compress/zstd"
)

type StreamMode int
type CompressionMode int

const (
	DecodeMode StreamMode = iota
	EncodeMode

	LittleEndian = iota
	BigEndian

	NoCompression = iota
	ZlibCompression
	ZstdCompression
)

var (
	errInvalidCompressionMode = errors.New("invalid compression mode")
	errInvalidMode            = errors.New("invalid mode")
	structValidate            = validator.New()
)

type Stream struct {
	r    *bytes.Reader
	w    *bytes.Buffer
	Mode StreamMode
}

func NewStreamReader(b []byte) *Stream {
	return &Stream{
		Mode: DecodeMode,
		r:    bytes.NewReader(b),
	}
}

func NewStreamBuffer() *Stream {
	return &Stream{
		Mode: EncodeMode,
		w:    bytes.NewBuffer(nil),
	}
}

func (s *Stream) StreamSymbol(value *Symbol) error {
	return s.StreamNumber(binary.LittleEndian, value)
}

func (s *Stream) StreamNumber(order binary.ByteOrder, value any) error {
	switch s.Mode {
	case DecodeMode:
		return binary.Read(s.r, order, value)
	case EncodeMode:
		return binary.Write(s.w, order, value)
	default:
		return errInvalidMode
	}
}

func (s *Stream) StreamIPAddress(data *net.IP) error {
	b := make([]byte, net.IPv4len)
	copy(b, (*data).To4())
	if err := s.StreamBytes(&b, net.IPv4len); err != nil {
		return err
	}
	*data = net.IP(b)
	return nil
}

func (s *Stream) StreamByte(value *byte) error {
	var err error
	switch s.Mode {
	case DecodeMode:
		*value, err = s.r.ReadByte()
		return err
	case EncodeMode:
		return s.w.WriteByte(*value)
	default:
		return errInvalidMode
	}
}

// StreamBytes reads or writes bytes to the stream based on the mode of the EasyStream.
// If the mode is ReadMode, it reads bytes from the stream and stores them in the provided data slice.
// If the length parameter is -1, it reads all available bytes from the stream.
// If the mode is WriteMode, it writes the bytes from the provided data slice to the stream.
// It returns an error if there is any issue with reading or writing the bytes.
func (s *Stream) StreamBytes(dst *[]byte, l int) error {
	var err error
	switch s.Mode {
	case DecodeMode:
		if l == 0 {
			return nil
		}
		if l == -1 {
			l = s.r.Len()
		}
		*dst = make([]byte, l)
		_, err = s.r.Read(*dst)
	case EncodeMode:
		_, err = s.w.Write(*dst)
	default:
		err = errInvalidMode
	}
	return err
}

func (s *Stream) StreamString(value *string, length int) error {
	var err error

	b := make([]byte, length)
	switch s.Mode {
	case DecodeMode:
		_, err = s.r.Read(b)
		*value = string(bytes.TrimRight(b, "\x00"))
	case EncodeMode:
		copy(b, []byte(*value))
		// Zero pad the value up to the length
		for i := len(*value); i < length; i++ {
			b[i] = 0
		}
		err = s.StreamBytes(&b, len(b))
	default:
		err = errInvalidMode
	}
	return err
}

func (s *Stream) StreamNullTerminatedString(str *string) error {
	b := make([]byte, 0, len(*str))
	switch s.Mode {
	case DecodeMode:
		for {
			if c, err := s.r.ReadByte(); err != nil {
				return err
			} else if c == 0x0 {
				break
			} else {
				b = append(b, c)
			}
		}
		*str = string(b)
		return nil
	case EncodeMode:
		bytes := []byte(*str)
		bytes = append(bytes, 0)
		return s.StreamBytes(&bytes, len(bytes))
	default:
		return errors.New("StreamNullTerminatedString: invalid mode")
	}
}

func (s *Stream) StreamStruct(obj Serializer) error {
	return obj.Stream(s)
}

type Streamer any

func (s *Stream) Stream(v any) error {
	// Check if this is a restruct Unpacker or Packer
	switch s.Mode {
	case DecodeMode:
		if p, ok := v.(restruct.Unpacker); ok {
			b := make([]byte, s.r.Len())
			remaining, err := p.Unpack(b, binary.LittleEndian)
			if err != nil {
				return err
			}
			// Set position to the end of the stream - remaining
			if err = s.SetPosition(s.Len() - len(remaining)); err != nil {
				return err
			}
			return nil
		}
	case EncodeMode:
		if p, ok := v.(restruct.Packer); ok {
			b := make([]byte, p.SizeOf())
			_, err := p.Pack(b, binary.LittleEndian)
			if err != nil {
				return err
			}
			if _, err = s.w.Write(b); err != nil {
				return err
			}
			return nil
		}
	default:
		return errInvalidMode
	}

	switch t := v.(type) {
	case Serializer:
		return s.StreamStruct(t)
	case []Serializer:
		for _, t := range t {
			if err := s.StreamStruct(t); err != nil {
				return err
			}
		}
		return nil
	case []Streamer:
		for _, t := range t {
			if err := s.Stream(t); err != nil {
				return err
			}
		}
		return nil
	case *Symbol:
		return s.StreamSymbol(t)
	case *int:
		return s.StreamNumber(binary.LittleEndian, t)
	case *int32:
		return s.StreamNumber(binary.LittleEndian, t)
	case *int64:
		return s.StreamNumber(binary.LittleEndian, t)
	case *uint32:
		return s.StreamNumber(binary.LittleEndian, t)
	case *uint64:
		return s.StreamNumber(binary.LittleEndian, t)
	case *byte:
		return s.StreamByte(t)
	case *[]byte:
		return s.StreamBytes(t, -1)
	case *string:
		return s.StreamString(t, len(*t))
	case *GUID:
		return s.StreamGUID(t)
	case []GUID:
		return s.StreamGUIDs(t)
	case *[]GUID:
		return s.StreamGUIDs(*t)
	case *net.IP:
		return s.StreamIPAddress(t)

	default:
		return errors.New("Stream: unsupported type")
	}
}

// Stream multiple GUIDs
func (s *Stream) StreamGUIDs(g []GUID) error {
	var err error
	for i := 0; i < len(g); i++ {
		if err = s.StreamGUID(&g[i]); err != nil {
			return err
		}
	}
	return nil
}

// Microsoft's GUID has some bytes re-ordered.
func (s *Stream) StreamGUID(guid *GUID) error {
	var err error
	var b []byte

	switch s.Mode {
	case DecodeMode:
		b = make([]byte, 16)
		if err = s.StreamBytes(&b, 16); err != nil {
			return err
		}
		if err = guid.UnmarshalBinary(b); err != nil {
			return err
		}
	case EncodeMode:
		if b, err = guid.MarshalBinary(); err != nil {
			return err
		}
		return s.StreamBytes(&b, 16)
	default:
		return errInvalidMode
	}
	return nil
}

func (s *Stream) StreamStringTable(entries *[]string) error {
	var err error
	var strings []string
	logCount := uint64(len(*entries))
	if err = s.StreamNumber(binary.LittleEndian, &logCount); err != nil {
		return err
	}
	switch s.Mode {
	case DecodeMode:
		strings = make([]string, logCount)
		offsets := make([]uint32, logCount)
		offsets[0] = 0
		for i := 1; i < int(logCount); i++ {
			off := uint32(0)
			if err = s.StreamNumber(binary.LittleEndian, &off); err != nil {
				return err
			}
			offsets[i] = off
		}

		bufferStart := s.Position()
		for i, off := range offsets {
			if err = s.SetPosition(bufferStart + int(off)); err != nil {
				return err
			}
			if err = s.StreamNullTerminatedString(&strings[i]); err != nil {
				return err
			}
		}
		*entries = strings
	case EncodeMode:
		// write teh offfsets
		for i := 0; i < int(logCount-1); i++ {
			off := uint32(len((*entries)[i]) + 1)
			if err = s.StreamNumber(binary.LittleEndian, &off); err != nil {
				return err
			}
		}
		// write the strings
		for _, str := range *entries {
			if s.StreamNullTerminatedString(&str); err != nil {
				return err
			}
		}
	default:
		return errInvalidMode
	}
	return nil
}

func (s *Stream) Bytes() []byte {
	if s.Mode == DecodeMode {
		b := make([]byte, s.r.Len())
		s.r.Read(b)
		return b
	}
	return s.w.Bytes()
}

func (s *Stream) Len() int {
	if s.Mode == DecodeMode {
		return s.r.Len()
	}
	return s.w.Len()
}

func (s *Stream) Position() int {
	if s.Mode == DecodeMode {
		pos, _ := s.r.Seek(0, 1) // Current position
		return int(pos)
	}
	return s.w.Len()
}

func (s *Stream) SetPosition(pos int) error {
	if s.Mode == DecodeMode {
		if _, err := s.r.Seek(int64(pos), 0); err != nil {
			return errors.New("SetPosition failed: " + err.Error())
		}
		return nil
	}
	if pos > s.w.Len() {
		return errors.New("SetPosition: position out of range")
	}
	s.w.Truncate(pos)
	return nil
}

func (s *Stream) Reset() {
	if s.Mode == DecodeMode {
		s.r.Reset(s.Bytes())
	} else {
		s.w.Reset()
	}
}

func (s *Stream) Skip(count int) error {
	if s.Mode == DecodeMode {
		_, err := s.r.Seek(int64(count), io.SeekCurrent)
		return err
	} else {
		b := make([]byte, count)
		if _, err := s.w.Write(b); err != nil {
			return err
		}
	}
	return nil
}

type Serializer interface {
	Stream(s *Stream) error
}

func RunErrorFunctions(funcs []func() error) error {
	var err error
	var fn func() error
	for _, fn = range funcs {
		if err = fn(); err != nil {
			return err
		}
	}
	return nil
}

func StringifyStruct(s interface{}) string {
	return spew.Sdump(s)
}

func ValidateStruct(s interface{}) error {

	err := structValidate.Struct(s)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return err
		}
		return err.(validator.ValidationErrors)
	}
	return nil
}

// Reads the bytes to the end of the stream or the nullTerminator
func ReadBytes(r io.ByteReader, dst *bytes.Buffer, isNullTerminated bool) error {
	var err error
	var b byte
	for err != io.EOF {

		if b, err = r.ReadByte(); isNullTerminated && b == '\x00' {
			break
		} else if err != nil {

			return err
		}
		dst.WriteByte(b)
	}
	return nil
}

func (s *Stream) StreamJSON(data any, isNullTerminated bool, compressionMode CompressionMode) error {
	var err error
	switch s.Mode {
	case DecodeMode:
		b := make([]byte, 0)
		// Read the compressed bytes into data
		err = s.StreamCompressedBytes(&b, isNullTerminated, compressionMode)
		if err != nil {
			return err
		}
		// Unmarshal the data into the provided interface
		if err = json.Unmarshal(b, &data); err != nil {
			return fmt.Errorf("unmarshal json error: %w: `%s`", err, data)
		}
		return nil
	case EncodeMode:
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal json error: %w", err)
		}
		if isNullTerminated {
			b = append(b, 0x0)
		}
		// Compress the bytes and write them to the stream
		if err = s.StreamCompressedBytes(&b, isNullTerminated, compressionMode); err != nil {
			return err
		}
	default:
		return errors.New("StreamJson: invalid mode")
	}
	return nil
}

func (s *Stream) StreamCompressedBytes(data *[]byte, isNullTerminated bool, compressionMode CompressionMode) error {
	buf := bytes.Buffer{}
	var err error
	switch s.Mode {
	case DecodeMode:
		switch compressionMode {
		case NoCompression:
			if err := ReadBytes(s.r, &buf, isNullTerminated); err != nil && err != io.EOF {
				return fmt.Errorf("read bytes error: %w", err)
			}
		case ZlibCompression:
			l64 := uint64(0)
			if err = binary.Read(s.r, binary.LittleEndian, &l64); err != nil {
				return fmt.Errorf("zlib length read error: %w", err)
			}
			r, err := zlib.NewReader(s.r)
			if err != nil {
				return err
			}
			_, err = io.Copy(&buf, r)
			if err != nil {
				return err
			}
			r.Close()
		case ZstdCompression:
			l32 := uint32(0)
			if err = binary.Read(s.r, binary.LittleEndian, &l32); err != nil {
				return fmt.Errorf("zstd length read error: %w", err)
			}
			r, err := zstd.NewReader(s.r)
			if err != nil {
				return err
			}
			_, err = io.Copy(&buf, r)
			if err != nil {
				return err
			}
			r.Close()
		default:
			return errInvalidCompressionMode
		}
		b := buf.Bytes()
		if isNullTerminated && len(b) > 0 && b[len(b)-1] == 0x0 {
			b = b[:len(b)-1]
		}
		*data = b

		return nil
	case EncodeMode:
		b := *data
		if isNullTerminated {
			b = append(b, 0x0)
		}
		switch compressionMode {
		case ZlibCompression:
			l64 := uint64(len(b))
			if err := binary.Write(s.w, binary.LittleEndian, l64); err != nil {
				return fmt.Errorf("write error: %w", err)
			}
			w := zlib.NewWriter(s.w)
			if _, err = w.Write(b); err != nil {
				return err
			}
			w.Close()
		case ZstdCompression:
			l32 := uint32(len(b))
			if err := binary.Write(s.w, binary.LittleEndian, l32); err != nil {
				return fmt.Errorf("write error: %w", err)
			}
			w, err := zstd.NewWriter(s.w, zstd.WithEncoderLevel(1))
			if err != nil {
				return err
			}
			if _, err = w.Write(b); err != nil {
				return err
			}
			w.Close()
		case NoCompression:
			if _, err := s.w.Write(b); err != nil {
				return err
			}
		default:
			return errInvalidCompressionMode
		}
	default:
		return errors.New("StreamJson: invalid mode")
	}
	return nil
}

func GetRandomBytes(l int) []byte {
	b := make([]byte, l)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("error generating random bytes: %v", err)
	}
	return b
}
