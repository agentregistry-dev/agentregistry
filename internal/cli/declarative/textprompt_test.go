package declarative

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptText_TypedValue(t *testing.T) {
	out := &bytes.Buffer{}
	got, err := promptText("Name", "myagent", nil, out, strings.NewReader("mybot\n"))
	require.NoError(t, err)
	assert.Equal(t, "mybot", got)
	assert.Contains(t, out.String(), "(myagent)")
}

func TestPromptText_EmptyAcceptsDefault(t *testing.T) {
	out := &bytes.Buffer{}
	got, err := promptText("Name", "myagent", nil, out, strings.NewReader("\n"))
	require.NoError(t, err)
	assert.Equal(t, "myagent", got)
}

func TestPromptText_EOFAcceptsDefault(t *testing.T) {
	out := &bytes.Buffer{}
	got, err := promptText("Name", "myagent", nil, out, strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, "myagent", got)
}

func TestPromptText_TrimsWhitespace(t *testing.T) {
	out := &bytes.Buffer{}
	got, err := promptText("Name", "default", nil, out, strings.NewReader("  spaced  \n"))
	require.NoError(t, err)
	assert.Equal(t, "spaced", got)
}

func TestPromptText_NoDefaultLabelOmitsParens(t *testing.T) {
	out := &bytes.Buffer{}
	_, _ = promptText("Name", "", nil, out, strings.NewReader("foo\n"))
	assert.NotContains(t, out.String(), "(")
}

func TestPromptText_ValidatorRejectsThenAccepts(t *testing.T) {
	out := &bytes.Buffer{}
	v := func(s string) error {
		if strings.Contains(s, "-") {
			return fmt.Errorf("name must not contain hyphens")
		}
		return nil
	}
	got, err := promptText("Name", "", v, out, strings.NewReader("bad-name\ngoodname\n"))
	require.NoError(t, err)
	assert.Equal(t, "goodname", got)
	assert.Contains(t, out.String(), "must not contain hyphens")
}

func TestPromptText_ValidatorThreeStrikesAborts(t *testing.T) {
	out := &bytes.Buffer{}
	v := func(s string) error { return errors.New("never ok") }
	_, err := promptText("Name", "", v, out, strings.NewReader("a\nb\nc\nd\n"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errTooManyAttempts))
}
