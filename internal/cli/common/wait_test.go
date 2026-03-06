package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWaitConstants(t *testing.T) {
	assert.Equal(t, 5*time.Minute, defaultWaitTimeout)
	assert.Equal(t, 2*time.Second, defaultPollInterval)
}
