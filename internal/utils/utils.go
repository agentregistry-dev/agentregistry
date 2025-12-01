package utils

import (
	"fmt"
	"net"
)

// FindAvailablePort finds an available port on localhost
func FindAvailablePort() (uint16, error) {
	// Try to bind to port 0, which tells the OS to pick an available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer func() { _ = listener.Close() }()

	// Get the port that was assigned
	addr := listener.Addr().(*net.TCPAddr)
	return uint16(addr.Port), nil
}
