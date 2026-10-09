package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalPropertiesAndEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	secret := strings.Repeat("a", 32)
	content := "spring.datasource.url=jdbc:postgresql://127.0.0.1:5432/fixture\nspring.datasource.username=fileuser\nspring.datasource.password=filepass\napp.security.gateway-secret=" + secret + "\nservices.billing.shared-secret=" + secret + "\nservices.billing.url=http://localhost:18999\nserver.port=${PORT:18084}\napp.storage.studies-dir=C\\:\\\\fixture\\\\studies\n"
	if err := os.WriteFile("secrets.properties", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_USERNAME", "aliasuser")
	t.Setenv("SPRING_DATASOURCE_USERNAME", "directuser")
	t.Setenv("PORT", "18085")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "18085" || !strings.Contains(c.DatabaseURL, "directuser:filepass@") || c.Storage.Directory != `C:\fixture\studies` {
		t.Fatalf("configuration precedence or escaping differs")
	}
	t.Setenv("APP_SECURITY_GATEWAY_SECRET", strings.Repeat("😀", 16))
	if _, err = Load(); err != nil {
		t.Fatalf("Java UTF-16 secret length: %v", err)
	}
	t.Setenv("APP_SECURITY_GATEWAY_SECRET", strings.Repeat("😀", 15))
	if _, err = Load(); err == nil {
		t.Fatal("accepted fewer than 32 UTF-16 units")
	}
}
func TestPropertyEscapesAndContinuation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "properties")
	if err := os.WriteFile(path, []byte("! comment\na\\ b : first\\\n  second\nunicode=\\u00e1\\uD83D\\uDE00\ncolon\\:key=value\\nnext\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := readProperties(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["a b"] != "firstsecond" || values["unicode"] != "á😀" || values["colon:key"] != "value\nnext" {
		t.Fatalf("unexpected decoding: %#v", values)
	}
}
