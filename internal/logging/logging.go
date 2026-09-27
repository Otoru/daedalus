// Package logging configura logs do subprocesso exclusivamente em stderr.
package logging

import (
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New cria um logger JSON sem qualquer referência a stdout.
func New(stderr io.Writer) *zap.Logger {
	if stderr == nil {
		stderr = io.Discard
	}
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(stderr), zapcore.InfoLevel)
	return zap.New(core)
}
