package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Otoru/daedalus/internal/config"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const testLifecycleTimeout = 5 * time.Second

// TestStartupEmitsOneHandshakeLineAndServesGRPC covers AC-16: startup writes
// exactly one valid handshake line, and the client connects only after that.
func TestStartupEmitsOneHandshakeLineAndServesGRPC(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	processConfig := config.Default()
	app := newApp(processConfig, &stdout, &stderr)

	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	defer cancelStart()
	require.NoError(t, app.Start(startContext))
	t.Cleanup(func() {
		stopContext, cancelStop := context.WithTimeout(context.Background(), testLifecycleTimeout)
		defer cancelStop()
		require.NoError(t, app.Stop(stopContext))
	})

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	require.Len(t, lines, 1)
	assert.True(t, strings.HasSuffix(stdout.String(), "\n"))

	var got handshake
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &got))
	assert.Equal(t, config.TransportTCP, got.Transport)
	assert.NotEqual(t, config.DefaultTCPAddr, got.Addr)
	assert.Equal(t, os.Getpid(), got.PID)
	assert.Equal(t, Version, got.Version)

	connection, err := grpc.NewClient(
		got.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := daedalusv1.NewDaedalusServiceClient(connection)
	response, err := client.Generate(context.Background(), &daedalusv1.GenerateRequest{
		Config: &daedalusv1.Config{Width: 1, Height: 1, Seed: 7},
	})
	require.NoError(t, err)
	require.NotNil(t, response.Layout)
	assert.Equal(t, uint64(7), response.Layout.Seed)
}

func TestBindFailureDoesNotAnnounceHandshake(t *testing.T) {
	occupied, err := net.Listen("tcp", config.DefaultTCPAddr)
	require.NoError(t, err)
	defer func() { require.NoError(t, occupied.Close()) }()

	processConfig := config.Default()
	processConfig.Addr = occupied.Addr().String()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newApp(processConfig, &stdout, &stderr)

	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	defer cancelStart()
	err = app.Start(startContext)

	require.Error(t, err)
	assert.Empty(t, stdout.String())
}

func TestShutdownClosesGRPCListener(t *testing.T) {
	var stdout bytes.Buffer
	app := newApp(config.Default(), &stdout, &bytes.Buffer{})
	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	require.NoError(t, app.Start(startContext))
	cancelStart()

	var got handshake
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got))
	stopContext, cancelStop := context.WithTimeout(context.Background(), testLifecycleTimeout)
	require.NoError(t, app.Stop(stopContext))
	cancelStop()

	connection, err := net.DialTimeout("tcp", got.Addr, 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
	}
	require.Error(t, err)
}

// TestDisabledHTTPDoesNotOpenPortOrChangeHandshake covers AC-30: with HTTP
// debug disabled, the HTTP address stays free to bind and the gRPC handshake
// is unchanged.
func TestDisabledHTTPDoesNotOpenPortOrChangeHandshake(t *testing.T) {
	probe, err := net.Listen("tcp", config.DefaultTCPAddr)
	require.NoError(t, err)
	httpAddr := probe.Addr().String()
	require.NoError(t, probe.Close())

	processConfig := config.Default()
	processConfig.HTTPDebugEnabled = false
	processConfig.HTTPDebugAddr = httpAddr
	var stdout bytes.Buffer
	app := newApp(processConfig, &stdout, &bytes.Buffer{})
	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	defer cancelStart()
	require.NoError(t, app.Start(startContext))
	defer func() {
		stopContext, cancelStop := context.WithTimeout(context.Background(), testLifecycleTimeout)
		defer cancelStop()
		require.NoError(t, app.Stop(stopContext))
	}()

	httpProbe, err := net.Listen("tcp", httpAddr)
	require.NoError(t, err)
	require.NoError(t, httpProbe.Close())

	var got handshake
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got))
	assert.Equal(t, config.TransportTCP, got.Transport)
	assert.NotEqual(t, httpAddr, got.Addr)
}

