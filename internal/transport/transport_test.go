package transport

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/Otoru/daedalus/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenTCPDevolveEnderecoResolvidoELoopback(t *testing.T) {
	t.Parallel()

	listener, err := Listen(config.Config{
		Transport:                config.TransportTCP,
		Addr:                     config.DefaultTCPAddr,
		MaxConcurrentGenerations: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	assert.Equal(t, config.TransportTCP, listener.Transport())
	assert.NotEqual(t, config.DefaultTCPAddr, listener.ResolvedAddr())
	host, port, err := net.SplitHostPort(listener.ResolvedAddr())
	require.NoError(t, err)
	assert.True(t, net.ParseIP(host).IsLoopback())
	assert.NotEqual(t, "0", port)
}

func TestListenFalhaQuandoEnderecoTCPEstaOcupado(t *testing.T) {
	t.Parallel()

	occupied, err := net.Listen("tcp", config.DefaultTCPAddr)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, occupied.Close()) })

	_, err = Listen(config.Config{
		Transport:                config.TransportTCP,
		Addr:                     occupied.Addr().String(),
		MaxConcurrentGenerations: 1,
	})

	require.Error(t, err)
}

func TestListenUDSLimpaArquivoAoFechar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix domain socket é exercitado somente em sistemas Unix")
	}
	t.Parallel()

	path := fmt.Sprintf("/tmp/daedalus-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { _ = os.Remove(path) })
	listener, err := Listen(config.Config{
		Transport:                config.TransportUDS,
		Addr:                     path,
		MaxConcurrentGenerations: 1,
	})
	require.NoError(t, err)

	assert.Equal(t, path, listener.ResolvedAddr())
	require.NoError(t, listener.Close())
	_, err = net.Dial("unix", path)
	require.Error(t, err)
}
