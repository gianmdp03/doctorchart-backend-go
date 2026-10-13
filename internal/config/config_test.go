package config

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestDotenvAndEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")
	if err := os.Unsetenv("DATABASE_URL"); err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("a", 32)
	fileURL := (&url.URL{Scheme: "postgresql", Host: "127.0.0.1:5432", Path: "/fixture", User: url.UserPassword("fileuser", "file$pass#á")}).String()
	content := "DATABASE_URL='" + fileURL + "'\nGATEWAY_SHARED_SECRET=" + secret + "\nSERVICES_BILLING_SHARED_SECRET=" + strings.Repeat("b", 32) + "\nSERVICES_BILLING_URL=http://localhost:18999\nPORT=18084\nSTUDIES_STORAGE_DIR='C:\\fixture\\studies'\n"
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
	processURL := (&url.URL{Scheme: "postgres", Host: "127.0.0.1:55439", Path: "/fixture", User: url.UserPassword("directuser", "process${literal}password")}).String()
	t.Setenv("DATABASE_URL", processURL)
	t.Setenv("PORT", "18085")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "18085" || c.DatabaseURL != processURL || c.Storage.Directory != `C:\fixture\studies` {
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
		"DATABASE_URL":                   "postgresql://processuser:processpass@localhost:5432/fixture",
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
	if err := os.WriteFile(".env", []byte("DATABASE_URL=\""+credential), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || strings.Contains(err.Error(), credential) {
		t.Fatal("malformed dotenv error must be redacted")
	}
}

func TestDatabaseURLValidationAndRedaction(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("g", 64))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 64))
	t.Setenv("SERVICES_BILLING_URL", "http://localhost:18999")
	const password = "DO_NOT_EXPOSE_DATABASE_PASSWORD"
	for _, tc := range []struct {
		name, address string
		valid         bool
	}{
		{"postgres", "postgres://fixture:" + password + "@localhost/fixture?sslmode=disable", true},
		{"postgresql IPv6", "postgresql://fixture:" + password + "@[::1]:55439/fixture", true},
		{"absent", "", false},
		{"JDBC rejected", "jdbc:postgresql://localhost:5432/fixture", false},
		{"HTTP rejected", "https://fixture:" + password + "@localhost/fixture", false},
		{"host absent", "postgres://fixture:" + password + "@/fixture", false},
		{"database absent", "postgres://fixture:" + password + "@localhost/", false},
		{"userinfo absent", "postgres://localhost/fixture", false},
		{"username absent", "postgres://:" + password + "@localhost/fixture", false},
		{"password absent", "postgres://fixture@localhost/fixture", false},
		{"password empty", "postgres://fixture:@localhost/fixture", false},
		{"invalid escape", "postgres://fixture:%not_hex@localhost/fixture", false},
		{"port zero", "postgres://fixture:" + password + "@localhost:0/fixture", false},
		{"port overflow", "postgres://fixture:" + password + "@localhost:65536/fixture", false},
		{"port text", "postgres://fixture:" + password + "@localhost:invalid/fixture", false},
		{"fragment rejected", "postgres://fixture:" + password + "@localhost/fixture#token", false},
		{"whitespace rejected", " postgres://fixture:" + password + "@localhost/fixture", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.address)
			c, err := Load()
			if tc.valid {
				if err != nil || c.DatabaseURL != tc.address {
					t.Fatal("valid PostgreSQL URL rejected or changed", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || strings.Contains(err.Error(), password) {
				t.Fatal("invalid PostgreSQL URL accepted or error was not redacted", err)
			}
		})
	}
}

func TestLegacyDatabaseSettingsDoNotConfigurePostgreSQL(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("g", 64))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 64))
	t.Setenv("SERVICES_BILLING_URL", "http://localhost:18999")
	t.Setenv("SPRING_DATASOURCE_URL", "jdbc:postgresql://localhost:5432/obsolete")
	t.Setenv("SPRING_DATASOURCE_USERNAME", "obsolete")
	t.Setenv("SPRING_DATASOURCE_PASSWORD", "obsolete")
	t.Setenv("DB_USERNAME", "obsolete")
	t.Setenv("DB_PASSWORD", "obsolete")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatal("legacy settings substituted for DATABASE_URL", err)
	}
	const current = "postgres://fixture:fixture@localhost:55439/current"
	t.Setenv("DATABASE_URL", current)
	c, err := Load()
	if err != nil || c.DatabaseURL != current {
		t.Fatal("legacy settings changed explicit DATABASE_URL", err)
	}
}
