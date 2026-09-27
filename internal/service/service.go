// Package service adapta o SDK puro ao serviço gRPC v1.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/Otoru/daedalus"
	daedalusv1 "github.com/Otoru/daedalus/internal/gen/go/daedalus/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumGridDimension uint32 = 256

// GenerateFunc é a fronteira compartilhável de geração usada pelos
// adaptadores gRPC e HTTP.
type GenerateFunc func(context.Context, daedalus.Config) (daedalus.Layout, error)

// Server implementa DaedalusService sem reter estado entre gerações.
type Server struct {
	daedalusv1.UnimplementedDaedalusServiceServer

	generate  GenerateFunc
	admission *Admission
	context   context.Context
	cancel    context.CancelFunc
	serving   atomic.Bool
}

// New cria o serviço com o gerador e o limitador compartilhado informados.
// Um generate nil seleciona exclusivamente os algoritmos embutidos do SDK.
func New(generate GenerateFunc, admission *Admission) *Server {
	if generate == nil {
		generator := daedalus.Generator{}
		generate = generator.GenerateContext
	}
	if admission == nil {
		panic("admission não pode ser nil")
	}
	serviceContext, cancel := context.WithCancel(context.Background())
	server := &Server{
		generate: generate, admission: admission,
		context: serviceContext, cancel: cancel,
	}
	server.serving.Store(true)
	return server
}

// Generate valida, admite e executa uma geração completa.
func (server *Server) Generate(
	ctx context.Context,
	request *daedalusv1.GenerateRequest,
) (*daedalusv1.GenerateResponse, error) {
	if request == nil || request.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "solicitação deve conter config")
	}
	if err := checkHardLimits(request.Config); err != nil {
		return nil, StatusError(err)
	}
	config, err := ConfigFromProto(request.Config)
	if err != nil {
		return nil, StatusError(err)
	}
	if err := server.admission.acquire(ctx); err != nil {
		if errors.Is(err, errAdmissionStopped) {
			return nil, status.Error(codes.Unavailable, "serviço em desligamento")
		}
		return nil, StatusError(err)
	}
	defer server.admission.release()

	generationContext, cancel := context.WithCancel(ctx)
	stopShutdownCancellation := context.AfterFunc(server.context, cancel)
	defer func() {
		stopShutdownCancellation()
		cancel()
	}()

	layout, err := server.generate(generationContext, config)
	if err != nil {
		return nil, StatusError(err)
	}
	return &daedalusv1.GenerateResponse{Layout: LayoutToProto(layout)}, nil
}

// BeginShutdown marca o serviço como não atendendo, interrompe admissões e
// cancela o trabalho em curso. É idempotente.
func (server *Server) BeginShutdown() {
	if server.serving.Swap(false) {
		server.admission.Stop()
		server.cancel()
	}
}

// Serving informa se o serviço ainda aceita novas solicitações.
func (server *Server) Serving() bool {
	return server.serving.Load()
}

// Wait aguarda as gerações já admitidas terminarem.
func (server *Server) Wait(ctx context.Context) error {
	return server.admission.Wait(ctx)
}

// StatusError converte categorias do SDK e Context em status gRPC sem
// revelar detalhes internos.
func StatusError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "prazo da geração excedido")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "geração cancelada")
	case errors.Is(err, daedalus.ErrInvalidConfig):
		return status.Error(codes.InvalidArgument, "configuração inválida")
	case errors.Is(err, daedalus.ErrLimitExceeded):
		return status.Error(codes.ResourceExhausted, "limite de recursos excedido")
	case errors.Is(err, daedalus.ErrNoCompatiblePlant):
		// A especificação fixa HTTP 422, mas não o status gRPC. A leitura
		// conservadora usa FailedPrecondition, seu análogo mais próximo.
		return status.Error(codes.FailedPrecondition, "nenhuma plant compatível")
	case errors.Is(err, daedalus.ErrUnroutableEdge):
		// Mesma interpretação conservadora de ErrNoCompatiblePlant.
		return status.Error(codes.FailedPrecondition, "aresta sem rota ortogonal")
	default:
		return status.Error(codes.Internal, "falha interna ao gerar layout")
	}
}

func checkHardLimits(config *daedalusv1.Config) error {
	if config.Width > maximumGridDimension || config.Height > maximumGridDimension {
		return fmt.Errorf("%w: dimensão do grid excede o máximo v1", daedalus.ErrLimitExceeded)
	}
	cellCount := uint64(config.Width) * uint64(config.Height)
	if cellCount > uint64(daedalus.MaxCells) {
		return fmt.Errorf("%w: quantidade de cells excede o máximo v1", daedalus.ErrLimitExceeded)
	}
	if config.MaxRooms > daedalus.MaxRooms {
		return fmt.Errorf("%w: max_rooms excede o máximo v1", daedalus.ErrLimitExceeded)
	}
	if config.RoomGeometry != nil &&
		config.RoomGeometry.MaxFootprintCells > daedalus.MaxFootprintCells {
		return fmt.Errorf("%w: max_footprint_cells excede o máximo v1", daedalus.ErrLimitExceeded)
	}
	return nil
}
