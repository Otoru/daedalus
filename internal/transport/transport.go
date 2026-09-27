// Package transport possui e limpa o listener do subprocesso.
package transport

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/Otoru/daedalus/internal/config"
)

// Listener envolve o listener efetivo e seu endereço resolvido.
type Listener struct {
	net.Listener

	transport    string
	resolvedAddr string
	cleanup      func() error
	closeOnce    sync.Once
	closeErr     error
}

// Listen abre o transporte configurado sem anunciar o processo.
func Listen(processConfig config.Config) (*Listener, error) {
	if err := processConfig.Validate(); err != nil {
		return nil, err
	}
	switch processConfig.Transport {
	case config.TransportTCP:
		listener, err := net.Listen("tcp", processConfig.Addr)
		if err != nil {
			return nil, fmt.Errorf("abrir listener tcp: %w", err)
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
		return nil, fmt.Errorf("transporte desconhecido %q", processConfig.Transport)
	}
}

// Transport devolve o identificador de transporte do handshake.
func (listener *Listener) Transport() string {
	return listener.transport
}

// ResolvedAddr devolve o endereço efetivo anunciado no handshake.
func (listener *Listener) ResolvedAddr() string {
	return listener.resolvedAddr
}

// Close fecha o listener e remove recursos locais de propriedade do processo.
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
