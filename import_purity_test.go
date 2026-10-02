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

// purePackages lists every package whose imports are policed, with the
// non-GOROOT import paths each one is permitted. The root permits none.
// utils/pathfinding, utils/vision, and utils/gating each permit only the root
// outside GOROOT: grpc, protobuf, fx, and zap stay outside them.
var purePackages = []struct {
	directory string   // relative to the repository root
	allowed   []string // import paths permitted outside GOROOT
}{
	{directory: ".", allowed: nil},
	{directory: "utils/pathfinding", allowed: []string{"github.com/Otoru/daedalus"}},
	{directory: "utils/vision", allowed: []string{"github.com/Otoru/daedalus"}},
	{directory: "utils/gating", allowed: []string{"github.com/Otoru/daedalus"}},
}

// TestRootPackageImportsOnlyStandardLibrary keeps the generation core
// independently importable. Each entry in purePackages is checked as a
// subtest: the root permits only the standard library, and utils/pathfinding
// and utils/vision and utils/gating may also import the root module. grpc,
// protobuf, fx, zap, and generated bindings stay outside all four. Analysis uses the AST
// (go/parser). An import is accepted when it is on that package's allowlist
// or when go/build resolves it inside GOROOT.
func TestRootPackageImportsOnlyStandardLibrary(t *testing.T) {
	for _, pure := range purePackages {
		t.Run(pure.directory, func(t *testing.T) {
			directory, files := collectProductionFiles(t, pure.directory)
			if len(files) == 0 {
				t.Fatalf("no production Go files in %s", pure.directory)
			}
			for _, fileName := range files {
				for _, importPath := range readImportPaths(t, directory, fileName) {
					assertPermittedImport(t, fileName, importPath, pure.allowed)
				}
			}
		})
	}
}

// TestAllowedImport locks the allowlist decision itself. A scan of the tree
// can stay green while the assertion is a tautology, because every import
// that exists today is legitimate. These cases are the ones the tree does
// not contain: grpc is refused by every policed package, the root module is
// refused at the root and accepted by utils/pathfinding and utils/vision,
// and a standard-library path is accepted by all three.
func TestAllowedImport(t *testing.T) {
	root := allowlist(t, ".")
	pathfinding := allowlist(t, "utils/pathfinding")
	vision := allowlist(t, "utils/vision")
	cases := []struct {
		name       string
		importPath string
		allowed    []string
		want       bool
	}{
		{name: "grpc rejected by root", importPath: "google.golang.org/grpc", allowed: root, want: false},
		{name: "grpc rejected by pathfinding", importPath: "google.golang.org/grpc", allowed: pathfinding, want: false},
		{name: "grpc rejected by vision", importPath: "google.golang.org/grpc", allowed: vision, want: false},
		{name: "root module rejected by root", importPath: "github.com/Otoru/daedalus", allowed: root, want: false},
		{name: "root module accepted by pathfinding", importPath: "github.com/Otoru/daedalus", allowed: pathfinding, want: true},
		{name: "root module accepted by vision", importPath: "github.com/Otoru/daedalus", allowed: vision, want: true},
		{name: "stdlib accepted by root", importPath: "fmt", allowed: root, want: true},
		{name: "stdlib accepted by pathfinding", importPath: "fmt", allowed: pathfinding, want: true},
		{name: "stdlib accepted by vision", importPath: "fmt", allowed: vision, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowedImport(tc.importPath, tc.allowed); got != tc.want {
				t.Fatalf("allowedImport(%q) = %v, want %v", tc.importPath, got, tc.want)
			}
		})
	}
}

func allowlist(t *testing.T, directory string) []string {
	t.Helper()
	for _, pure := range purePackages {
		if pure.directory == directory {
			return pure.allowed
		}
	}
	t.Fatalf("purePackages has no entry for %s", directory)
	return nil
}

// allowedImport reports whether importPath may appear in a package whose
// non-GOROOT allowlist is allowed. A listed path is accepted before any
// GOROOT lookup. Every other path must resolve inside GOROOT.
func allowedImport(importPath string, allowed []string) bool {
	for _, permitted := range allowed {
		if importPath == permitted {
			return true
		}
	}
	root, ok := repositoryRoot()
	if !ok {
		return false
	}
	imported, err := build.Default.Import(importPath, root, build.FindOnly)
	return err == nil && imported.Goroot
}

// collectProductionFiles lists non-test Go files in directory (relative to
// the repository root) that match the default build constraints.
func collectProductionFiles(t *testing.T, relative string) (string, []string) {
	t.Helper()
	root, ok := repositoryRoot()
	if !ok {
		t.Fatal("locate this test file")
	}
	directory := filepath.Join(root, relative)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("list production files in %s: %v", relative, err)
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

func repositoryRoot() (string, bool) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	return filepath.Dir(thisFile), true
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

// assertPermittedImport fails when an import is neither on the package
// allowlist nor resolved inside GOROOT.
func assertPermittedImport(t *testing.T, fileName, importPath string, allowed []string) {
	t.Helper()
	if allowedImport(importPath, allowed) {
		return
	}
	t.Errorf("%s imports package outside the standard library: %q", filepath.Base(fileName), importPath)
}
