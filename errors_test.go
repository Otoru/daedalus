package daedalus

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrosSentinelaSuportamErrorsIs garante que cada categoria de erro do
// contrato (Apêndice B da especificação) pode ser reconhecida com errors.Is
// mesmo depois de envolvida com contexto via fmt.Errorf e %w.
func TestErrosSentinelaSuportamErrorsIs(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrLimitExceeded", ErrLimitExceeded},
		{"ErrNoCompatiblePlant", ErrNoCompatiblePlant},
		{"ErrUnroutableEdge", ErrUnroutableEdge},
	}
	for _, tc := range sentinels {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("%s é nil", tc.name)
			}
			wrapped := fmt.Errorf("gerar layout: %w", tc.err)
			if !errors.Is(wrapped, tc.err) {
				t.Errorf("errors.Is não reconheceu %s após envelopamento", tc.name)
			}
		})
	}

	if errors.Is(ErrLimitExceeded, ErrInvalidConfig) {
		t.Error("sentinelas distintos não podem ser equivalentes entre si")
	}
}
