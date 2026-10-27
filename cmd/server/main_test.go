package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthcheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	require.NoError(t, healthcheck(":"+port))
	require.NoError(t, healthcheck("0.0.0.0:"+port))
	require.NoError(t, healthcheck("127.0.0.1:"+port))

	require.NoError(t, ln.Close())
	require.Error(t, healthcheck(":"+port), "nothing listens once the listener closes")
	require.Error(t, healthcheck("8080"), "an address without a port is rejected")
}
