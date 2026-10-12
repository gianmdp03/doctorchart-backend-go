package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/clinical"
	"github.com/syntah/doctorchart-backend-go/internal/config"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	files, err := storage.New(c.Storage)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	poolConfig, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid PostgreSQL configuration")
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 10 * time.Minute
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	poolConfig.ConnConfig.RuntimeParams["search_path"] = "public"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("create PostgreSQL pool failed")
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		return errors.New("PostgreSQL is unavailable")
	}
	if err := bootstrap.EnsureSchema(startup, pool); err != nil {
		return err
	}
	rules := billing.New(c.BillingURL)
	rules.Load(startup)
	mux := http.NewServeMux()
	api.BaseRoutes(mux, rules, c.BillingSecret)
	validator, err := wizard.NewValidator()
	if err != nil {
		return err
	}
	if err := bootstrap.Run(startup, pool, validator); err != nil {
		return err
	}
	workspaces := workspace.New(pool, validator)
	workspaces.Routes(mux)
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
	maintenanceDone := make(chan struct{})
	go func() { defer close(maintenanceDone); clinicalService.Maintain(ctx) }()
	defer func() { stop(); <-maintenanceDone }()
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
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}
