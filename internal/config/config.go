// Package config defines only the Daedalus process configuration.
// The generation Config belongs to the root package and is never stored here.
package config

import (
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

const (
	// TransportTCP selects a TCP listener restricted to a loopback IP.
	TransportTCP = "tcp"
	// TransportUDS selects a Unix domain socket on Unix systems and a named
	// pipe on Windows.
	TransportUDS = "uds"

	// DefaultTCPAddr requests an ephemeral port on loopback.
	DefaultTCPAddr = "127.0.0.1:0"
	// DefaultHTTPDebugAddr is the opt-in debug bind: literal loopback IP
	// 127.0.0.1 and port 8090. It is read only when the HTTP debug server
	// is enabled. A wildcard, a non-loopback IP, a hostname, or port 0 is
	// rejected.
	DefaultHTTPDebugAddr = "127.0.0.1:8090"

	minimumTCPPort = 1
	maximumTCPPort = 65535
)

// Config holds subprocess flags and limits, never generator parameters.
type Config struct {
	Transport                string
	Addr                     string
	MaxConcurrentGenerations int
	HTTPDebugEnabled         bool
	HTTPDebugAddr            string
}

// Default returns the default process configuration.
func Default() Config {
	return Config{
		// No default gRPC transport or address is fixed. TCP on loopback
		// with an ephemeral port works on every supported system and does
		// not expose the service remotely. HTTP debug stays off: enabling
		// or disabling it does not change the handshake.
		Transport:                TransportTCP,
		Addr:                     DefaultTCPAddr,
		MaxConcurrentGenerations: runtime.NumCPU(),
		HTTPDebugEnabled:         false,
		HTTPDebugAddr:            DefaultHTTPDebugAddr,
	}
}

// Parse interprets the process flags and validates the resulting configuration.
func Parse(args []string, stderr io.Writer) (Config, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	processConfig := Default()
	flags := pflag.NewFlagSet("daedalus", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&processConfig.Transport, "transport", processConfig.Transport, "gRPC transport: tcp or uds")
	flags.StringVar(&processConfig.Addr, "addr", processConfig.Addr, "gRPC listener address")
	flags.IntVar(
		&processConfig.MaxConcurrentGenerations,
		"max-concurrent-generations",
		processConfig.MaxConcurrentGenerations,
		"maximum concurrent generations",
	)
	flags.BoolVar(
		&processConfig.HTTPDebugEnabled,
		"http-debug-enabled",
		processConfig.HTTPDebugEnabled,
		"enables the local HTTP debug server",
	)
	flags.StringVar(
		&processConfig.HTTPDebugAddr,
		"http-debug-addr",
		processConfig.HTTPDebugAddr,
		"literal loopback IP address of the HTTP debug server",
	)
	if err := flags.Parse(args); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("positional arguments are not accepted: %s", strings.Join(flags.Args(), " "))
	}
	if err := processConfig.Validate(); err != nil {
		return Config{}, err
	}
	return processConfig, nil
}

// Validate checks addresses and limits without opening listeners.
func (config Config) Validate() error {
	if config.MaxConcurrentGenerations < 1 {
		return fmt.Errorf("max-concurrent-generations must be greater than zero")
	}
	switch config.Transport {
	case TransportTCP:
		if err := validateLiteralLoopback(config.Addr, true); err != nil {
			return fmt.Errorf("invalid addr for tcp transport: %w", err)
		}
	case TransportUDS:
		if strings.TrimSpace(config.Addr) == "" || strings.ContainsRune(config.Addr, '\x00') {
			return fmt.Errorf("invalid addr for uds transport")
		}
	default:
		return fmt.Errorf("invalid transport %q: use tcp or uds", config.Transport)
	}
	if config.HTTPDebugEnabled {
		if err := validateLiteralLoopback(config.HTTPDebugAddr, false); err != nil {
			return fmt.Errorf("invalid http-debug-addr: %w", err)
		}
	}
	return nil
}

func validateLiteralLoopback(addr string, allowPortZero bool) error {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("expected a literal IP and port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("the IP must be a literal loopback address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("invalid port")
	}
	minimumPort := minimumTCPPort
	if allowPortZero {
		minimumPort = 0
	}
	if port < minimumPort || port > maximumTCPPort {
		return fmt.Errorf("port must be between %d and %d", minimumPort, maximumTCPPort)
	}
	return nil
}
