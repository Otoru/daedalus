package config

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseAppliesProcessDefaults checks that HTTP debug is off unless
// requested, and that the default address is 127.0.0.1:8090.
func TestParseAppliesProcessDefaults(t *testing.T) {
	t.Parallel()

	got, err := Parse(nil, &bytes.Buffer{})

	require.NoError(t, err)
	assert.Equal(t, TransportTCP, got.Transport)
	assert.Equal(t, DefaultTCPAddr, got.Addr)
	assert.Equal(t, runtime.NumCPU(), got.MaxConcurrentGenerations)
	assert.False(t, got.HTTPDebugEnabled)
	assert.Equal(t, DefaultHTTPDebugAddr, got.HTTPDebugAddr)
}

func TestParseAcceptsProcessFlags(t *testing.T) {
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

func TestParseRejectsNonPositiveConcurrency(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--max-concurrent-generations=0"}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "max-concurrent-generations")
}

// TestParseValidatesHTTPAddressOnlyWhenEnabled checks that a non-loopback HTTP
// address is rejected when debug is enabled, before any listener starts.
func TestParseValidatesHTTPAddressOnlyWhenEnabled(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--http-debug-addr=0.0.0.0:8090"}, &bytes.Buffer{})
	require.NoError(t, err)

	cases := []struct {
		name string
		addr string
	}{
		{name: "wildcard", addr: "0.0.0.0:8090"},
		{name: "remote ip", addr: "192.0.2.1:8090"},
		{name: "hostname", addr: "localhost:8090"},
		{name: "port zero", addr: "127.0.0.1:0"},
		{name: "missing port", addr: "127.0.0.1"},
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

func TestParseRejectsTCPOutsideLoopback(t *testing.T) {
	t.Parallel()

	_, err := Parse([]string{"--transport=tcp", "--addr=0.0.0.0:1234"}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "addr")
}

func TestParseSendsFlagErrorsToProvidedWriter(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	_, err := Parse([]string{"--nonexistent-flag"}, &stderr)

	require.Error(t, err)
	assert.NotEmpty(t, stderr.String())
}
