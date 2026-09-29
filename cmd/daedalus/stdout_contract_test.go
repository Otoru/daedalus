package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBinaryEmitsExactlyOneLineOnRealStdout checks the real process stdout:
// startup writes exactly one handshake line, so logs cannot land there.
//
// It runs the actual subprocess, and not an fx composition with an injected
// writer.
//
// The other tests in this package pass a lockedBuffer to newApp and prove
// that *that* writer receives a single line. That does not prove the process
// contract. Stdout is the wire the client reads before it connects, and a
// fmt.Println anywhere in the binary writes directly to os.Stdout and escapes
// the injected buffer, breaking the handshake without failing a buffer-injected
// test.
func TestBinaryEmitsExactlyOneLineOnRealStdout(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "daedalus-contract")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "compile the subprocess")

	processContext, cancelProcess := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelProcess()
	command := exec.CommandContext(processContext, binary)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())

	reader := bufio.NewReader(stdout)
	firstLine, err := reader.ReadString('\n')
	require.NoError(t, err, "the process must publish a complete handshake line")

	// Allow an improper second write to reach the pipe before stopping the
	// process, then drain the pipe without racing the handshake write.
	time.Sleep(100 * time.Millisecond)
	cancelProcess()
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	_ = command.Wait()
	produced := firstLine + string(rest)

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
