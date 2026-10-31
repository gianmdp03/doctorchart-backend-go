package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"encoding/base64"
	"github.com/joho/godotenv"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

type Config struct {
	Notifications         notifications.Config
	Port                  string
	DatabaseURL           string
	GatewaySecret         string
	BillingSecret         string
	BillingURL            string
	SchedulesEnabled      bool
	BookingsEnabled       bool
	StaffEnabled          bool
	StaffOrigin           string
	PublicBookingsEnabled bool
	Storage               storage.Config
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
		{"DATABASE_URL", "", ""},
		{"app.storage.type", "STORAGE_TYPE", "local"}, {"app.storage.studies-dir", "STUDIES_STORAGE_DIR", ""}, {"app.storage.s3.endpoint", "S3_ENDPOINT", ""}, {"app.storage.s3.bucket", "S3_BUCKET", ""}, {"app.storage.s3.access-key", "S3_ACCESS_KEY", ""}, {"app.storage.s3.secret-key", "S3_SECRET_KEY", ""}, {"app.storage.s3.region", "S3_REGION", "auto"},
		{"services.image-converter.url", "IMAGE_CONVERTER_URL", ""}, {"services.image-converter.api-key", "IMAGE_CONVERTER_API_KEY", ""}, {"app.storage.allow-study-image-transformation", "STUDIES_ALLOW_IMAGE_TRANSFORM", "false"},
	} {
		values[item[0]] = get(item[0], item[1], item[2])
	}
	c := Config{Port: values["server.port"], GatewaySecret: values["app.security.gateway-secret"], BillingSecret: values["services.billing.shared-secret"], BillingURL: values["services.billing.url"]}
	scheduleFlag := get("features.schedules.enabled", "SCHEDULES_ENABLED", "false")
	if scheduleFlag != "true" && scheduleFlag != "false" {
		return c, errors.New("Configuración inválida: SCHEDULES_ENABLED debe ser true o false.")
	}
	c.SchedulesEnabled = scheduleFlag == "true"
	bookingFlag := get("features.bookings.enabled", "BOOKINGS_ENABLED", "false")
	if bookingFlag != "true" && bookingFlag != "false" {
		return c, errors.New("Configuración inválida: BOOKINGS_ENABLED debe ser true o false.")
	}
	c.BookingsEnabled = bookingFlag == "true"
	if c.BookingsEnabled && !c.SchedulesEnabled {
		return c, errors.New("BOOKINGS_ENABLED requiere SCHEDULES_ENABLED.")
	}
	publicFlag := get("features.public-bookings.enabled", "PUBLIC_BOOKINGS_ENABLED", "false")
	if publicFlag != "true" && publicFlag != "false" {
		return c, errors.New("PUBLIC_BOOKINGS_ENABLED debe ser true o false")
	}
	c.PublicBookingsEnabled = publicFlag == "true"
	if c.PublicBookingsEnabled && !c.BookingsEnabled {
		return c, errors.New("PUBLIC_BOOKINGS_ENABLED requiere BOOKINGS_ENABLED")
	}
	staffFlag := get("features.staff.enabled", "STAFF_ENABLED", "false")
	if staffFlag != "true" && staffFlag != "false" {
		return c, errors.New("STAFF_ENABLED debe ser true o false")
	}
	c.StaffEnabled = staffFlag == "true"
	c.StaffOrigin = get("features.staff.origin", "STAFF_ORIGIN", "")
	if c.StaffEnabled {
		origin, e := url.Parse(c.StaffOrigin)
		if !c.BookingsEnabled || e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			return c, errors.New("STAFF_ENABLED requiere BOOKINGS_ENABLED y STAFF_ORIGIN HTTPS exacto sin path")
		}
	}
	notifyFlag := get("features.notifications.enabled", "NOTIFICATIONS_ENABLED", "false")
	if notifyFlag != "true" && notifyFlag != "false" {
		return c, errors.New("NOTIFICATIONS_ENABLED debe ser true o false")
	}
	if notifyFlag == "true" {
		port, e := strconv.Atoi(get("notifications.smtp.port", "NOTIFICATIONS_SMTP_PORT", "587"))
		if e != nil {
			return c, errors.New("Puerto SMTP inválido")
		}
		lead, e := strconv.Atoi(get("notifications.reminder-minutes", "NOTIFICATIONS_REMINDER_MINUTES", "1440"))
		if e != nil || lead < 1 || lead > 10080 {
			return c, errors.New("Recordatorio inválido")
		}
		c.Notifications = notifications.Config{PublicOrigin: get("notifications.public-origin", "NOTIFICATIONS_PUBLIC_ORIGIN", ""), Enabled: true, Key: get("notifications.key", "NOTIFICATIONS_KEY", ""), Host: get("notifications.smtp.host", "NOTIFICATIONS_SMTP_HOST", ""), Port: port, From: get("notifications.smtp.from", "NOTIFICATIONS_SMTP_FROM", ""), Username: get("notifications.smtp.username", "NOTIFICATIONS_SMTP_USERNAME", ""), Password: get("notifications.smtp.password", "NOTIFICATIONS_SMTP_PASSWORD", ""), TLSMode: get("notifications.smtp.tls", "NOTIFICATIONS_SMTP_TLS", "starttls"), ReminderMinutes: lead}
		raw, e := base64.StdEncoding.DecodeString(c.Notifications.Key)
		if !c.BookingsEnabled || e != nil || len(raw) != 32 {
			return c, errors.New("NOTIFICATIONS_ENABLED requiere BOOKINGS_ENABLED y NOTIFICATIONS_KEY de32bytes base64")
		}
		origin, e := url.Parse(c.Notifications.PublicOrigin)
		if e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
			return c, errors.New("NOTIFICATIONS_PUBLIC_ORIGIN debe ser un origen HTTPS exacto")
		}
		if _, e = notifications.NewSMTP(c.Notifications); e != nil {
			return c, e
		}
	}
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
	for _, key := range []string{"DATABASE_URL", "services.billing.url"} {
		if api.JavaBlank(values[key]) {
			return c, fmt.Errorf("Configuración inválida: %s es obligatorio.", key)
		}
	}
	raw := values["DATABASE_URL"]
	db, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(raw) != raw ||
		(db.Scheme != "postgres" && db.Scheme != "postgresql") ||
		db.Hostname() == "" || strings.TrimSpace(strings.TrimPrefix(db.Path, "/")) == "" ||
		db.User == nil || strings.TrimSpace(db.User.Username()) == "" || db.Fragment != "" || db.Opaque != "" {
		return c, errors.New("Configuración inválida: DATABASE_URL debe ser una URL PostgreSQL con host, base y credenciales.")
	}
	password, present := db.User.Password()
	if !present || strings.TrimSpace(password) == "" {
		return c, errors.New("Configuración inválida: DATABASE_URL requiere una contraseña.")
	}
	if db.Port() != "" {
		port, err := strconv.Atoi(db.Port())
		if err != nil || port < 1 || port > 65535 {
			return c, errors.New("Configuración inválida: DATABASE_URL contiene un puerto inválido.")
		}
	}
	c.DatabaseURL = db.String()
	billingURL, err := url.Parse(c.BillingURL)
	if err != nil || billingURL.Hostname() == "" || billingURL.Scheme != "http" && billingURL.Scheme != "https" || billingURL.User != nil || billingURL.ForceQuery || strings.Contains(c.BillingURL, "#") || billingURL.RawQuery != "" || billingURL.Fragment != "" {
		return c, fmt.Errorf("Configuración inválida: services.billing.url debe ser una URL HTTP(S) sin credenciales, parámetros ni fragmentos.")
	}
	return c, nil
}
