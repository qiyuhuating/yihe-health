// Command openapi exports exactly the document served by the Go application.
// It does not start a server or connect to a database.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"yihe-care/internal/apidoc"
)

func main() {
	w := httptest.NewRecorder()
	apidoc.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		fmt.Fprintln(os.Stderr, "OpenAPI export failed")
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(w.Body.Bytes()); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write OpenAPI document")
		os.Exit(1)
	}
}
