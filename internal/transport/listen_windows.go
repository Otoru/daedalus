//go:build windows

package transport

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func listenLocal(addr string) (net.Listener, func() error, error) {
	listener, err := winio.ListenPipe(addr, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open named pipe: %w", err)
	}
	return listener, nil, nil
}
