// This file was generated from JSON Schema using quicktype, do not modify it directly.
// To parse and unparse this JSON data, add this code to your project and do:
//
//    gameProfiles, err := UnmarshalGameProfiles(bytes)
//    bytes, err = gameProfiles.Marshal()

package evr

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDeveloperFeatures_Omitted_When_Empty(t *testing.T) {

	type testProfile struct {
		BeforeDev         string            `json:"before_dev,omitempty"`
		DeveloperFeatures DeveloperFeatures `json:"dev,omitempty"`
		AfterDev          string            `json:"after_dev,omitempty"`
	}

	profile := testProfile{
		BeforeDev: "before",
		DeveloperFeatures: DeveloperFeatures{
			DisableAfkTimeout: false,
			EvrIDOverride: EvrId{
				PlatformCode: 0,
				AccountId:    0,
			},
		},
		AfterDev: "after",
	}

	got, err := json.Marshal(profile)
	if err != nil {
		t.Errorf("DeveloperFeatures.MarshalJSON() error = %v", err)
		return
	}

	want := `{"before_dev":"before","after_dev":"after"}`
	var wantErr error = nil
	if err != wantErr {
		t.Errorf("DeveloperFeatures.MarshalJSON() error = %v, wantErr %v", err, wantErr)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeveloperFeatures.MarshalJSON() = `%v`, want `%v`", string(got), string(want))
	}

}
