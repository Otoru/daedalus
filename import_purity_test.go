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

// TestRootPackageImportsOnlyStandardLibrary keeps the generation core
// independently importable: grpc, protobuf, fx, zap, and generated bindings
// live outside this package. Analysis uses the AST (go/parser), and each import
// is resolved with go/build; any import outside GOROOT fails the test.
func TestRootPackageImportsOnlyStandardLibrary(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate this test file")
	}
	directory := filepath.Dir(thisFile)
	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("list root production files: %v", err)
	}

	foundProductionFile := false
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		matchesBuild, err := build.Default.MatchFile(directory, fileName)
		if err != nil {
			t.Fatalf("evaluate build constraints for %s: %v", fileName, err)
		}
		if !matchesBuild {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(directory, fileName), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports from %s: %v", fileName, err)
		}
		foundProductionFile = true
		for _, importSpec := range file.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatalf("interpret import in %s: %v", fileName, err)
			}
			importedPackage, err := build.Default.Import(importPath, directory, build.FindOnly)
			if err != nil || !importedPackage.Goroot {
				t.Errorf("%s imports package outside the standard library: %q", filepath.Base(fileName), importPath)
			}
		}
	}
	if !foundProductionFile {
		t.Fatal("daedalus production package not found")
	}
}
