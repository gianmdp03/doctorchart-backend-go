package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
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
	directory, err := localConfigDirectory()
	if err != nil {
		return Config{}, err
	}
	fileValues, err := godotenv.Read(filepath.Join(directory, ".env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// Parser errors can contain the offending line, including credentials.
		return Config{}, errors.New("Configuración inválida: no se pudo leer .env (directorio actual o raíz del módulo); revisá sus permisos y sintaxis.")
	}
	get := func(key, alias, fallback string) string {
		names := []string{key, strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key)), strings.ToUpper(strings.NewReplacer(".", "_", "-", "").Replace(key)), alias}
		if key == "app.security.gateway-secret" {
			names = []string{"GATEWAY_SHARED_SECRET", "APP_SECURITY_GATEWAY_SECRET", "APP_SECURITY_GATEWAYSECRET", key}
		}
		if key == "services.billing.shared-secret" {
			names = []string{"SERVICES_BILLING_SHARED_SECRET", "SERVICES_POINTLY_SHARED_SECRET", key, "services.pointly.shared-secret", "SERVICES_BILLING_SHAREDSECRET"}
		}
		for _, name := range names {
			if value, ok := os.LookupEnv(name); ok {
				return value
			}
		}
		for _, name := range names {
			if value, ok := fileValues[name]; ok {
				return value
			}
		}
		return fallback
	}
	values := map[string]string{}
	for _, item := range [][3]string{
		{"server.port", "PORT", "8083"}, {"app.security.gateway-secret", "GATEWAY_SHARED_SECRET", ""}, {"services.billing.shared-secret", "SERVICES_BILLING_SHARED_SECRET", ""}, {"services.billing.url", "SERVICES_BILLING_URL", ""},
		{"spring.datasource.url", "SPRING_DATASOURCE_URL", ""}, {"spring.datasource.username", "DB_USERNAME", ""}, {"spring.datasource.password", "DB_PASSWORD", ""},
		{"app.storage.type", "STORAGE_TYPE", "local"}, {"app.storage.studies-dir", "STUDIES_STORAGE_DIR", ""}, {"app.storage.s3.endpoint", "S3_ENDPOINT", ""}, {"app.storage.s3.bucket", "S3_BUCKET", ""}, {"app.storage.s3.access-key", "S3_ACCESS_KEY", ""}, {"app.storage.s3.secret-key", "S3_SECRET_KEY", ""}, {"app.storage.s3.region", "S3_REGION", "auto"},
		{"services.image-converter.url", "IMAGE_CONVERTER_URL", ""}, {"services.image-converter.api-key", "IMAGE_CONVERTER_API_KEY", ""}, {"app.storage.allow-study-image-transformation", "STUDIES_ALLOW_IMAGE_TRANSFORM", "false"},
	} {
		values[item[0]] = get(item[0], item[1], item[2])
	}
	c := Config{Port: values["server.port"], GatewaySecret: values["app.security.gateway-secret"], BillingSecret: values["services.billing.shared-secret"], BillingURL: values["services.billing.url"]}
	c.Storage = storage.Config{Type: values["app.storage.type"], Directory: values["app.storage.studies-dir"], Endpoint: values["app.storage.s3.endpoint"], Bucket: values["app.storage.s3.bucket"], AccessKey: values["app.storage.s3.access-key"], SecretKey: values["app.storage.s3.secret-key"], Region: values["app.storage.s3.region"]}
	transformValue := values["app.storage.allow-study-image-transformation"]
	if transformValue != "true" && transformValue != "false" {
		return c, errors.New("Configuración inválida: STUDIES_ALLOW_IMAGE_TRANSFORM debe ser true o false.")
	}
	c.Storage.Converter = storage.ConverterConfig{URL: values["services.image-converter.url"], APIKey: values["services.image-converter.api-key"], AllowStudyTransformation: transformValue == "true"}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("Configuración inválida: PORT.")
	}
	for _, item := range []struct{ name, value string }{{"GATEWAY_SHARED_SECRET", c.GatewaySecret}, {"SERVICES_BILLING_SHARED_SECRET", c.BillingSecret}} {
		if err := validateSharedSecret(item.name, item.value); err != nil {
			return c, err
		}
	}
	if c.GatewaySecret == c.BillingSecret {
		return c, fmt.Errorf("Configuración inválida: GATEWAY_SHARED_SECRET y SERVICES_BILLING_SHARED_SECRET deben ser distintos.")
	}
	if err := storage.ValidateConverterConfig(c.Storage.Converter); err != nil {
		return c, err
	}
	if c.Storage.Converter.APIKey != "" && (c.Storage.Converter.APIKey == c.GatewaySecret || c.Storage.Converter.APIKey == c.BillingSecret) {
		return c, errors.New("Configuración inválida: IMAGE_CONVERTER_API_KEY debe ser independiente de Gateway y Billing.")
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
