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
	directory, files := collectRootProductionFiles(t)
	if len(files) == 0 {
		t.Fatal("daedalus production package not found")
	}
	for _, fileName := range files {
		for _, importPath := range readImportPaths(t, directory, fileName) {
			assertStandardLibraryImport(t, directory, fileName, importPath)
		}
	}
}

// collectRootProductionFiles lists non-test Go files in the repository root
// that match the default build constraints.
func collectRootProductionFiles(t *testing.T) (string, []string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate this test file")
	}
	directory := filepath.Dir(thisFile)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("list root production files: %v", err)
	}

	var files []string
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
		files = append(files, fileName)
	}
	return directory, files
}

// readImportPaths parses one production file and returns its import paths.
func readImportPaths(t *testing.T, directory, fileName string) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filepath.Join(directory, fileName), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports from %s: %v", fileName, err)
	}
	paths := make([]string, 0, len(file.Imports))
	for _, importSpec := range file.Imports {
		importPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			t.Fatalf("interpret import in %s: %v", fileName, err)
		}
		paths = append(paths, importPath)
	}
	return paths
}

// assertStandardLibraryImport fails when an import does not resolve inside GOROOT.
func assertStandardLibraryImport(t *testing.T, directory, fileName, importPath string) {
	t.Helper()
	importedPackage, err := build.Default.Import(importPath, directory, build.FindOnly)
	if err != nil || !importedPackage.Goroot {
		t.Errorf("%s imports package outside the standard library: %q", filepath.Base(fileName), importPath)
	}
}
