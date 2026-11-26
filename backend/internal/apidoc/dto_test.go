package apidoc

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"yihe-care/internal/health"
)

func TestLatestDTORequiredFieldsMatchActualJSON(t *testing.T) {
	schema, err := jsonTypeSchema(reflect.TypeFor[health.Record](), map[reflect.Type]bool{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(health.Record{})
	if err != nil {
		t.Fatal(err)
	}
	var value object
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	var compare func(object, object)
	compare = func(schema, value object) {
		t.Helper()
		required := schema["required"].([]string)
		properties := schema["properties"].(object)
		for key := range value {
			if !slices.Contains(required, key) {
				t.Errorf("JSON field %s is emitted even for the zero value but is not required", key)
			}
			if nested, ok := value[key].(map[string]any); ok {
				compare(properties[key].(object), nested)
			}
		}
		for _, key := range required {
			if _, ok := value[key]; !ok {
				t.Errorf("required field %s is absent from JSON", key)
			}
		}
	}
	compare(schema, value)
	properties := schema["properties"].(object)
	if alternatives, ok := properties["deviceId"].(object)["anyOf"].([]object); !ok || alternatives[1]["type"] != "null" {
		t.Fatal("unbound deviceId must allow null")
	}
	sample := properties["sample"].(object)
	if slices.Contains(sample["required"].([]string), "sourceVersion") || !slices.Contains(sample["required"].([]string), "value") {
		t.Fatal("omitempty and required nullable sample values differ from Go JSON")
	}
	metadata := sample["properties"].(object)["metadata"].(object)
	if metadata["additionalProperties"].(object)["type"] != "string" {
		t.Fatal("metadata values must retain the Go string-map contract")
	}
}

type customJSON string

func (customJSON) MarshalJSON() ([]byte, error) { return []byte(`42`), nil }

func TestDTOExporterRejectsUnsupportedJSONRepresentations(t *testing.T) {
	type quoted struct {
		Value int64 `json:"value,string"`
	}
	type recursive struct {
		Next *recursive `json:"next"`
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[quoted](), reflect.TypeFor[recursive](), reflect.TypeFor[map[int]string](), reflect.TypeFor[customJSON]()} {
		if _, err := jsonTypeSchema(typ, map[reflect.Type]bool{}); err == nil {
			t.Errorf("silently accepted unsupported DTO: %s", typ)
		}
	}
}
