package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	common "github.com/bycode/bycode-image-converter-go/pkg/media"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/clinical"
	"github.com/syntah/doctorchart-backend-go/internal/clinics"
	"github.com/syntah/doctorchart-backend-go/internal/config"
	"github.com/syntah/doctorchart-backend-go/internal/dataexport"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	reviews, err := workspace.NewReviewAuthority(os.Getenv("DOCTORCHART_TEMPLATE_REVIEW_MANIFEST"), os.Getenv("DOCTORCHART_SYNTHETIC_TEMPLATE_REVIEWER"))
	if err != nil {
		return err
	}
	mediaConfig, err := common.Load("doctorchart")
	if err != nil {
		return err
	}
	c.Storage.Managed = &mediaConfig
	files, err := storage.New(c.Storage)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := openPool(ctx, c.DatabaseURL, 10)
	if err != nil {
		return err
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	initialized, err := db.CheckRuntime(startup, pool, db.AppRole)
	if err != nil {
		return err
	}
	if files.Managed() {
		if c.MediaDatabaseURL == "" {
			return errors.New("MEDIA_DATABASE_URL is required for managed storage")
		}
		mediaPool, err := openPool(ctx, c.MediaDatabaseURL, 4)
		if err != nil {
			return err
		}
		defer mediaPool.Close()
		mediaInitialized, err := db.CheckRuntime(startup, mediaPool, db.MediaRole)
		if err != nil {
			return err
		}
		if !mediaInitialized.Equal(initialized) {
			return errors.New("media catalog must use the initialized DoctorChart database")
		}
		mediaDB := stdlib.OpenDBFromPool(mediaPool)
		defer mediaDB.Close()
		files.SetCatalog(mediaDB)
	}
	rules := billing.New(c.BillingURL)
	entitlements := billing.NewEntitlements(pool, c.BillingURL, c.BillingSecret).WithPlanPolicy()
	rules.SetInvalidator(entitlements.Invalidate)
	rules.Load(startup)
	mux := http.NewServeMux()
	api.BaseRoutes(mux, rules, c.BillingSecret, func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return errors.New("PostgreSQL is unavailable")
		}
		if !rules.Available() {
			return errors.New("billing rules are unavailable")
		}
		return nil
	})
	validator, err := wizard.NewValidator()
	if err != nil {
		return err
	}
	notices, err := notifications.New(pool, c.Notifications)
	if err != nil {
		return err
	}
	if notices != nil {
		if err = notices.ValidateKey(startup); err != nil {
			return err
		}
	}
	workspaces := workspace.New(pool, validator).WithReviewAuthority(reviews).WithLogoConverter(files)
	workspaces.Routes(mux)
	clinics.NewWithBookings(pool, entitlements, c.SchedulesEnabled, c.BookingsEnabled).WithPublicBookings(c.PublicBookingsEnabled, c.GatewaySecret).WithStaff(c.StaffEnabled, c.StaffOrigin).WithNotifications(notices).Routes(mux)
	clinicalService, err := clinical.New(pool, workspaces, validator, rules, files)
	if err != nil {
		return err
	}
	clinicalService.Routes(mux)
	clinicalService.DirectoryRoutes(mux)
	clinicalService.EncounterRoutes(mux)
	clinicalService.PrescriptionRoutes(mux)
	clinicalService.GrowthRoutes(mux)
	clinicalService.VaccinationRoutes(mux)
	clinicalService.CalculatorRoutes(mux)
	clinicalService.StudyRoutes(mux)
	clinicalService.PrintRoutes(mux)
	exports, err := dataexport.New(ctx, pool, files, c.BillingURL, c.BillingSecret)
	if err != nil {
		return err
	}
	defer exports.Close()
	exports.Routes(mux)
	notificationsDone := make(chan struct{})
	go func() {
		defer close(notificationsDone)
		if notices != nil {
			notices.Run(ctx)
		}
	}()
	defer func() { stop(); <-notificationsDone }()
	maintenanceDone := make(chan struct{})
	go func() { defer close(maintenanceDone); clinicalService.Maintain(ctx) }()
	rulesDone := make(chan struct{})
	go func() { defer close(rulesDone); rules.RunRefresh(ctx) }()
	defer func() { stop(); <-maintenanceDone; <-rulesDone }()
	if err := clinicalService.CatalogRoutes(mux); err != nil {
		return err
	}
	server := &http.Server{Addr: ":" + c.Port, Handler: api.Gateway(c.GatewaySecret, api.Dispatch(mux)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	slog.Info("doctorchart listening", "port", c.Port)
	select {
	case err := <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
	return nil
}
func openPool(ctx context.Context, url string, maxConnections int32) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	config.MaxConns = maxConnections
	config.MinConns = 0
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 10 * time.Minute
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	config.ConnConfig.RuntimeParams["search_path"] = "public"
	config.ConnConfig.RuntimeParams["row_security"] = "on"
	config.ConnConfig.RuntimeParams["doctorchart.template_lab"] = "off"
	if config.ConnConfig.User == db.AppRole {
		starterTemplates, err := starterTemplateConfig(os.Getenv("DOCTORCHART_STARTER_TEMPLATES"))
		if err != nil {
			return nil, err
		}
		config.ConnConfig.RuntimeParams["doctorchart.starter_templates"] = starterTemplates
		if err := configureTemplateLab(config.ConnConfig); err != nil {
			return nil, err
		}
	}
	return pgxpool.NewWithConfig(ctx, config)
}

func starterTemplateConfig(raw string) (string, error) {
	if raw == "" {
		return "{}", nil
	}
	var entries map[string]string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil || entries == nil {
		return "", errors.New("invalid initial template configuration")
	}
	code := regexp.MustCompile("^[a-z][a-z0-9_]{0,63}$")
	fingerprint := regexp.MustCompile("^[a-f0-9]{64}$")
	for key, value := range entries {
		if !code.MatchString(key) || !fingerprint.MatchString(value) {
			return "", errors.New("invalid initial template code or fingerprint")
		}
	}
	normalized, err := json.Marshal(entries)
	return string(normalized), err
}

func initializeDatabase() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("init-db requires DATABASE_URL for the schema owner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := openPool(ctx, url, 1)
	if err != nil {
		return err
	}
	defer pool.Close()
	validator, err := wizard.NewValidator()
	if err != nil {
		return err
	}
	return bootstrap.Run(ctx, pool, validator)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	var err error
	switch {
	case len(os.Args) == 1:
		err = run()
	case len(os.Args) == 2 && os.Args[1] == "init-db":
		err = initializeDatabase()
	default:
		err = errors.New("supported command: init-db")
	}
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}
