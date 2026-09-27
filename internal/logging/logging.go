// Package logging configures subprocess logs exclusively on stderr.
// stdout carries exactly one line, the handshake JSON, and a log must
// never be written there.
package logging

import (
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a JSON logger with no reference to stdout.
func New(stderr io.Writer) *zap.Logger {
	if stderr == nil {
		stderr = io.Discard
	}
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(stderr), zapcore.InfoLevel)
	return zap.New(core)
}
