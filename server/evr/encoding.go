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
	"github.com/gofrs/uuid/v5"
	"github.com/klauspost/compress/zstd"
)

type StreamMode int
type CompressionMode int

const (
	NoCompression = iota
	ZlibCompression
	ZstdCompression
)

var (
	ErrInvalidCompressionMode = errors.New("invalid compression mode")
	ErrInvalidMode            = errors.New("invalid mode")
)

type Serializer interface {
	Stream(s *Stream) error
}

type Stream struct {
	r *bytes.Reader
	w *bytes.Buffer
}

func NewStream(b []byte) *Stream {
	if b != nil {
		r := bytes.NewReader(b)
		return &Stream{
			r: r,
		}
	}
	w := bytes.NewBuffer(nil)
	return &Stream{
		w: w,
	}
}

func (s *Stream) Bytes() []byte {
	if s.r != nil {
		b := make([]byte, s.r.Len())
		s.r.Read(b)
		return b
	}
	return s.w.Bytes()
}

func (s *Stream) Len() int {
	if s.r != nil {
		return s.r.Len()
	}
	return s.w.Len()
}

func (s *Stream) Position() int {
	if s.r != nil {
		pos, _ := s.r.Seek(0, 1) // Current position
		return int(pos)
	}
	return s.w.Len()
}

func (s *Stream) SetPosition(pos int) error {
	if s.r != nil {
		if _, err := s.r.Seek(int64(pos), 0); err != nil {
			return fmt.Errorf("failed to set position: %w", err)
		}
		return nil
	}
	if pos > s.w.Len() {
		return fmt.Errorf("position out of range: %d", pos)
	}
	s.w.Truncate(pos)
	return nil
}

func (s *Stream) Reset() {
	if s.r != nil {
		s.r.Reset(s.Bytes())
	} else {
		s.w.Reset()
	}
}

func (s *Stream) Skip(count int) error {
	if s.r != nil {
		_, err := s.r.Seek(int64(count), io.SeekCurrent)
		return err
	}
	_, err := s.w.Write(make([]byte, count))
	return err
}

func (s *Stream) StreamBE(value any) error {
	if s.r != nil {
		return binary.Read(s.r, binary.BigEndian, value)
	}
	return binary.Write(s.w, binary.BigEndian, value)
}

func (s *Stream) StreamLE(value any) error {
	if s.r != nil {
		return binary.Read(s.r, binary.LittleEndian, value)
	}
	return binary.Write(s.w, binary.LittleEndian, value)
}

func (s *Stream) StreamIP(data *net.IP) error {
	b := make([]byte, net.IPv4len)
	copy(b, (*data).To4())
	if err := s.StreamBytes(&b, net.IPv4len); err != nil {
		return err
	}
	*data = net.IP(b)
	return nil
}

func (s *Stream) StreamByte(value *byte) (err error) {
	if s.r != nil {
		*value, err = s.r.ReadByte()
		return err
	}
	return s.w.WriteByte(*value)
}

func (s *Stream) StreamBytes(dst *[]byte, l int) (err error) {
	if s.r != nil {
		switch l {
		case 0:
			return nil
		case -1:
			*dst, err = io.ReadAll(s.r)
		default:
			*dst = make([]byte, l)
			_, err = s.r.Read(*dst)
		}
	} else {
		_, err = s.w.Write(*dst)
	}
	return err
}

func (s *Stream) StreamString(v *string, length int) (err error) {
	b := make([]byte, length)
	if s.r != nil {
		_, err = s.r.Read(b)
		*v = string(bytes.TrimRight(b, "\x00"))
	} else {
		copy(b, []byte(*v))
		err = s.StreamBytes(&b, len(b))
	}
	return err
}

func (s *Stream) StreamNullTerminatedString(str *string) error {
	b := make([]byte, 0, len(*str))
	if s.r != nil {
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
	}
	bytes := []byte(*str)
	bytes = append(bytes, 0)
	return s.StreamBytes(&bytes, len(bytes))
}

type Streamer any

