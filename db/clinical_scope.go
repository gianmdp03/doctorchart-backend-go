package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// BindClinicalScope binds trusted service identity to this transaction only.
func BindClinicalScope(ctx context.Context, tx pgx.Tx, owner string, contextID int64) error {
	_, err := tx.Exec(ctx, `SELECT set_config('doctorchart.owner', $1, true),
		set_config('doctorchart.context', CASE WHEN $2::bigint > 0 THEN $2::bigint::text ELSE '' END, true)`,
		owner, contextID)
	return err
}
