// Package config define somente a configuração do processo Daedalus.
// A Config de geração pertence ao pacote raiz e nunca é armazenada aqui.
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
	// TransportTCP seleciona um listener TCP restrito a um IP de loopback.
	TransportTCP = "tcp"
	// TransportUDS seleciona Unix domain socket nos sistemas Unix e named
	// pipe no Windows.
	TransportUDS = "uds"

	// DefaultTCPAddr solicita uma porta efêmera no loopback.
	DefaultTCPAddr = "127.0.0.1:0"
	// DefaultHTTPDebugAddr é o endereço normativo da seção 2.3.
	DefaultHTTPDebugAddr = "127.0.0.1:8090"

	minimumTCPPort = 1
	maximumTCPPort = 65535
)

// Config contém flags e limites do subprocesso, nunca parâmetros do gerador.
type Config struct {
	Transport                string
	Addr                     string
	MaxConcurrentGenerations int
	HTTPDebugEnabled         bool
	HTTPDebugAddr            string
}

// Default devolve a configuração padrão do processo.
func Default() Config {
	return Config{
		// A especificação não fixa transporte/endereço padrão. TCP em
		// loopback com porta efêmera é a leitura conservadora: funciona em
		// todos os sistemas suportados e não expõe o serviço remotamente.
		Transport:                TransportTCP,
		Addr:                     DefaultTCPAddr,
		MaxConcurrentGenerations: runtime.NumCPU(),
		HTTPDebugEnabled:         false,
		HTTPDebugAddr:            DefaultHTTPDebugAddr,
	}
}

// Parse interpreta as flags do processo e valida a configuração resultante.
func Parse(args []string, stderr io.Writer) (Config, error) {
	if stderr == nil {
		stderr = io.Discard
	}
	processConfig := Default()
	flags := pflag.NewFlagSet("daedalus", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&processConfig.Transport, "transport", processConfig.Transport, "transporte gRPC: tcp ou uds")
	flags.StringVar(&processConfig.Addr, "addr", processConfig.Addr, "endereço do listener gRPC")
	flags.IntVar(
		&processConfig.MaxConcurrentGenerations,
		"max-concurrent-generations",
		processConfig.MaxConcurrentGenerations,
		"máximo de gerações simultâneas",
	)
	flags.BoolVar(
		&processConfig.HTTPDebugEnabled,
		"http-debug-enabled",
		processConfig.HTTPDebugEnabled,
		"habilita o servidor HTTP local de depuração",
	)
	flags.StringVar(
		&processConfig.HTTPDebugAddr,
		"http-debug-addr",
		processConfig.HTTPDebugAddr,
		"endereço IP literal de loopback do servidor HTTP de depuração",
	)
	if err := flags.Parse(args); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return Config{}, fmt.Errorf("interpretar flags: %w", err)
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("argumentos posicionais não são aceitos: %s", strings.Join(flags.Args(), " "))
	}
	if err := processConfig.Validate(); err != nil {
		return Config{}, err
	}
	return processConfig, nil
}

// Validate valida endereços e limites sem abrir listeners.
func (config Config) Validate() error {
	if config.MaxConcurrentGenerations < 1 {
		return fmt.Errorf("max-concurrent-generations deve ser maior que zero")
	}
	switch config.Transport {
	case TransportTCP:
		if err := validateLiteralLoopback(config.Addr, true); err != nil {
			return fmt.Errorf("addr inválido para transporte tcp: %w", err)
		}
	case TransportUDS:
		if strings.TrimSpace(config.Addr) == "" || strings.ContainsRune(config.Addr, '\x00') {
			return fmt.Errorf("addr inválido para transporte uds")
		}
	default:
		return fmt.Errorf("transport inválido %q: use tcp ou uds", config.Transport)
	}
	if config.HTTPDebugEnabled {
		if err := validateLiteralLoopback(config.HTTPDebugAddr, false); err != nil {
			return fmt.Errorf("http-debug-addr inválido: %w", err)
		}
	}
	return nil
}

func validateLiteralLoopback(addr string, allowPortZero bool) error {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("esperado IP literal e porta: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("o IP deve ser literal e de loopback")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("porta inválida")
	}
	minimumPort := minimumTCPPort
	if allowPortZero {
		minimumPort = 0
	}
	if port < minimumPort || port > maximumTCPPort {
		return fmt.Errorf("porta deve estar entre %d e %d", minimumPort, maximumTCPPort)
	}
	return nil
}
