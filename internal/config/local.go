package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func localConfigDirectory() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("no se pudo localizar .env")
	}
	if _, err := os.Stat(filepath.Join(start, ".env")); err == nil {
		return start, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no se pudo leer .env; revisá sus permisos")
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no se pudo localizar la raíz del módulo")
		}
		if filepath.Dir(dir) == dir {
			return start, nil
		}
	}
}

func validateSharedSecret(name, value string) error {
	lower := strings.ToLower(value)
	placeholder := false
	for _, marker := range []string{"replace", "example", "changeme", "change-me", "your_", "your-", "<", ">", "shared-internal-secret"} {
		placeholder = placeholder || strings.Contains(lower, marker)
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 32 || strings.TrimSpace(value) != value || placeholder {
		return fmt.Errorf("Configuración inválida: %s debe contener al menos 32 caracteres, sin espacios al inicio/final ni valores de ejemplo; configurala en el entorno o en .env (directorio actual o raíz del módulo).", name)
	}
	return nil
}
