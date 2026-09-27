package daedalus

import (
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
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
	packages, err := parser.ParseDir(fileSet, directory, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("analisar arquivos de produção da raiz: %v", err)
	}

	rootPackage, ok := packages["daedalus"]
	if !ok {
		t.Fatal("pacote de produção daedalus não encontrado")
	}
	for fileName, file := range rootPackage.Files {
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
}
