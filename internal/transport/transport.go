// Package transport owns and cleans up the subprocess listener.
package transport

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/Otoru/daedalus/internal/config"
)

// Listener wraps the effective listener and its resolved address.
type Listener struct {
	net.Listener

	transport    string
	resolvedAddr string
	cleanup      func() error
	closeOnce    sync.Once
	closeErr     error
}

// Listen opens the configured transport without announcing the process.
// A bind failure returns an error and leaves nothing for a handshake to
// advertise. Announcement happens only after every requested listener
// has bound.
func Listen(processConfig config.Config) (*Listener, error) {
	if err := processConfig.Validate(); err != nil {
		return nil, err
	}
	switch processConfig.Transport {
	case config.TransportTCP:
		listener, err := net.Listen("tcp", processConfig.Addr)
		if err != nil {
			return nil, fmt.Errorf("open tcp listener: %w", err)
		}
		return &Listener{
			Listener: listener, transport: config.TransportTCP,
			resolvedAddr: listener.Addr().String(),
		}, nil
	case config.TransportUDS:
		listener, cleanup, err := listenLocal(processConfig.Addr)
		if err != nil {
			return nil, err
		}
		return &Listener{
			Listener: listener, transport: config.TransportUDS,
			resolvedAddr: processConfig.Addr, cleanup: cleanup,
		}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", processConfig.Transport)
	}
}

// Transport returns the handshake transport identifier.
func (listener *Listener) Transport() string {
	return listener.transport
}

// ResolvedAddr returns the effective address announced in the handshake.
func (listener *Listener) ResolvedAddr() string {
	return listener.resolvedAddr
}

// Close closes the listener and removes local resources owned by the process.
func (listener *Listener) Close() error {
	listener.closeOnce.Do(func() {
		listener.closeErr = listener.Listener.Close()
		if errors.Is(listener.closeErr, net.ErrClosed) {
			listener.closeErr = nil
		}
		if listener.cleanup != nil {
			if cleanupErr := listener.cleanup(); listener.closeErr == nil {
				listener.closeErr = cleanupErr
			}
		}
	})
	return listener.closeErr
}
