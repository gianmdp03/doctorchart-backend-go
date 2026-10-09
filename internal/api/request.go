package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

func Only(request map[string]any, fields ...string) error {
	if request == nil {
		return Invalid("La solicitud contiene propiedades desconocidas.")
	}
	for key := range request {
		found := false
		for _, field := range fields {
			if key == field {
				found = true
				break
			}
		}
		if !found {
			return Invalid("La solicitud contiene propiedades desconocidas.")
		}
	}
	return nil
}
func Text(request map[string]any, key string, max int) (string, error) {
	text, ok := request[key].(string)
	if !ok || JavaBlank(text) || JavaLength(text) > max {
		return "", Invalid(fmt.Sprintf("El campo %s es obligatorio y admite hasta %d caracteres.", key, max))
	}
	return text, nil
}
func JavaLength(text string) int { return len(utf16.Encode([]rune(text))) }
func JavaTrim(text string) string {
	return strings.TrimFunc(text, func(r rune) bool { return r <= 0x20 })
}
func JavaBlank(text string) bool {
	for _, r := range text {
		if r == 0x85 || r == 0xa0 || r == 0x2007 || r == 0x202f {
			return false
		}
		if !unicode.IsSpace(r) && (r < 0x1c || r > 0x1f) {
			return false
		}
	}
	return true
}
func OptionalText(request map[string]any, key string) (*string, error) {
	if request[key] == nil {
		return nil, nil
	}
	value, ok := request[key].(string)
	if !ok {
		return nil, Invalid("El campo " + key + " debe ser texto o null.")
	}
	return &value, nil
}
func Object(request map[string]any, key string) (map[string]any, error) {
	value, ok := request[key].(map[string]any)
	if !ok {
		return nil, Invalid("El campo " + key + " debe ser un objeto JSON.")
	}
	return value, nil
}
func Integer(value any, key string, min int64) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, Invalid("El campo " + key + " debe ser un entero válido.")
	}
	n, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil {
		return 0, Invalid("El campo " + key + " debe ser un entero válido.")
	}
	if n < min {
		return 0, Invalid(fmt.Sprintf("El campo %s debe ser mayor o igual a %d.", key, min))
	}
	return n, nil
}
func Version(request map[string]any, nullable bool) (*int64, error) {
	value, present := request["expectedVersion"]
	if !present || value == nil && !nullable {
		return nil, Invalid("expectedVersion es obligatorio.")
	}
	if value == nil {
		return nil, nil
	}
	version, err := Integer(value, "expectedVersion", 0)
	if err != nil {
		return nil, err
	}
	return &version, nil
}
