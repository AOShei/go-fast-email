package gofastemail_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// imports returns the import paths of every non-test Go file under dir,
// by file.
func imports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != dir {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range parsed.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			out[path] = append(out[path], p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The module depends on the standard library and go-fast modules only.
func TestImportsStandardLibraryAndGoFastOnly(t *testing.T) {
	files := imports(t, ".")
	if len(files) < 15 {
		t.Fatalf("only %d files were checked", len(files))
	}
	for file, paths := range files {
		for _, p := range paths {
			first, _, _ := strings.Cut(p, "/")
			if strings.Contains(first, ".") && !strings.HasPrefix(p, "github.com/AOShei/go-fast-") {
				t.Errorf("%s imports %q", file, p)
			}
		}
	}
}

// The packages that read a message work on the bytes they are given and
// on nothing else: they open no file, start no program and reach no
// network, because they do not import the packages that would let them.
// Only the public package opens a file, in ExtractFile.
func TestReadersImportNothingThatReachesOutside(t *testing.T) {
	files := imports(t, "internal")
	for file, paths := range files {
		if strings.Contains(file, "msgtest") {
			continue
		}
		for _, p := range paths {
			for _, banned := range []string{"os", "net", "syscall", "unsafe", "plugin", "runtime/debug"} {
				if p == banned || strings.HasPrefix(p, banned+"/") {
					t.Errorf("%s imports %q", file, p)
				}
			}
		}
	}
}
