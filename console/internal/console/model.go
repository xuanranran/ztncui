package console

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Object map[string]any

func objectOf(value any) Object {
	switch value := value.(type) {
	case Object:
		return value
	case map[string]any:
		return Object(value)
	}
	return Object{}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func flag(v any) bool { b, _ := v.(bool); return b }
func objects(v any) []Object {
	result := []Object{}
	switch a := v.(type) {
	case []Object:
		return a
	case []any:
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				result = append(result, Object(m))
			} else if m, ok := x.(Object); ok {
				result = append(result, m)
			}
		}
	}
	return result
}
func stringsOf(v any) []string {
	result := []string{}
	switch a := v.(type) {
	case []string:
		return a
	case []any:
		for _, s := range a {
			if v, ok := s.(string); ok {
				result = append(result, v)
			}
		}
	}
	return result
}
func clone(v Object) Object {
	data, _ := json.Marshal(v)
	var result Object
	_ = json.Unmarshal(data, &result)
	return result
}
func validID(id string, length int) bool {
	if len(id) != length {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func fail(message string) error { return fmt.Errorf("%s", message) }
func hasAny(m Object, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}
func trim(s string) string { return strings.TrimSpace(s) }
