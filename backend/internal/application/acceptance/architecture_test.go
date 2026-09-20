package acceptance_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDependencyDirection(t *testing.T) {
	for _, layer := range []string{"domain", "application"} {
		err := filepath.WalkDir(filepath.Join("..", "..", layer), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, e := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if e != nil {
				return e
			}
			for _, imp := range file.Imports {
				value, _ := strconv.Unquote(imp.Path.Value)
				forbidden := strings.Contains(value, "/adapters/") || strings.Contains(value, "/internal/httpapi") || strings.Contains(value, "/internal/agent") || strings.Contains(value, "/internal/llm") || strings.Contains(value, "/internal/config")
				if layer == "domain" {
					forbidden = forbidden || value == "net/http" || value == "os" || value == "encoding/json"
				}
				if forbidden {
					t.Errorf("%s imports %s", path, value)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
