package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Otoru/daedalus/internal/config"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/internal/logging"
	"github.com/Otoru/daedalus/internal/service"
	"github.com/Otoru/daedalus/internal/transport"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const (
	developmentVersion = "dev"
	// lifecycleTimeout limita startup e shutdown para que sinais não deixem
	// o subprocesso bloqueado indefinidamente.
	lifecycleTimeout = 15 * time.Second
)

// Version recebe o valor de release por -ldflags "-X main.Version=vX.Y.Z".
var Version = developmentVersion

type handshake struct {
	Transport string `json:"transport"`
	Addr      string `json:"addr"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
}

type stdoutWriter struct{ io.Writer }
type stderrWriter struct{ io.Writer }

type grpcRuntime struct {
	processConfig config.Config
	service       *service.Server
	logger        *zap.Logger
	stdout        io.Writer

	listener   *transport.Listener
	grpcServer *grpc.Server
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	processConfig, err := config.Parse(args, stderr)
	if err != nil {
		return err
	}
	app := newApp(processConfig, stdout, stderr)
	if err := app.Err(); err != nil {
		return fmt.Errorf("compor subprocesso: %w", err)
	}

	startContext, cancelStart := context.WithTimeout(context.Background(), lifecycleTimeout)
	err = app.Start(startContext)
	cancelStart()
	if err != nil {
		return fmt.Errorf("iniciar subprocesso: %w", err)
	}

	signalContext, stopSignals := notifyShutdown(context.Background())
	<-signalContext.Done()
	stopSignals()

	stopContext, cancelStop := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancelStop()
	if err := app.Stop(stopContext); err != nil {
		return fmt.Errorf("encerrar subprocesso: %w", err)
	}
	return nil
}

func newApp(processConfig config.Config, stdout, stderr io.Writer) *fx.App {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return fx.New(
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
		fx.Supply(
			processConfig,
			stdoutWriter{Writer: stdout},
			stderrWriter{Writer: stderr},
		),
		fx.Provide(
			func(writer stderrWriter) *zap.Logger { return logging.New(writer.Writer) },
			func(processConfig config.Config) *service.Admission {
				return service.NewAdmission(processConfig.MaxConcurrentGenerations)
			},
			func(admission *service.Admission) *service.Server {
				return service.New(nil, admission)
			},
			newGRPCRuntime,
		),
		fx.Invoke(func(*grpcRuntime) {}),
	)
}

func newGRPCRuntime(
	lifecycle fx.Lifecycle,
	processConfig config.Config,
	server *service.Server,
	logger *zap.Logger,
	stdout stdoutWriter,
) *grpcRuntime {
	runtime := &grpcRuntime{
		processConfig: processConfig,
		service:       server,
		logger:        logger,
		stdout:        stdout.Writer,
	}
	lifecycle.Append(fx.Hook{
		OnStart: runtime.start,
		OnStop:  runtime.stop,
	})
	return runtime
}

func (runtime *grpcRuntime) start(context.Context) error {
	listener, err := transport.Listen(runtime.processConfig)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer()
	daedalusv1.RegisterDaedalusServiceServer(grpcServer, runtime.service)

	runtime.listener = listener
	runtime.grpcServer = grpcServer
	go func() {
		serveErr := grpcServer.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			runtime.logger.Error("servidor gRPC encerrou com erro", zap.Error(serveErr))
		}
	}()

	wireHandshake := handshake{
		Transport: listener.Transport(), Addr: listener.ResolvedAddr(),
		PID: os.Getpid(), Version: Version,
	}
	if err := json.NewEncoder(runtime.stdout).Encode(wireHandshake); err != nil {
		runtime.service.BeginShutdown()
		grpcServer.Stop()
		_ = listener.Close()
		return fmt.Errorf("escrever handshake no stdout: %w", err)
	}
	runtime.logger.Info(
		"servidor gRPC iniciado",
		zap.String("transport", listener.Transport()),
		zap.String("addr", listener.ResolvedAddr()),
	)
	return nil
}

func (runtime *grpcRuntime) stop(ctx context.Context) error {
	if runtime.grpcServer == nil {
		return nil
	}
	runtime.service.BeginShutdown()

	stopped := make(chan struct{})
	go func() {
		runtime.grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		runtime.grpcServer.Stop()
		<-stopped
	}
	if err := runtime.listener.Close(); err != nil {
		return err
	}
	runtime.logger.Info("servidor gRPC encerrado")
	return nil
}
