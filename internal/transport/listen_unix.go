//go:build !windows

package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
)

func listenLocal(addr string) (net.Listener, func() error, error) {
	listener, err := net.Listen("unix", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("abrir unix domain socket: %w", err)
	}
	cleanup := func() error {
		err := os.Remove(addr)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remover unix domain socket: %w", err)
		}
		return nil
	}
	return listener, cleanup, nil
}
