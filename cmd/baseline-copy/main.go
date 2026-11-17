package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
)

func main() {
	sourceVersion := 19
	if len(os.Args) == 3 {
		switch os.Args[2] {
		case "--source-baseline-16":
			sourceVersion = 16
		case "--source-baseline-17":
			sourceVersion = 17
		case "--source-baseline-18":
			sourceVersion = 18
		default:
			sourceVersion = 0
		}
	}
	if sourceVersion == 0 || len(os.Args) < 2 || len(os.Args) > 3 || os.Args[1] != "--confirm-empty-target" {
		fmt.Fprintln(os.Stderr, "Use baseline-copy --confirm-empty-target [--source-baseline-16|--source-baseline-17|--source-baseline-18] with DOCTORCHART_COPY_SOURCE_DSN and DOCTORCHART_COPY_TARGET_DSN")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, timeout := context.WithTimeout(ctx, 30*time.Minute)
	defer timeout()
	if err := run(ctx, sourceVersion); err != nil {
		fmt.Fprintln(os.Stderr, "Baseline copy failed.", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, sourceVersion int) error {
	source, err := open(ctx, "DOCTORCHART_COPY_SOURCE_DSN")
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := open(ctx, "DOCTORCHART_COPY_TARGET_DSN")
	if err != nil {
		return err
	}
	defer target.Close()
	copyBaseline := db.CopyBaseline19
	switch sourceVersion {
	case 16:
		copyBaseline = db.CopyBaseline16
	case 17:
		copyBaseline = db.CopyBaseline17
	case 18:
		copyBaseline = db.CopyBaseline18
	}
	result, err := copyBaseline(ctx, source, target)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			// PostgreSQL can include complete tuples or COPY values in errors.
			return fmt.Errorf("PostgreSQL rejected the copy (SQLSTATE %s)", pg.Code)
		}
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func open(ctx context.Context, env string) (*pgxpool.Pool, error) {
	dsn := os.Getenv(env)
	if dsn == "" {
		return nil, fmt.Errorf("%s is required", env)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid connection configuration in %s", env)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("could not connect using %s", env)
	}
	return pool, nil
}
