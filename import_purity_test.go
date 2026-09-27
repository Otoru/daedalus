package daedalus

import (
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestPacoteRaizImportaApenasBibliotecaPadrao mantém o núcleo de geração
// importável de forma independente: grpc, protobuf, fx, zap e os bindings
// gerados vivem fora deste pacote. A análise é feita por AST (go/parser) e
// cada import é resolvido com go/build; qualquer import fora de GOROOT falha
// o teste.
func TestPacoteRaizImportaApenasBibliotecaPadrao(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("localizar este arquivo de teste")
	}
	directory := filepath.Dir(thisFile)
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("listar arquivos de produção da raiz: %v", err)
	}

	foundProductionFile := false
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		matchesBuild, err := build.Default.MatchFile(directory, fileName)
		if err != nil {
			t.Fatalf("avaliar restrições de build de %s: %v", fileName, err)
		}
		if !matchesBuild {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, fileName), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("analisar imports de %s: %v", fileName, err)
		}
		foundProductionFile = true
		for _, importSpec := range file.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatalf("interpretar import em %s: %v", fileName, err)
			}
			importedPackage, err := build.Default.Import(importPath, directory, build.FindOnly)
			if err != nil || !importedPackage.Goroot {
				t.Errorf("%s importa pacote fora da biblioteca padrão: %q", filepath.Base(fileName), importPath)
			}
		}
	}
	if !foundProductionFile {
		t.Fatal("pacote de produção daedalus não encontrado")
	}
}
