package db_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// PostgreSQL belongs to the independent analytics projection. Business SQL
// must reach database/sql in MySQL syntax, with no runtime dialect compiler.
func TestBusinessSQLUsesNativeMySQL(t *testing.T) {
	root := filepath.Join("..", "..")
	parameter := regexp.MustCompile(`\$[0-9]+`)
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
				strings.HasPrefix(rel, "internal/analytics/") || strings.HasPrefix(rel, "cmd/rekey-projection/") || rel == "internal/rekey/projection_pg.go" {
				return nil
			}
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				if strings.HasPrefix(value, "$2a$") || strings.HasPrefix(value, "$2b$") || strings.HasPrefix(value, "$2y$") {
					return true
				}
				if parameter.MatchString(value) || strings.Contains(value, "$%d") {
					t.Errorf("%s: PostgreSQL parameter in business source", positions.Position(literal.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
