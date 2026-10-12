package config

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDotenvAndEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	secret := strings.Repeat("a", 32)
	content := "SPRING_DATASOURCE_URL=jdbc:postgresql://127.0.0.1:5432/fixture\nDB_USERNAME=fileuser\nDB_PASSWORD='file$pass#á'\nGATEWAY_SHARED_SECRET=" + secret + "\nSERVICES_BILLING_SHARED_SECRET=" + strings.Repeat("b", 32) + "\nSERVICES_BILLING_URL=http://localhost:18999\nPORT=18084\nSTUDIES_STORAGE_DIR='C:\\fixture\\studies'\n"
	if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	fileConfig, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := url.Parse(fileConfig.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := db.User.Password()
	if db.User.Username() != "fileuser" || password != "file$pass#á" || fileConfig.Port != "18084" {
		t.Fatal("dotenv quoted credentials changed")
	}
	t.Setenv("DB_USERNAME", "aliasuser")
	t.Setenv("DB_PASSWORD", "process${literal}password")
	t.Setenv("SPRING_DATASOURCE_USERNAME", "directuser")
	t.Setenv("PORT", "18085")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "18085" || !strings.Contains(c.DatabaseURL, "directuser:process$%7Bliteral%7Dpassword@") || c.Storage.Directory != `C:\fixture\studies` {
		t.Fatal("configuration precedence or escaping differs")
	}
	t.Setenv("APP_SECURITY_GATEWAY_SECRET", strings.Repeat("😀", 32))
	if _, err = Load(); err != nil {
		t.Fatalf("Unicode character secret length: %v", err)
	}
	t.Setenv("APP_SECURITY_GATEWAY_SECRET", strings.Repeat("😀", 31))
	if _, err = Load(); err == nil {
		t.Fatal("accepted fewer than 32 characters")
	}
}

func TestProcessEnvironmentWithoutDotenv(t *testing.T) {
	t.Chdir(t.TempDir())
	for name, value := range map[string]string{
		"SPRING_DATASOURCE_URL": "jdbc:postgresql://localhost:5432/fixture",
		"DB_USERNAME":           "processuser", "DB_PASSWORD": "processpass",
		"GATEWAY_SHARED_SECRET":          strings.Repeat("a", 32),
		"SERVICES_BILLING_SHARED_SECRET": strings.Repeat("b", 32),
		"SERVICES_BILLING_URL":           "http://localhost:18999",
	} {
		t.Setenv(name, value)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("secrets.properties", []byte("ignored obsolete format"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatal("obsolete configuration must not be loaded")
	}
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("explicitly empty process credentials must be rejected")
	}
}
func TestMalformedDotenvDoesNotExposeCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	const credential = "DO_NOT_EXPOSE_THIS_CREDENTIAL"
	if err := os.WriteFile(".env", []byte("DB_PASSWORD=\""+credential), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || strings.Contains(err.Error(), credential) {
		t.Fatal("malformed dotenv error must be redacted")
	}
}
