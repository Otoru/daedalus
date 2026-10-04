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

const (
	// rootPackage is the import path of the root SDK package.
	rootPackage = "github.com/Otoru/daedalus"
	// corePackage is the import path of the perspective-agnostic core.
	corePackage = "github.com/Otoru/daedalus/core"
)

// purePackages lists every package whose imports are policed, with the
// non-GOROOT import paths each one is permitted. core permits none: it is the
// bottom of the dependency order and may import only the standard library. The
// root permits core and nothing else, so the generation core stays
// independently importable and a sibling generator can share the frozen
// primitives without depending on the dungeon vocabulary. platform permits
// core alone: the policy admits the root there as well, but the package does
// not need it, and the narrower allowlist is the one that actually bites — a
// later front that genuinely needs the dungeon vocabulary widens this entry
// deliberately instead of inheriting the permission. utils/pathfinding,
// utils/vision and utils/gating each permit the root and core: grpc, protobuf,
// fx, and zap stay outside them.
//
// A package absent from this list is not checked at all, and the scan is not
// recursive, so every new policed directory needs an explicit entry here.
var purePackages = []struct {
	directory string   // relative to the repository root
	allowed   []string // import paths permitted outside GOROOT
}{
	{directory: "core", allowed: nil},
	{directory: ".", allowed: []string{corePackage}},
	{directory: "platform", allowed: []string{corePackage}},
	{directory: "utils/pathfinding", allowed: []string{rootPackage, corePackage}},
	{directory: "utils/vision", allowed: []string{rootPackage, corePackage}},
	{directory: "utils/gating", allowed: []string{rootPackage, corePackage}},
}

// TestRootPackageImportsOnlyStandardLibrary keeps the generation core
// independently importable. Each entry in purePackages is checked as a
// subtest: core permits only the standard library, the root and platform
// permit the standard library and core, and utils/pathfinding, utils/vision
// and utils/gating may also import the root. grpc, protobuf, fx, zap, and
// generated bindings stay outside all of them. Analysis uses the AST
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
// refused at core and at the root and accepted by the utils packages, core is
// accepted by the root and by the utils packages and refused by core itself,
// and a standard-library path is accepted everywhere.
func TestAllowedImport(t *testing.T) {
	coreList := allowlist(t, "core")
	root := allowlist(t, ".")
	platform := allowlist(t, "platform")
	pathfinding := allowlist(t, "utils/pathfinding")
	vision := allowlist(t, "utils/vision")
	gating := allowlist(t, "utils/gating")
	cases := []struct {
		name       string
		importPath string
		allowed    []string
		want       bool
	}{
		{name: "grpc rejected by core", importPath: "google.golang.org/grpc", allowed: coreList, want: false},
		{name: "grpc rejected by root", importPath: "google.golang.org/grpc", allowed: root, want: false},
		{name: "grpc rejected by platform", importPath: "google.golang.org/grpc", allowed: platform, want: false},
		{name: "grpc rejected by pathfinding", importPath: "google.golang.org/grpc", allowed: pathfinding, want: false},
		{name: "grpc rejected by vision", importPath: "google.golang.org/grpc", allowed: vision, want: false},
		{name: "grpc rejected by gating", importPath: "google.golang.org/grpc", allowed: gating, want: false},
		{name: "root module rejected by core", importPath: rootPackage, allowed: coreList, want: false},
		{name: "root module rejected by root", importPath: rootPackage, allowed: root, want: false},
		{name: "root module rejected by platform", importPath: rootPackage, allowed: platform, want: false},
		{name: "root module accepted by pathfinding", importPath: rootPackage, allowed: pathfinding, want: true},
		{name: "root module accepted by vision", importPath: rootPackage, allowed: vision, want: true},
		{name: "root module accepted by gating", importPath: rootPackage, allowed: gating, want: true},
		{name: "core rejected by core", importPath: corePackage, allowed: coreList, want: false},
		{name: "core accepted by root", importPath: corePackage, allowed: root, want: true},
		{name: "core accepted by platform", importPath: corePackage, allowed: platform, want: true},
		{name: "core accepted by pathfinding", importPath: corePackage, allowed: pathfinding, want: true},
		{name: "core accepted by vision", importPath: corePackage, allowed: vision, want: true},
		{name: "core accepted by gating", importPath: corePackage, allowed: gating, want: true},
		{name: "stdlib accepted by core", importPath: "fmt", allowed: coreList, want: true},
		{name: "stdlib accepted by root", importPath: "fmt", allowed: root, want: true},
		{name: "stdlib accepted by platform", importPath: "fmt", allowed: platform, want: true},
		{name: "stdlib accepted by pathfinding", importPath: "fmt", allowed: pathfinding, want: true},
		{name: "stdlib accepted by vision", importPath: "fmt", allowed: vision, want: true},
		{name: "stdlib accepted by gating", importPath: "fmt", allowed: gating, want: true},
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

// TestEveryGenerationPackageIsPoliced fails when a new package of production
// Go files appears outside cmd/ and internal/ without an entry in
// purePackages. The import scan is not recursive and does not discover
// directories by itself, so a package that was never listed is checked by
// nothing and the suite stays green.
func TestEveryGenerationPackageIsPoliced(t *testing.T) {
	root, ok := repositoryRoot()
	if !ok {
		t.Fatal("locate this test file")
	}
	listed := make(map[string]bool, len(purePackages))
	for _, pure := range purePackages {
		listed[pure.directory] = true
	}
	var missing []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		return checkGenerationDirectory(root, path, entry, walkErr, listed, &missing)
	})
	if err != nil {
		t.Fatalf("walk generation packages: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("production packages with no purePackages entry (the purity test checks nothing there): %s", strings.Join(missing, ", "))
	}
}

func checkGenerationDirectory(root, path string, entry os.DirEntry, walkErr error, listed map[string]bool, missing *[]string) error {
	if walkErr != nil {
		return walkErr
	}
	if !entry.IsDir() {
		return nil
	}
	name := entry.Name()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel != "." && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "bin") {
		return filepath.SkipDir
	}
	if rel == "cmd" || rel == "internal" || strings.HasPrefix(rel, "cmd"+string(filepath.Separator)) || strings.HasPrefix(rel, "internal"+string(filepath.Separator)) {
		return filepath.SkipDir
	}
	if directoryHasProductionGo(path) && !listed[rel] {
		*missing = append(*missing, rel)
	}
	return nil
}

func directoryHasProductionGo(directory string) bool {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		return true
	}
	return false
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
