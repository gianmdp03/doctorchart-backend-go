package config

import (
	"os"
	"strings"
	"testing"
)

func TestConverterConfigurationAndPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	base := "GATEWAY_SHARED_SECRET=" + strings.Repeat("g", 64) + "\nSERVICES_BILLING_SHARED_SECRET=" + strings.Repeat("b", 64) + "\nDATABASE_URL=postgresql://fixture:fixture@127.0.0.1:5432/fixture\nSERVICES_BILLING_URL=http://127.0.0.1:8081\nIMAGE_CONVERTER_URL=http://127.0.0.1:8080\nIMAGE_CONVERTER_API_KEY=" + strings.Repeat("c", 64) + "\nSTUDIES_ALLOW_IMAGE_TRANSFORM=true\n"
	if err := os.WriteFile(".env", []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Storage.Converter.URL != "http://127.0.0.1:8080" || !c.Storage.Converter.AllowStudyTransformation {
		t.Fatal("converter configuration not loaded")
	}
	t.Setenv("IMAGE_CONVERTER_URL", "http://127.0.0.1:18080")
	t.Setenv("STUDIES_ALLOW_IMAGE_TRANSFORM", "false")
	c, err = Load()
	if err != nil || c.Storage.Converter.URL != "http://127.0.0.1:18080" || c.Storage.Converter.AllowStudyTransformation {
		t.Fatal("process did not override dotenv")
	}
	for _, test := range []struct{ key, value string }{{"IMAGE_CONVERTER_URL", "https://key@host"}, {"IMAGE_CONVERTER_URL", "http://host?query=1"}, {"IMAGE_CONVERTER_API_KEY", ""}, {"IMAGE_CONVERTER_API_KEY", strings.Repeat("g", 64)}, {"IMAGE_CONVERTER_API_KEY", strings.Repeat("b", 64)}, {"STUDIES_ALLOW_IMAGE_TRANSFORM", "yes"}} {
		t.Run(test.key+test.value, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			if _, err := Load(); err == nil || strings.Contains(err.Error(), strings.Repeat("c", 64)) {
				t.Fatal("invalid deployment configuration accepted or secret exposed")
			}
		})
	}
}