// TestEnabledHTTPServesRoutesWithoutChangingGRPCHandshake covers AC-31: with
// HTTP debug enabled, GET /healthz and GET /debug/ succeed and the gRPC
// handshake stays on its own address.
func TestEnabledHTTPServesRoutesWithoutChangingGRPCHandshake(t *testing.T) {
	httpAddr := reserveTCPAddress(t)
	processConfig := config.Default()
	processConfig.HTTPDebugEnabled = true
	processConfig.HTTPDebugAddr = httpAddr
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newApp(processConfig, &stdout, &stderr)

	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	defer cancelStart()
	require.NoError(t, app.Start(startContext))
	t.Cleanup(func() {
		stopContext, cancelStop := context.WithTimeout(context.Background(), testLifecycleTimeout)
		defer cancelStop()
		require.NoError(t, app.Stop(stopContext))
	})

	var got handshake
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got))
	assert.Equal(t, config.TransportTCP, got.Transport)
	assert.NotEqual(t, httpAddr, got.Addr)

	healthResponse, err := http.Get("http://" + httpAddr + "/healthz")
	require.NoError(t, err)
	defer func() { require.NoError(t, healthResponse.Body.Close()) }()
	healthBody, err := io.ReadAll(healthResponse.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, healthResponse.StatusCode)
	assert.JSONEq(t, `{"status":"ok","version":"`+Version+`"}`, string(healthBody))

	debugResponse, err := http.Get("http://" + httpAddr + "/debug/")
	require.NoError(t, err)
	defer func() { require.NoError(t, debugResponse.Body.Close()) }()
	assert.Equal(t, http.StatusOK, debugResponse.StatusCode)
	assert.Contains(t, stderr.String(), "HTTP debug server started")
	assert.Contains(t, stderr.String(), httpAddr)
}

// TestHTTPBindFailureAbortsStartupWithoutAnnouncingOrKeepingGRPC covers
// AC-38: an HTTP port already in use aborts startup, writes no handshake and
// does not leave the gRPC listener open.
func TestHTTPBindFailureAbortsStartupWithoutAnnouncingOrKeepingGRPC(t *testing.T) {
	occupiedHTTP, err := net.Listen("tcp", config.DefaultTCPAddr)
	require.NoError(t, err)
	defer func() { require.NoError(t, occupiedHTTP.Close()) }()

	grpcAddr := reserveTCPAddress(t)
	processConfig := config.Default()
	processConfig.Addr = grpcAddr
	processConfig.HTTPDebugEnabled = true
	processConfig.HTTPDebugAddr = occupiedHTTP.Addr().String()
	var stdout bytes.Buffer
	app := newApp(processConfig, &stdout, &bytes.Buffer{})

	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	defer cancelStart()
	err = app.Start(startContext)

	require.Error(t, err)
	assert.Empty(t, stdout.String())
	grpcProbe, listenErr := net.Listen("tcp", grpcAddr)
	require.NoError(t, listenErr, "gRPC listener must be closed after the HTTP failure")
	require.NoError(t, grpcProbe.Close())
}

// TestShutdownClosesGRPCAndHTTPListeners covers AC-39: shutdown closes the
// HTTP listener together with the gRPC listener.
func TestShutdownClosesGRPCAndHTTPListeners(t *testing.T) {
	httpAddr := reserveTCPAddress(t)
	processConfig := config.Default()
	processConfig.HTTPDebugEnabled = true
	processConfig.HTTPDebugAddr = httpAddr
	var stdout bytes.Buffer
	app := newApp(processConfig, &stdout, &bytes.Buffer{})

	startContext, cancelStart := context.WithTimeout(context.Background(), testLifecycleTimeout)
	require.NoError(t, app.Start(startContext))
	cancelStart()
	var got handshake
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got))
	healthResponse, err := http.Get("http://" + httpAddr + "/healthz")
	require.NoError(t, err)
	require.NoError(t, healthResponse.Body.Close())
	require.Equal(t, http.StatusOK, healthResponse.StatusCode)

	stopContext, cancelStop := context.WithTimeout(context.Background(), testLifecycleTimeout)
	require.NoError(t, app.Stop(stopContext))
	cancelStop()

	for _, addr := range []string{got.Addr, httpAddr} {
		connection, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
		}
		require.Error(t, err, "listener %s remained open", addr)
	}
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", config.DefaultTCPAddr)
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	return addr
}
