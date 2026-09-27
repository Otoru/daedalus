package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/Otoru/daedalus/internal/config"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"github.com/Otoru/daedalus/internal/httpdebug"
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
	// lifecycleTimeout limits startup and shutdown so signals do not leave
	// the subprocess blocked indefinitely.
	lifecycleTimeout = 15 * time.Second
	// httpReadHeaderTimeout limits local clients that send incomplete headers
	// without imposing an additional deadline on generation.
	httpReadHeaderTimeout = 5 * time.Second
)

// Version receives the release value via -ldflags "-X main.Version=vX.Y.Z".
var Version = developmentVersion

// handshake is the only bytes ever written to stdout: one JSON object
// with transport, addr, pid and version, followed by a newline. The
// client reads that line before connecting. Logs, flag errors, and
// diagnostics go to stderr.
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

	listener     *transport.Listener
	grpcServer   *grpc.Server
	httpDebug    *httpdebug.Server
	httpListener net.Listener
	httpServer   *http.Server
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
		return fmt.Errorf("compose subprocess: %w", err)
	}

	startContext, cancelStart := context.WithTimeout(context.Background(), lifecycleTimeout)
	err = app.Start(startContext)
	cancelStart()
	if err != nil {
		return fmt.Errorf("start subprocess: %w", err)
	}

	signalContext, stopSignals := notifyShutdown(context.Background())
	<-signalContext.Done()
	stopSignals()

	stopContext, cancelStop := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancelStop()
	if err := app.Stop(stopContext); err != nil {
		return fmt.Errorf("stop subprocess: %w", err)
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
		// fx logs would be a second stdout write and break the handshake.
		// The null logger keeps that stream to the single JSON line.
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
			func(server *service.Server, logger *zap.Logger) *httpdebug.Server {
				return httpdebug.New(server, Version, logger)
			},
			newGRPCRuntime,
		),
		fx.Invoke(func(*grpcRuntime) {
			// fx calls a Provide constructor only when another component
			// requests its result. This invoke is that request: newGRPCRuntime
			// must run so it can append the gRPC and HTTP OnStart/OnStop hooks
			// to the lifecycle. The body is empty because those hooks, not this
			// function, start and stop the servers.
		}),
	)
}

func newGRPCRuntime(
	lifecycle fx.Lifecycle,
	processConfig config.Config,
	server *service.Server,
	httpDebug *httpdebug.Server,
	logger *zap.Logger,
	stdout stdoutWriter,
) *grpcRuntime {
	runtime := &grpcRuntime{
		processConfig: processConfig,
		service:       server,
		httpDebug:     httpDebug,
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
	// Failing to bind any requested listener aborts startup completely.
	// The handshake is written only after every requested listener is
	// open, so a partial service is never announced.
	listener, err := transport.Listen(runtime.processConfig)
	if err != nil {
		return err
	}

	var httpListener net.Listener
	if runtime.processConfig.HTTPDebugEnabled {
		httpListener, err = net.Listen("tcp", runtime.processConfig.HTTPDebugAddr)
		if err != nil {
			// The gRPC listener is already open. Close it before returning
			// so startup failure leaves no listening socket behind.
			_ = listener.Close()
			return fmt.Errorf("open HTTP debug listener: %w", err)
		}
	}

	grpcServer := grpc.NewServer()
	daedalusv1.RegisterDaedalusServiceServer(grpcServer, runtime.service)

	runtime.listener = listener
	runtime.grpcServer = grpcServer
	runtime.httpListener = httpListener
	go func() {
		serveErr := grpcServer.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			runtime.logger.Error("gRPC server stopped with error", zap.Error(serveErr))
		}
	}()
	if httpListener != nil {
		runtime.httpServer = &http.Server{
			Handler:           runtime.httpDebug.Handler(),
			ReadHeaderTimeout: httpReadHeaderTimeout,
		}
		go func() {
			serveErr := runtime.httpServer.Serve(httpListener)
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				runtime.logger.Error("HTTP debug server stopped with error", zap.Error(serveErr))
			}
		}()
		runtime.logger.Info(
			"HTTP debug server started",
			zap.String("addr", httpListener.Addr().String()),
			zap.Bool("active", true),
		)
	}

	wireHandshake := handshake{
		Transport: listener.Transport(), Addr: listener.ResolvedAddr(),
		PID: os.Getpid(), Version: Version,
	}
	if err := json.NewEncoder(runtime.stdout).Encode(wireHandshake); err != nil {
		runtime.service.BeginShutdown()
		grpcServer.Stop()
		if runtime.httpServer != nil {
			_ = runtime.httpServer.Close()
		}
		if httpListener != nil {
			_ = httpListener.Close()
		}
		_ = listener.Close()
		return fmt.Errorf("write handshake to stdout: %w", err)
	}
	runtime.logger.Info(
		"gRPC server started",
		zap.String("transport", listener.Transport()),
		zap.String("addr", listener.ResolvedAddr()),
	)
	return nil
}

// stop marks the service as not serving, stops admission, lets in-flight
// work observe cancellation, and shuts gRPC down gracefully. When HTTP
// debug is enabled it is closed in the same lifecycle, alongside gRPC.
func (runtime *grpcRuntime) stop(ctx context.Context) error {
	if runtime.grpcServer == nil {
		return nil
	}
	runtime.service.BeginShutdown()

	grpcStopped := make(chan struct{})
	go func() {
		runtime.grpcServer.GracefulStop()
		close(grpcStopped)
	}()
	var httpErr error
	if runtime.httpServer != nil {
		httpErr = runtime.httpServer.Shutdown(ctx)
		if httpErr != nil {
			_ = runtime.httpServer.Close()
		}
	}
	select {
	case <-grpcStopped:
	case <-ctx.Done():
		runtime.grpcServer.Stop()
		<-grpcStopped
	}
	var listenerErr error
	if runtime.listener != nil {
		listenerErr = runtime.listener.Close()
	}
	if runtime.httpListener != nil && httpErr == nil {
		if err := runtime.httpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			httpErr = err
		}
	}
	if httpErr != nil || listenerErr != nil {
		return errors.Join(httpErr, listenerErr)
	}
	if runtime.httpServer != nil {
		runtime.logger.Info("HTTP debug server stopped")
	}
	runtime.logger.Info("gRPC server stopped")
	return nil
}
