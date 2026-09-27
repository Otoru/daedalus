package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
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

func TestStartupEmiteUmaLinhaDeHandshakeEServeGRPC(t *testing.T) {
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

func TestFalhaDeBindNaoAnunciaHandshake(t *testing.T) {
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

func TestShutdownFechaListenerGRPC(t *testing.T) {
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

func TestHTTPDesabilitadoNaoAbrePortaNemAlteraHandshake(t *testing.T) {
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
