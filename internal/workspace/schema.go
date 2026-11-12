package workspace

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Schema implements the closed subset of JSON Schema used by the registry. The
// same definitions are published to MCP clients and used by all command paths.
type Schema struct {
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties bool               `json:"additionalProperties"`
	Items                *Schema            `json:"items,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	MinLength            int                `json:"minLength,omitempty"`
	MaxLength            int                `json:"maxLength,omitempty"`
	MinItems             int                `json:"minItems,omitempty"`
	MaxItems             int                `json:"maxItems,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
}

func Str(description string, max int) *Schema {
	return &Schema{Type: "string", Description: description, MinLength: 1, MaxLength: max}
}
func Enum(values ...string) *Schema    { return &Schema{Type: "string", Enum: values} }
func Number(min, max float64) *Schema  { return &Schema{Type: "number", Minimum: &min, Maximum: &max} }
func Integer(min, max float64) *Schema { s := Number(min, max); s.Type = "integer"; return s }
func Obj(props map[string]*Schema, required ...string) *Schema {
	return &Schema{Type: "object", Properties: props, Required: required}
}
func Array(item *Schema, min, max int) *Schema {
	return &Schema{Type: "array", Items: item, MinItems: min, MaxItems: max}
}
func (s *Schema) Validate(v any, path string) error {
	if len(s.AnyOf) > 0 {
		for _, choice := range s.AnyOf {
			if choice.Validate(v, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: data does not match any supported widget schema", path)
	}
	fail := func(reason string) error { return fmt.Errorf("%s: %s", path, reason) }
	switch s.Type {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fail("expected object")
		}
		for _, k := range s.Required {
			if _, ok := m[k]; !ok {
				return fail("missing " + k)
			}
		}
		for k, v := range m {
			p, ok := s.Properties[k]
			if !ok {
				if !s.AdditionalProperties {
					return fail("unknown field " + k)
				}
				continue
			}
			if err := p.Validate(v, path+"."+k); err != nil {
				return err
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fail("expected array")
		}
		if len(a) < s.MinItems || s.MaxItems > 0 && len(a) > s.MaxItems {
			return fail("invalid array size")
		}
		for i, x := range a {
			if err := s.Items.Validate(x, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		x, ok := v.(string)
		if !ok {
			return fail("expected string")
		}
		n := utf8.RuneCountInString(x)
		if n < s.MinLength || s.MaxLength > 0 && n > s.MaxLength || strings.IndexByte(x, 0) >= 0 {
			return fail("invalid string length/content")
		}
		if len(s.Enum) > 0 {
			for _, e := range s.Enum {
				if x == e {
					return nil
				}
			}
			return fail("expected one of " + strings.Join(s.Enum, ", "))
		}
	case "integer", "number":
		x, ok := v.(float64)
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
			return fail("expected number")
		}
		if s.Type == "integer" && math.Trunc(x) != x {
			return fail("expected integer")
		}
		if s.Minimum != nil && x < *s.Minimum || s.Maximum != nil && x > *s.Maximum {
			return fail("number outside bounds")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fail("expected boolean")
		}
	}
	return nil
}
func Decode(raw []byte, s *Schema) (map[string]any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if err := s.Validate(v, "arguments"); err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}
