package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

type Config struct {
	Port          string
	DatabaseURL   string
	GatewaySecret string
	BillingSecret string
	BillingURL    string
	Storage       storage.Config
}

func Load() (Config, error) {
	properties, err := readProperties("secrets.properties")
	if err != nil {
		return Config{}, fmt.Errorf("read external configuration: %w", err)
	}
	get := func(key, alias, fallback string) string {
		for _, name := range []string{key, strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key)), strings.ToUpper(strings.NewReplacer(".", "_", "-", "").Replace(key))} {
			if value, ok := os.LookupEnv(name); ok {
				return value
			}
		}
		if value, ok := properties[key]; ok {
			return value
		}
		if value, ok := os.LookupEnv(alias); ok {
			return value
		}
		return fallback
	}
	values := map[string]string{}
	for _, item := range [][3]string{
		{"server.port", "PORT", "8083"}, {"app.security.gateway-secret", "GATEWAY_SHARED_SECRET", ""}, {"services.billing.shared-secret", "SERVICES_BILLING_SHARED_SECRET", ""}, {"services.billing.url", "SERVICES_BILLING_URL", ""},
		{"spring.datasource.url", "SPRING_DATASOURCE_URL", ""}, {"spring.datasource.username", "DB_USERNAME", ""}, {"spring.datasource.password", "DB_PASSWORD", ""},
		{"app.storage.type", "STORAGE_TYPE", "local"}, {"app.storage.studies-dir", "STUDIES_STORAGE_DIR", ""}, {"app.storage.s3.endpoint", "S3_ENDPOINT", ""}, {"app.storage.s3.bucket", "S3_BUCKET", ""}, {"app.storage.s3.access-key", "S3_ACCESS_KEY", ""}, {"app.storage.s3.secret-key", "S3_SECRET_KEY", ""}, {"app.storage.s3.region", "S3_REGION", "auto"},
	} {
		value := get(item[0], item[1], item[2])
		for attempts := 0; strings.Contains(value, "${"); attempts++ {
			if attempts >= 32 {
				return Config{}, fmt.Errorf("Configuración inválida: %s contiene un placeholder sin resolver.", item[0])
			}
			start := strings.Index(value, "${")
			end := strings.Index(value[start+2:], "}")
			if end < 0 {
				return Config{}, fmt.Errorf("Configuración inválida: %s contiene un placeholder sin resolver.", item[0])
			}
			end += start + 2
			key, defaultValue, hasDefault := strings.Cut(value[start+2:end], ":")
			replacement, ok := os.LookupEnv(key)
			if !ok {
				replacement, ok = properties[key]
			}
			if !ok && !hasDefault {
				return Config{}, fmt.Errorf("Configuración inválida: %s contiene un placeholder sin resolver.", item[0])
			}
			if !ok {
				replacement = defaultValue
			}
			value = value[:start] + replacement + value[end+1:]
		}
		values[item[0]] = value
	}
	c := Config{Port: values["server.port"], GatewaySecret: values["app.security.gateway-secret"], BillingSecret: values["services.billing.shared-secret"], BillingURL: values["services.billing.url"]}
	c.Storage = storage.Config{Type: values["app.storage.type"], Directory: values["app.storage.studies-dir"], Endpoint: values["app.storage.s3.endpoint"], Bucket: values["app.storage.s3.bucket"], AccessKey: values["app.storage.s3.access-key"], SecretKey: values["app.storage.s3.secret-key"], Region: values["app.storage.s3.region"]}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("Configuración inválida: PORT.")
	}
	for key, value := range map[string]string{"app.security.gateway-secret": c.GatewaySecret, "services.billing.shared-secret": c.BillingSecret} {
		if api.JavaLength(value) < 32 || api.JavaTrim(value) != value || strings.Contains(strings.ToLower(value), "replace") {
			return c, fmt.Errorf("Configuración inválida: %s debe contener al menos 32 caracteres y no ser un ejemplo.", key)
		}
	}
	for _, key := range []string{"spring.datasource.username", "spring.datasource.password", "spring.datasource.url", "services.billing.url"} {
		if api.JavaBlank(values[key]) {
			return c, fmt.Errorf("Configuración inválida: %s es obligatorio.", key)
		}
	}
	raw := values["spring.datasource.url"]
	if !strings.HasPrefix(raw, "jdbc:postgresql://") {
		return c, fmt.Errorf("Configuración inválida: spring.datasource.url debe identificar una base PostgreSQL.")
	}
	db, err := url.Parse(strings.TrimPrefix(raw, "jdbc:"))
	if err != nil || db.Hostname() == "" {
		return c, fmt.Errorf("Configuración inválida: spring.datasource.url.")
	}
	db.User = url.UserPassword(values["spring.datasource.username"], values["spring.datasource.password"])
	c.DatabaseURL = db.String()
	billingURL, err := url.Parse(c.BillingURL)
	if err != nil || billingURL.Hostname() == "" || billingURL.Scheme != "http" && billingURL.Scheme != "https" || billingURL.User != nil || billingURL.ForceQuery || strings.Contains(c.BillingURL, "#") || billingURL.RawQuery != "" || billingURL.Fragment != "" {
		return c, fmt.Errorf("Configuración inválida: services.billing.url debe ser una URL HTTP(S) sin credenciales, parámetros ni fragmentos.")
	}
	return c, nil
}
