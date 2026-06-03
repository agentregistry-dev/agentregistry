package migrate

import (
	"bytes"
	"testing"
)

// TestStatusJSONShape locks the `arctl db migrate status -o json` wire
// format. Operators consume it from CI/CD via `jq`, so a renamed or
// retyped field is a breaking change — this golden assertion fails the
// moment the field set, names, types, or order drift. If you intend the
// change, bump the CLI major version and update this fixture deliberately.
func TestStatusJSONShape(t *testing.T) {
	var buf bytes.Buffer
	lines := []lineRow{{
		src:       Source{Name: "oss"},
		applied:   3,
		pending:   1,
		dbVersion: 3,
		dirty:     true,
	}}
	if err := writeStatusJSON(&buf, lines, 3, 1); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}

	want := `{
  "applied": 3,
  "pending": 1,
  "sources": [
    {
      "name": "oss",
      "applied": 3,
      "pending": 1,
      "version": 3,
      "downgraded": false,
      "dirty": true
    }
  ]
}
`
	if got := buf.String(); got != want {
		t.Errorf("status JSON shape changed (breaking for `jq` consumers).\n got:\n%s\nwant:\n%s", got, want)
	}
}
