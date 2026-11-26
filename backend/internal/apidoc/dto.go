package apidoc

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"yihe-care/internal/health"
)

// The latest-health response serializes health.Record directly. Derive its
// fields, requiredness and nullability from that same type instead of maintaining
// an independent frontend DTO. Unsupported representations fail the export.
func applyGoDTOs(paths map[string]map[string]object) error {
	operation := paths["/api/v1/health/latest"]["post"]
	properties, err := nestedObject(operation, "responses", "200", "content", "application/json", "schema", "properties")
	if err != nil {
		return err
	}
	schema, err := jsonTypeSchema(reflect.TypeFor[health.Record](), map[reflect.Type]bool{})
	if err != nil {
		return err
	}
	properties["items"] = object{"type": "array", "items": schema}
	return nil
}

func nestedObject(value object, keys ...string) (object, error) {
	for _, key := range keys {
		next, ok := value[key].(object)
		if !ok {
			return nil, fmt.Errorf("missing OpenAPI object: %s", key)
		}
		value = next
	}
	return value, nil
}

func jsonTypeSchema(t reflect.Type, stack map[reflect.Type]bool) (object, error) {
	if t == reflect.TypeFor[time.Time]() {
		return object{"type": "string", "format": "date-time"}, nil
	}
	for _, marshaler := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()} {
		if t.Implements(marshaler) || reflect.PointerTo(t).Implements(marshaler) {
			return nil, fmt.Errorf("custom JSON representation needs an explicit schema: %s", t)
		}
	}
	if stack[t] {
		return nil, fmt.Errorf("recursive JSON DTO needs an explicit schema: %s", t)
	}
	stack[t] = true
	defer delete(stack, t)
	if t.Kind() == reflect.Pointer {
		value, err := jsonTypeSchema(t.Elem(), stack)
		return object{"anyOf": []object{value, {"type": "null"}}}, err
	}
	switch t.Kind() {
	case reflect.String:
		return object{"type": "string"}, nil
	case reflect.Bool:
		return object{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return object{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return object{"type": "number"}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("JSON DTO map keys must be strings: %s", t)
		}
		value, err := jsonTypeSchema(t.Elem(), stack)
		return object{"anyOf": []object{{"type": "object", "additionalProperties": value}, {"type": "null"}}}, err
	case reflect.Struct:
		properties := object{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if field.Anonymous {
				return nil, fmt.Errorf("embedded JSON DTO needs explicit field resolution: %s", t)
			}
			if name == "" {
				name = field.Name
			}
			optional := false
			for _, option := range tag[1:] {
				if option != "omitempty" {
					return nil, fmt.Errorf("unsupported JSON DTO tag: %s.%s", t, field.Name)
				}
				// encoding/json does not omit non-pointer structs for omitempty.
				optional = field.Type.Kind() != reflect.Struct
			}
			if _, exists := properties[name]; exists {
				return nil, fmt.Errorf("duplicate JSON DTO property: %s", name)
			}
			value, err := jsonTypeSchema(field.Type, stack)
			if err != nil {
				return nil, err
			}
			if optional && field.Type.Kind() == reflect.Map {
				// A nil map is omitted here; an emitted map cannot be null.
				value = value["anyOf"].([]object)[0]
			}
			properties[name] = value
			if !optional {
				required = append(required, name)
			}
		}
		return object{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, nil
	default:
		return nil, fmt.Errorf("JSON DTO needs an explicit schema for %s", t)
	}
}
