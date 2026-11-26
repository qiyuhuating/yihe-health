package rekey

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

type failingCloseDriver struct{}
type failingCloseConnection struct{ large bool }
type failingCloseRows struct {
	sent  int
	large bool
}

var errRowsClose = errors.New("synthetic SQL stream close failure")

func (failingCloseDriver) Open(name string) (driver.Conn, error) {
	return &failingCloseConnection{large: name == "byte-budget"}, nil
}
func (*failingCloseConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not used")
}
func (*failingCloseConnection) Close() error              { return nil }
func (*failingCloseConnection) Begin() (driver.Tx, error) { return nil, errors.New("not used") }
func (c *failingCloseConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &failingCloseRows{large: c.large}, nil
}
func (*failingCloseRows) Columns() []string { return []string{"id", "value", "oversized"} }
func (*failingCloseRows) Close() error      { return errRowsClose }
func (r *failingCloseRows) Next(dest []driver.Value) error {
	limit := 1
	if r.large {
		limit = 2
	}
	if r.sent >= limit {
		return io.EOF
	}
	r.sent++
	text := "synthetic"
	if r.large {
		text = strings.Repeat("x", maxPageBytes/2)
	}
	dest[0], dest[1], dest[2] = int64(r.sent), text, false
	return nil
}

func init() { sql.Register("rekey-close-failure", failingCloseDriver{}) }

func TestReadPageRejectsCloseErrorsBeforeReturningCandidates(t *testing.T) {
	for _, mode := range []string{"eof", "byte-budget"} {
		t.Run(mode, func(t *testing.T) {
			pool, err := sql.Open("rekey-close-failure", mode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			})
			page, err := readPage(context.Background(), pool, "synthetic", nil, 1)
			if !errors.Is(err, errRowsClose) || page != nil {
				t.Fatal("failed SQL stream was returned as update candidates", err)
			}
		})
	}
}
