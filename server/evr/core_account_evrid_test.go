package evr

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/google/go-cmp/cmp"
)

func TestEvrId_UUID(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	tests := []struct {
		name   string
		fields fields
		want   uuid.UUID
	}{
		{
			name: "valid UUID",
			fields: fields{
				PlatformCode: 3,
				AccountId:    123412341234,
			},
			want: uuid.FromStringOrNil("0348ff29-e6a7-5fea-80cb-0f3834801a74"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xpi := &EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			if got := xpi.UUID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestEvrId_Equal(t *testing.T) {

	evrID1 := EvrId{
		PlatformCode: 1,
		AccountId:    1,
	}
	evrID2 := EvrId{
		PlatformCode: 1,
		AccountId:    1,
	}

	if evrID1 != evrID2 {
		t.Errorf("EvrId.Equal() = %v, want %v", evrID1, evrID2)
	}
}

func TestEvrId_Equals(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	type args struct {
		other EvrId
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 1,
				AccountId:    1,
			},
			args: args{
				EvrId{
					PlatformCode: 1,
					AccountId:    1,
				},
			},
			want: true,
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountId:    1,
			},
			args: args{
				EvrId{
					PlatformCode: 1,
					AccountId:    1,
				},
			},
			want: false,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 1,
				AccountId:    0,
			},
			args: args{
				EvrId{
					PlatformCode: 1,
					AccountId:    1,
				},
			},
			want: false,
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 1,
				AccountId:    1,
			},
			args: args{
				EvrId{
					PlatformCode: 2,
					AccountId:    2,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xpi := &EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			if got := xpi.Equals(tt.args.other); got != tt.want {
				t.Errorf("EvrId.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrId_IsNil(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 0,
				AccountId:    0,
			},
			want: true,
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountId:    1,
			},
			want: false,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 1,
				AccountId:    0,
			},
			want: false,
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 1,
				AccountId:    1,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xpi := &EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			if got := xpi.IsNil(); got != tt.want {
				t.Errorf("EvrId.IsNil() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrId_MarshalText(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 0,
				AccountId:    1,
			},
			want:    []byte("STM-1"),
			wantErr: false,
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 55,
				AccountId:    1,
			},
			want:    []byte("UNK-1"),
			wantErr: false,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 4,
				AccountId:    0,
			},
			want:    []byte("OVR-0"),
			wantErr: false,
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 0,
				AccountId:    0,
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			got, err := e.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("EvrId.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrId.MarshalText() = `%v`, want `%v`", string(got), string(tt.want))
			}
		})
	}
}

func TestEvrId_UnmarshalJSON(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	type args struct {
		b []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 1,
				AccountId:    1,
			},
			args: args{
				b: []byte(`"STM-1"`),
			},
			wantErr: false,
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountId:    1,
			},
			args: args{
				b: []byte(`"UNK-1"`),
			},
			wantErr: false,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 1,
				AccountId:    0,
			},
			args: args{
				b: []byte(`"STM-0"`),
			},
			wantErr: false,
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 0,
				AccountId:    0,
			},
			args: args{
				b: []byte(`""`),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			if err := json.Unmarshal(tt.args.b, e); (err != nil) != tt.wantErr {
				t.Errorf("EvrId.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvrId_MarshalBinary(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 4,
				AccountId:    3963667097037078,
			},
			want: []byte{0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x16, 0xa9, 0x53, 0x29, 0xef, 0x14, 0x0e, 0x00},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			got, err := e.MarshalBinary()
			if (err != nil) != tt.wantErr {
				t.Errorf("EvrId.MarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrId.MarshalBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrId_UnmarshalBinary(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 4,
				AccountId:    3963667097037078,
			},
			args: args{
				data: []byte{0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x16, 0xa9, 0x53, 0x29, 0xef, 0x14, 0x0e, 0x00},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			if err := e.UnmarshalBinary(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("EvrId.UnmarshalBinary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if e.PlatformCode != tt.fields.PlatformCode {
				t.Errorf("EvrId.UnmarshalBinary() PlatformCode = %v, want %v", e.PlatformCode, tt.fields.PlatformCode)
			}
			if e.AccountId != tt.fields.AccountId {
				t.Errorf("EvrId.UnmarshalBinary() AccountId = %v, want %v", e.AccountId, tt.fields.AccountId)
			}

		})
	}
}

func TestEvrId_MarshalJSON(t *testing.T) {
	type fields struct {
		PlatformCode PlatformCode
		AccountId    uint64
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 3,
				AccountId:    3963667097037078,
			},
			want:    []byte(`"OVR-ORG-3963667097037078"`),
			wantErr: false,
		},
		{
			name: "nil",
			fields: fields{
				PlatformCode: 0,
				AccountId:    0,
			},
			want:    []byte("null"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := EvrId{
				PlatformCode: tt.fields.PlatformCode,
				AccountId:    tt.fields.AccountId,
			}
			got, err := e.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("EvrId.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("(- want, + got): %s", cmp.Diff(string(tt.want), string(got)))
			}
		})
	}
}
