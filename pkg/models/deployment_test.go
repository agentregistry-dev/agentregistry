package models

import "testing"

func TestUnmarshalFrom_NilInputReturnsEmptyJSONObject(t *testing.T) {
	got, err := UnmarshalFrom(nil)
	if err != nil {
		t.Fatalf("UnmarshalFrom(nil) returned error: %v", err)
	}
	if got == nil {
		t.Fatal("UnmarshalFrom(nil) returned nil map")
	}
	if len(got) != 0 {
		t.Fatalf("UnmarshalFrom(nil) returned unexpected values: %#v", got)
	}
}

func TestUnmarshalFrom_StructProducesObjectMap(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	got, err := UnmarshalFrom(payload{Name: "weather", Count: 3})
	if err != nil {
		t.Fatalf("UnmarshalFrom(struct) returned error: %v", err)
	}
	if got == nil {
		t.Fatal("UnmarshalFrom(struct) returned nil map")
	}
	if got["name"] != "weather" {
		t.Fatalf("unexpected name: %#v", got["name"])
	}
	if got["count"] != float64(3) {
		t.Fatalf("unexpected count: %#v", got["count"])
	}
}

func TestJSONObjectUnmarshalInto_StructRoundTrip(t *testing.T) {
	input := JSONObject{
		"platform": "kubernetes",
		"enabled":  true,
	}

	var out struct {
		Platform string `json:"platform"`
		Enabled  bool   `json:"enabled"`
	}
	if err := input.UnmarshalInto(&out); err != nil {
		t.Fatalf("UnmarshalInto() returned error: %v", err)
	}
	if out.Platform != "kubernetes" || !out.Enabled {
		t.Fatalf("unexpected decoded struct: %#v", out)
	}
}
