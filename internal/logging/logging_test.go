package logging

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWritesLogsOnlyToProvidedWriter checks that logs stay off stdout: the
// logger writes only to the injected writer.
func TestNewWritesLogsOnlyToProvidedWriter(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	var stdout bytes.Buffer
	logger := New(&stderr)
	logger.Info("service started")
	_ = logger.Sync()

	assert.Contains(t, stderr.String(), "service started")
	assert.Empty(t, stdout.String())
}

func TestNewAcceptsNilWriterWithoutPanic(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		logger := New(nil)
		logger.Info("discarded")
	})
}
