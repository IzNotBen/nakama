package evr

import (
	"reflect"
	"testing"

	"github.com/gofrs/uuid/v5"
)

func TestEvrID_UUID(t *testing.T) {
	type fields struct {
		PlatformCode uint64
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
				PlatformCode: 1,
				AccountId:    1,
			},
			want: uuid.FromStringOrNil("496d8944-6159-5c53-bdc8-1cab22f9d28d"),
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountId:    12341234,
			},
			want: uuid.Nil,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 4,
				AccountId:    0,
			},
			want: uuid.Nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xpi := &EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountId,
			}
			if got := xpi.UUID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrID.UUID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrID_Equal(t *testing.T) {

	evrID1 := EvrID{
		PlatformCode: 1,
		AccountID:    1,
	}
	evrID2 := EvrID{
		PlatformCode: 1,
		AccountID:    1,
	}

	if evrID1 != evrID2 {
		t.Errorf("EvrID.Equal() = %v, want %v", evrID1, evrID2)
	}
}

func TestEvrID_Equals(t *testing.T) {
	type fields struct {
		PlatformCode uint64
		AccountId    uint64
	}
	type args struct {
		other EvrID
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
				EvrID{
					PlatformCode: 1,
					AccountID:    1,
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
				EvrID{
					PlatformCode: 1,
					AccountID:    1,
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
				EvrID{
					PlatformCode: 1,
					AccountID:    1,
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
				EvrID{
					PlatformCode: 2,
					AccountID:    2,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xpi := &EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountId,
			}
			if got := xpi.Equals(tt.args.other); got != tt.want {
				t.Errorf("EvrID.Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrID_IsNil(t *testing.T) {
	type fields struct {
		PlatformCode uint64
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
			xpi := &EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountId,
			}
			if got := xpi.IsNil(); got != tt.want {
				t.Errorf("EvrID.IsNil() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrID_MarshalText(t *testing.T) {
	type fields struct {
		PlatformCode uint64
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
				PlatformCode: 1,
				AccountId:    1,
			},
			want:    []byte("STM-1"),
			wantErr: false,
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountId:    1,
			},
			want:    []byte("UNK-1"),
			wantErr: false,
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 1,
				AccountId:    0,
			},
			want:    []byte("STM-0"),
			wantErr: false,
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 0,
				AccountId:    0,
			},
			want:    []byte(""),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountId,
			}
			got, err := e.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("EvrID.MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrID.MarshalText() = `%v`, want `%v`", string(got), string(tt.want))
			}
		})
	}
}

func TestEvrID_UnmarshalText(t *testing.T) {
	type fields struct {
		PlatformCode uint64
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
				b: []byte("STM-1"),
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
				b: []byte("UNK-1"),
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
				b: []byte("STM-0"),
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
				b: []byte(""),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountId,
			}
			if err := e.UnmarshalText(tt.args.b); (err != nil) != tt.wantErr {
				t.Errorf("EvrID.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvrIDFromString(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    *EvrID
		wantErr bool
	}{
		{
			name: "valid",
			args: args{
				s: "STM-1",
			},
			want: &EvrID{
				PlatformCode: 1,
				AccountID:    1,
			},
			wantErr: false,
		},
		{
			name: "invalid PlatformCode",
			args: args{
				s: "UNK-1",
			},
			want: &EvrID{
				PlatformCode: 0,
				AccountID:    1,
			},
			wantErr: false,
		},
		{
			name: "invalid AccountId",
			args: args{
				s: "STM-0",
			},
			want: &EvrID{
				PlatformCode: 1,
				AccountID:    0,
			},
			wantErr: false,
		},
		{
			name: "OVR_ORG-3963667097037078",
			args: args{
				s: "OVR_ORG-3963667097037078",
			},
			want: &EvrID{
				PlatformCode: 4,
				AccountID:    3963667097037078,
			},
			wantErr: false,
		},
		{
			name: "OVR-ORG-3963667097037078",
			args: args{
				s: "OVR-ORG-3963667097037078",
			},
			want: &EvrID{
				PlatformCode: 4,
				AccountID:    3963667097037078,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvrIDFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvrIDFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrIDFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrIDFromStringOrNil(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want EvrID
	}{
		{
			name: "valid",
			args: args{
				s: "STM-1",
			},
			want: EvrID{
				PlatformCode: 1,
				AccountID:    1,
			},
		},
		{
			name: "invalid PlatformCode",
			args: args{
				s: "UNK-1",
			},
			want: EvrID{
				PlatformCode: 0,
				AccountID:    0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EvrIDFromStringOrNil(tt.args.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EvrIDFromStringOrNil() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvrID_String(t *testing.T) {
	type fields struct {
		PlatformCode uint64
		AccountID    uint64
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "valid",
			fields: fields{
				PlatformCode: 1,
				AccountID:    1,
			},
			want: "STM-1",
		},
		{
			name: "invalid PlatformCode",
			fields: fields{
				PlatformCode: 0,
				AccountID:    1,
			},
			want: "UNK-1",
		},
		{
			name: "invalid AccountId",
			fields: fields{
				PlatformCode: 1,
				AccountID:    0,
			},
			want: "STM-0",
		},
		{
			name: "invalid",
			fields: fields{
				PlatformCode: 0,
				AccountID:    0,
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := EvrID{
				PlatformCode: tt.fields.PlatformCode,
				AccountID:    tt.fields.AccountID,
			}
			if got := id.String(); got != tt.want {
				t.Errorf("EvrID.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
