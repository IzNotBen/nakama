package server

import (
	"reflect"
	"testing"

	"github.com/gofrs/uuid/v5"
)

func TestNewMatchToken(t *testing.T) {
	type args struct {
		id   uuid.UUID
		node string
	}
	tests := []struct {
		name    string
		args    args
		want    MatchID
		wantErr bool
	}{
		{
			name: "Match token created successfully",
			args: args{
				id:   uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			wantErr: false,
		},
		{
			name: "Match token creation failed due to invalid ID",
			args: args{
				id:   uuid.Nil,
				node: "node",
			},
			want: MatchID{
				node: "node",
			},
			wantErr: true,
		},
		{
			name: "Match token creation failed due to empty node",
			args: args{
				id:   uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "",
			},
			want: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewMatchToken(tt.args.id, tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMatchToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NewMatchToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_String(t *testing.T) {
	tests := []struct {
		name string
		m    MatchID
		want string
	}{
		{
			name: "Match token stringified successfully",
			m: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: "a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e.node",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.String(); got != tt.want {
				t.Errorf("MatchToken.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_ID(t *testing.T) {
	tests := []struct {
		name string
		tr   MatchID
		want uuid.UUID
	}{
		{
			name: "Match token ID extracted successfully",
			tr: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tr.uuid; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MatchToken.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_Node(t *testing.T) {
	tests := []struct {
		name string
		tr   MatchID
		want string
	}{
		{
			name: "Match token node extracted successfully",
			tr: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: "node",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tr.node; got != tt.want {
				t.Errorf("MatchToken.Node() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_IsValid(t *testing.T) {
	tests := []struct {
		name string
		tr   MatchID
		want bool
	}{
		{
			name: "Match token is valid",
			tr: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: true,
		},
		{
			name: "empty token is invalid",
			tr:   MatchID{},
			want: false,
		},
		{
			name: "Match token without seperator is invalid",
			tr: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: false,
		},
		{
			name: "Match token without empty node is invalid",
			tr: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			want: false,
		},
		{
			name: "Match token with empty id is invalid",
			tr: MatchID{
				node: "node",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tr.IsValid(); got != tt.want {
				t.Errorf("MatchToken.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_UnmarshalText(t *testing.T) {
	type args struct {
		data []byte
	}
	tests := []struct {
		name    string
		tr      MatchID
		args    args
		wantErr bool
	}{
		{
			name: "Match token unmarshalled successfully",
			tr: MatchID{
				uuid: uuid.Nil,
				node: "",
			},
			args:    args{data: []byte(`a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e.node`)},
			wantErr: false,
		},
		{
			name: "Match token unmarshalling failed",
			tr: MatchID{
				uuid: uuid.Nil,
				node: "",
			},
			args:    args{data: []byte(`a3d5f9e4-6a3ddd-4b8e-9d98-2d0e8e9f5a3e.node`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.tr.UnmarshalText(tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("MatchToken.UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMatchTokenFromString(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		wantT   MatchID
		wantErr bool
	}{
		{
			name: "valid match token is successful",
			args: args{s: "a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e.node"},
			wantT: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
			wantErr: false,
		},
		{
			name:    "empty string is successful",
			args:    args{s: ""},
			wantT:   MatchID{},
			wantErr: false,
		},
		{
			name:    "failed due to empty node",
			args:    args{s: "a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e."},
			wantT:   MatchID{},
			wantErr: true,
		},
		{
			name:    "failed due to empty id",
			args:    args{s: ".node"},
			wantT:   MatchID{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotT, err := MatchTokenFromString(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("MatchTokenFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotT != tt.wantT {
				t.Errorf("MatchTokenFromString() = %v, want %v", gotT, tt.wantT)
			}
		})
	}
}

func TestMatchTokenFromStringOrNil(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want MatchID
	}{
		{
			name: "Match token created successfully",
			args: args{s: "a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e.node"},
			want: MatchID{
				uuid: uuid.FromStringOrNil("a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e"),
				node: "node",
			},
		},
		{
			name: "Match token creation failed due to invalid token",
			args: args{s: "a3d5f9e4-6a3d-4b8e-9d98-2d0e8e9f5a3e."},
			want: MatchID{},
		},
		{
			name: "Match token creation failed due to invalid token",
			args: args{s: ".node"},
			want: MatchID{},
		},
		{
			name: "Match token creation failed due to invalid token",
			args: args{s: ""},
			want: MatchID{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchIDFromStringOrNil(tt.args.s); got != tt.want {
				t.Errorf("MatchTokenFromStringOrNil() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchToken_Nil(t *testing.T) {
	tests := []struct {
		name string
		m    MatchID
		want MatchID
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.Nil(); got != tt.want {
				t.Errorf("MatchToken.Nil() = %v, want %v", got, tt.want)
			}
		})
	}
}
