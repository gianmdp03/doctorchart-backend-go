package bootstrap

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
)

// EnsureSchema validates or creates the embedded baseline without catalog seeds.
// Normal startup uses Run so the schema and its catalogs share one transaction.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	return db.Ensure(ctx, pool, nil)
}
