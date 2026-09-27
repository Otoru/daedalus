package logging

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEscreveLogsSomenteNoWriterInformado(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	logger := New(&stderr)
	logger.Info("serviço iniciado")
	_ = logger.Sync()

	assert.Contains(t, stderr.String(), "serviço iniciado")
	assert.Empty(t, stdout.String())
}

func TestNewAceitaWriterNilSemPanico(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		logger := New(nil)
		logger.Info("descartado")
	})
}
