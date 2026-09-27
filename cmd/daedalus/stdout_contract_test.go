package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBinaryEmitsExactlyOneLineOnRealStdout covers AC-16 on the real process
// stdout: exactly one handshake line, so logs cannot land there.
//
// It runs the actual subprocess, and not an fx composition with an injected
// writer.
//
// The other tests in this package pass a bytes.Buffer to newApp and prove
// that *that* writer receives a single line. This does not cover the contract
// of section 2.1, which is about the process stdout: a fmt.Println at any
// point in the binary writes directly to os.Stdout and escapes the injected
// buffer, breaking the handshake for the client without failing any test.
func TestBinaryEmitsExactlyOneLineOnRealStdout(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "daedalus-contract")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "compile the subprocess")

	command := exec.Command(binary)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()

	// Gives the lifecycle time to finish and any improper write time to
	// appear, before stopping and reading everything the process produced.
	time.Sleep(2 * time.Second)
	require.NoError(t, command.Process.Kill())
	produced, err := readAllAvailable(stdout)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(produced, "\n"), "\n")
	require.Len(t, lines, 1, "stdout is a wire contract: only the handshake may be written to it, got %q", produced)
	assert.True(t, strings.HasSuffix(produced, "\n"), "the handshake ends with a newline")

	var announced handshake
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &announced), "the only line must be the JSON handshake")
	assert.NotEmpty(t, announced.Transport)
	assert.NotEmpty(t, announced.Addr)
	assert.Positive(t, announced.PID)
	assert.NotEmpty(t, announced.Version)
}

func readAllAvailable(reader interface{ Read([]byte) (int, error) }) (string, error) {
	var builder strings.Builder
	buffer := make([]byte, 4096)
	for {
		read, err := reader.Read(buffer)
		builder.Write(buffer[:read])
		if err != nil {
			return builder.String(), nil
		}
		if read == 0 {
			return builder.String(), nil
		}
	}
}
