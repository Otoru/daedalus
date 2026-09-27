package config

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAplicaDefaultsDoProcesso(t *testing.T) {
	t.Parallel()

	got, err := Parse(nil, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, TransportTCP, got.Transport)
	assert.Equal(t, DefaultTCPAddr, got.Addr)
	assert.Equal(t, runtime.NumCPU(), got.MaxConcurrentGenerations)
	assert.False(t, got.HTTPDebugEnabled)
	assert.Equal(t, DefaultHTTPDebugAddr, got.HTTPDebugAddr)
}

func TestParseAceitaFlagsDoProcesso(t *testing.T) {
	t.Parallel()

	got, err := Parse([]string{
		"--transport=uds",
		"--addr=/tmp/daedalus-test.sock",
		"--max-concurrent-generations=3",
		"--http-debug-enabled",
		"--http-debug-addr=127.0.0.1:18090",
	}, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, TransportUDS, got.Transport)
	assert.Equal(t, "/tmp/daedalus-test.sock", got.Addr)
	assert.Equal(t, 3, got.MaxConcurrentGenerations)
	assert.True(t, got.HTTPDebugEnabled)
	assert.Equal(t, "127.0.0.1:18090", got.HTTPDebugAddr)
}

func TestParseRejeitaConcorrenciaNaoPositiva(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--max-concurrent-generations=0"}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max-concurrent-generations")
}

func TestParseValidaEnderecoHTTPApenasQuandoHabilitado(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--http-debug-addr=0.0.0.0:8090"}, &bytes.Buffer{})
	require.NoError(t, err)

	cases := []struct {
		name string
		addr string
	}{
		{name: "wildcard", addr: "0.0.0.0:8090"},
		{name: "ip remoto", addr: "192.0.2.1:8090"},
		{name: "hostname", addr: "localhost:8090"},
		{name: "porta zero", addr: "127.0.0.1:0"},
		{name: "sem porta", addr: "127.0.0.1"},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, parseErr := Parse([]string{
				"--http-debug-enabled",
				"--http-debug-addr=" + testCase.addr,
			}, &bytes.Buffer{})

			require.Error(t, parseErr)
			assert.Contains(t, parseErr.Error(), "http-debug-addr")
		})
	}
}

func TestParseRejeitaTCPForaDeLoopback(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--transport=tcp", "--addr=0.0.0.0:1234"}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "addr")
}

func TestParseDirecionaErrosDeFlagAoWriterInformado(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := Parse([]string{"--flag-inexistente"}, &stderr)

	require.Error(t, err)
	assert.NotEmpty(t, stderr.String())
}