func (s *Stream) Stream(v any) (err error) {
	// Check if this is a restruct Unpacker or Packer
	switch t := v.(type) {
	case Serializer:
		return t.Stream(s)
	case []Serializer:
		for _, t := range t {
			if err = t.Stream(s); err != nil {
				return err
			}
		}
		return nil
	case []Streamer:
		for _, v := range t {
			if err = s.Stream(v); err != nil {
				return err
			}
		}
		return nil
	case *Symbol:
		return s.StreamLE(t)
	case *int:
		return s.StreamLE(t)
	case *int8:
		return s.StreamLE(t)
	case *uint8:
		return s.StreamLE(t)
	case *int16:
		return s.StreamLE(t)
	case *uint16:
		return s.StreamLE(t)
	case *int32:
		return s.StreamLE(t)
	case *uint32:
		return s.StreamLE(t)
	case *int64:
		return s.StreamLE(t)
	case *uint64:
		return s.StreamLE(t)
	case *[]byte:
		return s.StreamBytes(t, -1)
	case *string:
		return s.StreamString(t, len(*t))
	case *GUID:
		return s.StreamGUID(t)
	case *uuid.UUID:
		return s.StreamUUID(t)
	case []GUID:
		return s.StreamGUIDs(t)
	case *[]GUID:
		return s.StreamGUIDs(*t)
	case *net.IP:
		return s.StreamIP(t)
	default:
		return fmt.Errorf("Stream: unsupported type: %T", v)
	}
}

// Microsoft's GUID has some bytes re-ordered.
func (s *Stream) StreamGUID(guid *GUID) (err error) {
	b := make([]byte, 16)
	if s.r != nil {
		if err = s.StreamBytes(&b, 16); err != nil {
			return err
		}

		if err = guid.UnmarshalBinary(b); err != nil {
			return err
		}
		return nil
	}
	if b, err = guid.MarshalBinary(); err != nil {
		return err
	}
	return s.StreamBytes(&b, 16)
}

// Microsoft's GUID has some bytes re-ordered.
func (s *Stream) StreamUUID(id *uuid.UUID) (err error) {
	b := make([]byte, 16)
	if s.r != nil {
		if err = s.StreamBytes(&b, 16); err != nil {
			return err
		}

		if err = id.UnmarshalBinary(b); err != nil {
			return err
		}
		return nil
	}
	if b, err = id.MarshalBinary(); err != nil {
		return err
	}
	return s.StreamBytes(&b, 16)
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

func (s *Stream) StreamStringTable(entries *[]string) error {
	var err error
	var strings []string
	logCount := uint64(len(*entries))
	if err = s.Stream(&logCount); err != nil {
		return err
	}
	if s.r != nil {
		strings = make([]string, logCount)
		offsets := make([]uint32, logCount)
		offsets[0] = 0
		for i := 1; i < int(logCount); i++ {
			off := uint32(0)
			if err = s.Stream(&off); err != nil {
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
		return nil
	}

	// write the offsets
	for i := 0; i < int(logCount-1); i++ {
		off := uint32(len((*entries)[i]) + 1)
		if err = s.Stream(&off); err != nil {
			return err
		}
	}
	// write the strings
	for _, str := range *entries {
		if s.StreamNullTerminatedString(&str); err != nil {
			return err
		}
	}
	return nil
}

func (s *Stream) StreamJSON(data interface{}, isNullTerminated bool, compressionMode CompressionMode) error {
	var buf bytes.Buffer
	var err error
	if s.r != nil {

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
			io.Copy(&buf, r)
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
			io.Copy(&buf, r)
			r.Close()
		default:
			return ErrInvalidCompressionMode
		}
		if isNullTerminated && buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == 0x0 {
			buf.Truncate(buf.Len() - 1)
		}

		if err = json.Unmarshal(buf.Bytes(), &data); err != nil {
			return fmt.Errorf("unmarshal json error: %w: %s", err, buf.Bytes())
		}
		return nil
	}

	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal JSON error: %w", err)
	}
	if isNullTerminated {
		b = append(b, 0x0)
	}
	switch compressionMode {
	case ZlibCompression:
		if err := binary.Write(s.w, binary.LittleEndian, uint64(len(b))); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		w := zlib.NewWriter(s.w)
		if _, err = w.Write(b); err != nil {
			return err
		}
		w.Close()
	case ZstdCompression:
		if err := binary.Write(s.w, binary.LittleEndian, uint32(len(b))); err != nil {
			return fmt.Errorf("write error: %w", err)
		}
		w, err := zstd.NewWriter(s.w)
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
		return ErrInvalidCompressionMode
	}

	return nil
}

func (s *Stream) StreamCompressedBytes(data *[]byte, isNullTerminated bool, compressionMode CompressionMode) error {
	buf := bytes.Buffer{}
	var err error
	if s.r != nil {
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
			return ErrInvalidCompressionMode
		}
		b := buf.Bytes()
		if isNullTerminated && len(b) > 0 && b[len(b)-1] == 0x0 {
			b = b[:len(b)-1]
		}
		*data = b
		return nil
	}

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
		return ErrInvalidCompressionMode
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

func RunErrorFunctions(funcs []func() error) (err error) {
	for _, fn := range funcs {
		if err = fn(); err != nil {
			return err
		}
	}
	return nil
}

func StringifyStruct(s interface{}) string {
	return spew.Sdump(s)
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
