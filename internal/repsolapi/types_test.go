package repsolapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// decodeFile reads a JSON fixture and unmarshals it into T.
func decodeFile[T any](t *testing.T, path string) T {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	return v
}

// mustContain fails when the JSON text lacks any of the wanted substrings.
func mustContain(t *testing.T, b []byte, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON %s does not contain %s", b, want)
		}
	}
}

// mustNotContain fails when the JSON text contains any of the unwanted
// substrings.
func mustNotContain(t *testing.T, b []byte, unwanted ...string) {
	t.Helper()
	for _, want := range unwanted {
		if strings.Contains(string(b), want) {
			t.Errorf("JSON %s unexpectedly contains %s", b, want)
		}
	}
}

func TestAddressJSONWireTypes(t *testing.T) {
	geo := decodeFile[Address](t, "testdata/geocode.json")
	b, err := json.Marshal(geo)
	if err != nil {
		t.Fatalf("marshal geocode address: %v", err)
	}
	mustContain(t, b,
		`"latitude":40.4168`,
		`"confidence":{"number_not_found":false`,
		`"currentFullAddress":""`,
	)

	rep := decodeFile[struct {
		Results []Address `json:"results"`
	}](t, "testdata/autocomplete.json").Results[0]
	if rep.Origin != "repsol" {
		t.Fatalf("first autocomplete origin = %q, want repsol", rep.Origin)
	}
	b, err = json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal autocomplete address: %v", err)
	}
	mustContain(t, b, `"latitude":""`, `"neutral":false`)
	mustNotContain(t, b, `"confidence"`, `"currentStreet"`)
}

func TestAddressTOMLRoundTrip(t *testing.T) {
	cases := map[string]Address{
		"geocode": decodeFile[Address](t, "testdata/geocode.json"),
		"autocomplete": decodeFile[struct {
			Results []Address `json:"results"`
		}](t, "testdata/autocomplete.json").Results[0],
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := toml.Marshal(want)
			if err != nil {
				t.Fatalf("toml.Marshal: %v", err)
			}
			var got Address
			if err := toml.Unmarshal(b, &got); err != nil {
				t.Fatalf("toml.Unmarshal: %v\n%s", err, b)
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("round trip mismatch:\nwant %#v\ngot  %#v\ntoml:\n%s", want, got, b)
			}
		})
	}
}
