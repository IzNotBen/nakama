package evr

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/google/go-cmp/cmp"
)

func TestGUID_String(t *testing.T) {
	tests := []struct {
		name string
		g    GUID
		want string
	}{
		{
			name: "valid GUID",
			g:    GUID(uuid.FromStringOrNil("01020304-0506-0708-090a-0A0b0C0D0E0F")),
			want: "01020304-0506-0708-090A-0A0B0C0D0E0F",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.g.String(); got != tt.want {
				t.Errorf("GUID.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGUID_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		g       GUID
		want    []byte
		wantErr bool
	}{
		{
			name:    "valid GUID",
			g:       GUID(uuid.FromStringOrNil("01020304-0506-0708-090A-0A0B0C0D0E0F")),
			want:    []byte("01020304-0506-0708-090A-0A0B0C0D0E0F"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.g.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("GUID.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GUID.MarshalText() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGUID_UnmarshalText(t *testing.T) {
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		g       *GUID
		args    args
		wantErr bool
	}{
		{
			name:    "valid GUID",
			g:       &GUID{},
			args:    args{data: []byte("01020304-0506-0708-090A-0A0B0C0D0E0F")},
			wantErr: false,
		},
		{
			name:    "invalid GUID",
			g:       &GUID{},
			args:    args{data: []byte("01020304-0506-0708-090A-0A0B0C0D0E0")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.g.UnmarshalText(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("GUID.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGUID_UnmarshalBinary(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		want    GUID
		wantErr bool
	}{
		{
			name:    "valid bytes",
			b:       []byte{4, 3, 2, 1, 6, 5, 8, 7, 9, 10, 10, 11, 12, 13, 14, 15},
			want:    GUID(uuid.FromStringOrNil("01020304-0506-0708-090A-0A0B0C0D0E0F")),
			wantErr: false,
		},
		{
			name:    "invalid bytes",
			b:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
			want:    GUID{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &GUID{}
			err := got.UnmarshalBinary(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("GUID.UnmarshalBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !slices.Equal(got[:], tt.want[:]) {
				t.Errorf("GUID.UnmarshalBytes() (- want / + got) = %s", cmp.Diff([]byte(tt.want[:]), []byte(got[:])))
			}
		})
	}
}

func TestGUID_MarshalBinary(t *testing.T) {
	tests := []struct {
		name    string
		g       GUID
		wantB   []byte
		wantErr bool
	}{
		{
			name:    "valid GUID",
			g:       GUID(uuid.FromStringOrNil("01020304-0506-0708-090A-0A0B0C0D0E0F")),
			wantB:   []byte{4, 3, 2, 1, 6, 5, 8, 7, 9, 10, 10, 11, 12, 13, 14, 15},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotB, err := tt.g.MarshalBinary()
			if (err != nil) != tt.wantErr {
				t.Errorf("GUID.MarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotB, tt.wantB) {
				t.Errorf("GUID.MarshalBinary() = %v, want %v", gotB, tt.wantB)
			}
		})
	}
}
