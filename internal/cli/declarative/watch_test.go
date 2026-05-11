package declarative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldIgnore_GitDir(t *testing.T) {
	assert.True(t, shouldIgnore("/proj/.git/refs/main"))
	assert.True(t, shouldIgnore("/proj/.gitignore"))
	assert.False(t, shouldIgnore("/proj/agent.py"))
}
