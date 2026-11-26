// Package apidoc serves the native Go HTTP contract.
package apidoc

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
)

//go:embed openapi-template.json
var schemaTemplate []byte

//go:embed contracts.json
var contractInventory []byte

type object = map[string]any

type document struct {
	OpenAPI    string                       `json:"openapi"`
	Info       json.RawMessage              `json:"info"`
	Components json.RawMessage              `json:"components"`
	Paths      map[string]map[string]object `json:"paths"`
}

var schema = sync.OnceValues(buildSchema)

func buildSchema() ([]byte, error) {
	var template struct {
		document
		OperationDefaults object `json:"operationDefaults"`
		WriteDefaults     object `json:"writeDefaults"`
	}
	decoder := json.NewDecoder(bytes.NewReader(schemaTemplate))
	decoder.UseNumber()
	if err := decoder.Decode(&template); err != nil {
		return nil, err
	}
	var contracts map[string]struct {
		Request  []string `json:"request"`
		Response []string `json:"response"`
	}
	if err := json.Unmarshal(contractInventory, &contracts); err != nil {
		return nil, err
	}
	for path, methods := range template.Paths {
		for method, patch := range methods {
			key := strings.ToUpper(method) + " " + path
			contract, ok := contracts[key]
			if !ok {
				return nil, fmt.Errorf("missing API field inventory for %s", key)
			}
			operation := maps.Clone(template.OperationDefaults)
			if method != "get" {
				maps.Copy(operation, template.WriteDefaults)
			}
			operation["summary"] = strings.TrimPrefix(path, "/api/v1/")
			applyOperationPatch(operation, patch)
			operation["x-request-fields"] = contract.Request
			operation["x-success-fields"] = contract.Response
			methods[method] = operation
		}
	}
	if err := applyGoDTOs(template.Paths); err != nil {
		return nil, err
	}
	return json.Marshal(template.document)
}

// applyOperationPatch copies inherited objects before editing them so sibling
// operations can reuse the same defaults.
func applyOperationPatch(operation, patch object) {
	for key, value := range patch {
		switch value := value.(type) {
		case nil:
			delete(operation, key)
		case object:
			base, _ := operation[key].(object)
			merged := make(object, len(base)+len(value))
			maps.Copy(merged, base)
			applyOperationPatch(merged, value)
			operation[key] = merged
		default:
			operation[key] = value
		}
	}
}

func ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	data, err := schema()
	if err != nil {
		http.Error(w, "schema unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}
