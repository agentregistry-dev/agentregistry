//go:build unit

package v1alpha1store

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"
)

// TestRebaseSQLPlaceholders_Examples is a small table of hand-picked
// fragments. It documents the contract by example before the fuzz
// driver explores the wider input space.
func TestRebaseSQLPlaceholders_Examples(t *testing.T) {
	cases := []struct {
		name   string
		clause string
		offset int
		want   string
	}{
		{"empty clause is a no-op", "", 5, ""},
		{"zero offset is a no-op", "name = $1 AND ns = $2", 0, "name = $1 AND ns = $2"},
		{"single placeholder shifts by offset", "name = $1", 3, "name = $4"},
		{"multiple placeholders all shift", "name = $1 AND ns = $2", 4, "name = $5 AND ns = $6"},
		{"repeated placeholder shifts every occurrence", "name = $1 OR alias = $1", 2, "name = $3 OR alias = $3"},
		{"out-of-order placeholders preserve relative order", "tenant = $2 AND name = $1", 10, "tenant = $12 AND name = $11"},
		{"surrounding text untouched", "  AND  $1::jsonb @> $2  ", 7, "  AND  $8::jsonb @> $9  "},
		{"$0 is rebased — postgres rejects $0 anyway", "x = $0", 4, "x = $4"},
		{"high placeholder still works", "x = $99", 1, "x = $100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rebaseSQLPlaceholders(tc.clause, tc.offset)
			if got != tc.want {
				t.Fatalf("rebaseSQLPlaceholders(%q, %d) = %q, want %q",
					tc.clause, tc.offset, got, tc.want)
			}
		})
	}
}

// FuzzRebaseSQLPlaceholders pins the rebase contract documented on
// rebaseSQLPlaceholders: for any clause + offset, the rebased output
// has every `$N` shifted by exactly `offset`, the count of placeholder
// tokens is preserved, and the relative ordering of placeholders is
// preserved.
//
// Why fuzzing matters here: ExtraWhere is the authz seam — enterprise
// builds wire RBAC predicates through it, so silent reordering or
// dropped placeholders would be a privilege-escalation bug, not just a
// SQL parse error. The fuzz driver is cheap insurance against the
// regex pass mis-handling adversarial input shapes (very large N,
// unicode digits, $$, dollar-quoted strings, `$N` glued to other
// digits, etc.) before they ship to a real authz boundary.
func FuzzRebaseSQLPlaceholders(f *testing.F) {
	// Seed corpus covers the documented examples above plus a few
	// edge cases the regex pass is known to be lax about — the fuzzer
	// then mutates from this base.
	for _, clause := range []string{
		"",
		"name = $1",
		"name = $1 AND ns = $2",
		"$1::jsonb @> $2",
		"x = $1 OR x = $1",
		"name LIKE '$1%'", // string literal containing $1 — rebaser will rewrite (documented limitation)
		"$$dollar quoted$$",
		"$0",
		"$999999",
		"plain text no placeholders",
		"$1 $2 $3 $4 $5",
		"$2 $1",
	} {
		for _, offset := range []int{0, 1, 5, 100} {
			f.Add(clause, offset)
		}
	}

	f.Fuzz(func(t *testing.T, clause string, offset int) {
		// Constrain the fuzz to the production contract: offset is
		// always ≥ 0 (it's len(args) - len(extraArgs) after a slice
		// append, never negative). Skip overflow-prone values too —
		// the property under test is the rebase behavior, not int
		// arithmetic limits in the format string.
		if offset < 0 || offset > 1_000_000 {
			t.Skip()
		}

		got := rebaseSQLPlaceholders(clause, offset)

		// Property 1: no-op cases short-circuit unchanged.
		if clause == "" || offset == 0 {
			if got != clause {
				t.Fatalf("no-op case: rebaseSQLPlaceholders(%q, %d) = %q, want %q",
					clause, offset, got, clause)
			}
			return
		}

		// Extract original placeholder positions and values.
		origMatches := sqlPlaceholderPattern.FindAllStringSubmatchIndex(clause, -1)
		gotMatches := sqlPlaceholderPattern.FindAllStringSubmatchIndex(got, -1)

		// Property 2: count of placeholder tokens is preserved.
		if len(origMatches) != len(gotMatches) {
			t.Fatalf("placeholder count changed: orig=%d in %q, got=%d in %q",
				len(origMatches), clause, len(gotMatches), got)
		}

		// Property 3: each placeholder shifts by exactly `offset`, and
		// the surrounding non-placeholder text is byte-identical
		// between input and output (regex replacement only touches
		// matched spans).
		assertSurroundingTextIdentical(t, clause, got, origMatches, gotMatches)

		for i, m := range origMatches {
			origN, err := strconv.Atoi(clause[m[2]:m[3]])
			if err != nil {
				// The regex matched but Atoi failed — the rebase
				// path leaves the token in place; assert that.
				gm := gotMatches[i]
				if got[gm[2]:gm[3]] != clause[m[2]:m[3]] {
					t.Fatalf("Atoi-fail token should be untouched: got %q from %q",
						got[gm[0]:gm[1]], clause[m[0]:m[1]])
				}
				continue
			}
			gm := gotMatches[i]
			gotN, err := strconv.Atoi(got[gm[2]:gm[3]])
			if err != nil {
				t.Fatalf("rebased token %q is not a number", got[gm[0]:gm[1]])
			}
			if gotN != origN+offset {
				t.Fatalf("token %d: expected $%d (orig %d + offset %d), got $%d in %q",
					i, origN+offset, origN, offset, gotN, got)
			}
		}
	})
}

// assertSurroundingTextIdentical compares the non-placeholder slices of
// clause and got. If every placeholder shifted by `offset`, only the
// digits inside `$N` tokens may differ — every other byte is identical
// in position and content modulo shifted token widths.
//
// The check walks both strings in lockstep and verifies the prefix
// before each token matches between input and output, then advances
// past the token spans. A mismatch indicates the regex pass touched
// non-token bytes — a contract violation.
func assertSurroundingTextIdentical(t *testing.T, clause, got string, origMatches, gotMatches [][]int) {
	t.Helper()
	if len(origMatches) != len(gotMatches) {
		// already asserted upstream; bail
		return
	}
	origCursor := 0
	gotCursor := 0
	for i, m := range origMatches {
		origPrefix := clause[origCursor:m[0]]
		gotPrefix := got[gotCursor:gotMatches[i][0]]
		if origPrefix != gotPrefix {
			t.Fatalf("non-placeholder text changed: original prefix %q != rebased prefix %q (token %d)",
				origPrefix, gotPrefix, i)
		}
		origCursor = m[1]
		gotCursor = gotMatches[i][1]
	}
	if clause[origCursor:] != got[gotCursor:] {
		t.Fatalf("trailing text changed: original tail %q != rebased tail %q",
			clause[origCursor:], got[gotCursor:])
	}
}

// fuzzAssertCompiledOK is a sanity guard run once at package init —
// if the production regex ever drifts in a way that breaks
// FindAllStringSubmatchIndex, we want a clear test failure rather than
// silent fuzz-corpus skipping.
var fuzzAssertCompiledOK = func() bool {
	if sqlPlaceholderPattern.NumSubexp() != 1 {
		panic(fmt.Sprintf("sqlPlaceholderPattern must have exactly one capture group, got %d", sqlPlaceholderPattern.NumSubexp()))
	}
	// Confirm the regex actually compiles (it does at package init,
	// but make it explicit for future maintainers reading this file).
	_ = regexp.MustCompile(`\$(\d+)`)
	return true
}()
