package v1alpha1store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The record_control_plane_event() body appears in both 009 (original) and
// 015 (repair for databases that ran 009's pre-exemption variant). They must
// stay byte-identical: an in-place edit to one without the other silently
// recreates the divergence 015 exists to fix. Edit both or add a new repair
// migration.
func TestControlPlaneEventFunctionBodiesMatch(t *testing.T) {
	t.Parallel()
	extract := func(path string) string {
		raw, err := v1alpha1MigrationFiles.ReadFile(path)
		require.NoError(t, err)
		content := string(raw)
		start := strings.Index(content, "CREATE OR REPLACE FUNCTION record_control_plane_event()")
		require.GreaterOrEqual(t, start, 0, "%s: function definition not found", path)
		end := strings.Index(content[start:], "$$ LANGUAGE plpgsql;")
		require.GreaterOrEqual(t, end, 0, "%s: function terminator not found", path)
		return content[start : start+end]
	}

	original := extract("migrations/009_controller_foundations.up.sql")
	repair := extract("migrations/015_reassert_control_plane_event_function.up.sql")
	require.Equal(t, original, repair,
		"record_control_plane_event() diverged between 009 and 015; edit both or ship a new repair migration")
}
