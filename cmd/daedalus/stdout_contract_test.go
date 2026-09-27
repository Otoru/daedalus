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

// TestBinarioEmiteExatamenteUmaLinhaNoStdoutReal executa o subprocesso de
// verdade, e não uma composição fx com writer injetado.
//
// Os demais testes deste pacote passam um bytes.Buffer para newApp e provam
// que *aquele* writer recebe uma única linha. Isso não cobre o contrato da
// seção 2.1, que é sobre o stdout do processo: um fmt.Println em qualquer
// ponto do binário escreve direto em os.Stdout e escapa do buffer injetado,
// quebrando o handshake para o cliente sem reprovar nenhum teste.
func TestBinarioEmiteExatamenteUmaLinhaNoStdoutReal(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "daedalus-contrato")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "compilar o subprocesso")

	command := exec.Command(binary)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()

	// Dá tempo de o ciclo de vida completar e de qualquer escrita indevida
	// aparecer, antes de encerrar e ler tudo o que o processo produziu.
	time.Sleep(2 * time.Second)
	require.NoError(t, command.Process.Kill())
	produzido, err := readAllAvailable(stdout)
	require.NoError(t, err)

	linhas := strings.Split(strings.TrimSuffix(produzido, "\n"), "\n")
	require.Len(t, linhas, 1, "stdout é contrato de fio: só o handshake pode ser escrito nele, veio %q", produzido)
	assert.True(t, strings.HasSuffix(produzido, "\n"), "o handshake termina em quebra de linha")

	var anunciado handshake
	require.NoError(t, json.Unmarshal([]byte(linhas[0]), &anunciado), "a única linha precisa ser o handshake JSON")
	assert.NotEmpty(t, anunciado.Transport)
	assert.NotEmpty(t, anunciado.Addr)
	assert.Positive(t, anunciado.PID)
	assert.NotEmpty(t, anunciado.Version)
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
