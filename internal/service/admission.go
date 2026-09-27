package service

import (
	"context"
	"errors"
	"sync"
)

var errAdmissionStopped = errors.New("admissão de gerações interrompida")

// Admission limita gerações simultâneas e pode ser compartilhada pelos
// transportes gRPC e HTTP. Cada processo deve criar exatamente uma instância.
type Admission struct {
	tokens    chan struct{}
	stopped   chan struct{}
	mutex     sync.Mutex
	accepting bool
	stopOnce  sync.Once
	active    sync.WaitGroup
}

// NewAdmission cria um limitador com a concorrência positiva informada.
func NewAdmission(limit int) *Admission {
	if limit < 1 {
		panic("limite de admissão deve ser maior que zero")
	}
	return &Admission{
		tokens:    make(chan struct{}, limit),
		stopped:   make(chan struct{}),
		accepting: true,
	}
}

func (admission *Admission) acquire(ctx context.Context) error {
	select {
	case <-admission.stopped:
		return errAdmissionStopped
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case admission.tokens <- struct{}{}:
		admission.mutex.Lock()
		if !admission.accepting {
			admission.mutex.Unlock()
			<-admission.tokens
			return errAdmissionStopped
		}
		// Add ocorre sob o mesmo mutex de Stop; depois que Stop retorna,
		// Wait nunca disputa com um Add tardio sobre contador zero.
		admission.active.Add(1)
		admission.mutex.Unlock()
		return nil
	case <-admission.stopped:
		return errAdmissionStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (admission *Admission) release() {
	<-admission.tokens
	admission.active.Done()
}

// Stop interrompe novas admissões e desperta chamadas que aguardam vaga.
// Chamadas já admitidas continuam até que seus Contexts sejam cancelados.
func (admission *Admission) Stop() {
	admission.stopOnce.Do(func() {
		admission.mutex.Lock()
		admission.accepting = false
		close(admission.stopped)
		admission.mutex.Unlock()
	})
}

// Wait aguarda todas as gerações admitidas terminarem ou o Context expirar.
func (admission *Admission) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		admission.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
