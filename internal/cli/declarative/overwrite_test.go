package declarative

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmOverwrite_Yes(t *testing.T) {
	out := &bytes.Buffer{}
	in := strings.NewReader("y\n")
	ok, err := confirmOverwrite("foo", out, in)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Contains(t, out.String(), `"foo"`)
	assert.Contains(t, out.String(), "(y/N)")
}

func TestConfirmOverwrite_YesUppercase(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader("Y\n"))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmOverwrite_YesWord(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader("YES\n"))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmOverwrite_No(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader("n\n"))
	require.False(t, ok)
	require.True(t, errors.Is(err, errOverwriteDeclined))
}

func TestConfirmOverwrite_EmptyDefaultsToNo(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader("\n"))
	require.False(t, ok)
	require.True(t, errors.Is(err, errOverwriteDeclined))
}

func TestConfirmOverwrite_EOFDefaultsToNo(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader(""))
	require.False(t, ok)
	require.True(t, errors.Is(err, errOverwriteDeclined))
}

func TestConfirmOverwrite_GarbageDefaultsToNo(t *testing.T) {
	out := &bytes.Buffer{}
	ok, err := confirmOverwrite("foo", out, strings.NewReader("maybe\n"))
	require.False(t, ok)
	require.True(t, errors.Is(err, errOverwriteDeclined))
}
