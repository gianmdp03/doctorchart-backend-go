package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	AppRole   = "doctorchart_app"
	MediaRole = "doctorchart_media"
)

// CheckRuntime validates the initialized baseline and the effective login.
// It uses the same advisory lock as bootstrap and never changes schema or seeds.
func CheckRuntime(ctx context.Context, pool *pgxpool.Pool, role string) (time.Time, error) {
	var initialized time.Time
	if role != AppRole && role != MediaRole {
		return initialized, fmt.Errorf("invalid DoctorChart runtime role")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return initialized, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL search_path = public, pg_catalog; SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '20s'"); err != nil {
		return initialized, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared($1,$2)", LockNamespace, LockKey); err != nil {
		return initialized, err
	}

	var allowed bool
	err = tx.QueryRow(ctx, `SELECT current_user = $1 AND session_user = $1
		AND rolcanlogin AND NOT (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication)
		AND current_setting('row_security') = 'on'
		AND NOT EXISTS (SELECT 1 FROM pg_auth_members WHERE member=r.oid)
		AND NOT has_schema_privilege('public','CREATE')
		AND NOT has_database_privilege(current_database(),'CREATE')
		AND NOT EXISTS (
			SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relkind IN ('r','p')
			AND (c.relowner=r.oid OR has_table_privilege(c.oid,'TRUNCATE,TRIGGER,MAINTAIN,REFERENCES')
			OR (CASE WHEN $1='doctorchart_media'
				THEN c.relname NOT IN ('app_schema_state','media_assets','media_events')
				ELSE c.relname IN ('media_assets','media_events') END
				AND has_table_privilege(c.oid,'SELECT,INSERT,UPDATE,DELETE,REFERENCES')))
		)
		AND NOT EXISTS (
			SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
			JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relkind IN ('r','p') AND a.attnum>0 AND NOT a.attisdropped
			AND (CASE WHEN $1='doctorchart_media'
				THEN c.relname NOT IN ('app_schema_state','media_assets','media_events')
					AND NOT (c.relname='patient_medical_studies' AND a.attname='media_asset_id')
				ELSE c.relname IN ('media_assets','media_events') END)
			AND has_column_privilege(c.oid,a.attnum,'SELECT,INSERT,UPDATE,REFERENCES')
		)
		AND NOT ($1='doctorchart_media' AND has_column_privilege(
			'public.patient_medical_studies','media_asset_id','INSERT,UPDATE,REFERENCES'))
		AND NOT EXISTS (
			SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
			JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relkind IN ('r','p') AND a.attnum>0 AND NOT a.attisdropped
			AND has_column_privilege(c.oid,a.attnum,'REFERENCES')
		)
		AND NOT EXISTS (
			SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
			WHERE n.nspname='public' AND p.prosecdef AND (
				p.proowner=r.oid OR EXISTS (
					SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl
					WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
				) OR (
					($1='doctorchart_media' OR p.proname IN ('guard_context_binding','validate_booking_owner_transition'))
					AND has_function_privilege(p.oid,'EXECUTE')
				)
			)
		)
		AND NOT has_table_privilege('public.app_schema_state','INSERT,UPDATE,DELETE')
		AND NOT has_table_privilege('public.clinical_access_events','INSERT,UPDATE,DELETE,TRUNCATE')
		AND NOT has_any_column_privilege('public.clinical_access_events','UPDATE')
		AND NOT has_table_privilege('public.clinical_document_issues','UPDATE,DELETE,TRUNCATE')
		AND NOT has_any_column_privilege('public.clinical_document_issues','UPDATE')
		AND NOT has_table_privilege('public.clinical_document_annulments','UPDATE,DELETE,TRUNCATE')
		AND NOT has_any_column_privilege('public.clinical_document_annulments','UPDATE')
		AND NOT has_table_privilege('public.clinical_export_evidence','UPDATE,DELETE,TRUNCATE')
		AND NOT has_any_column_privilege('public.clinical_export_evidence','UPDATE')
		FROM pg_roles r WHERE r.rolname=current_user`, role).Scan(&allowed)
	if err != nil || !allowed {
		return initialized, fmt.Errorf("PostgreSQL runtime requires restricted login %s and an initialized baseline", role)
	}
	var major int
	if err = tx.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer / 10000").Scan(&major); err != nil {
		return initialized, err
	}
	expected, err := catalogForMajor(major)
	if err != nil {
		return initialized, err
	}
	if err = validateMarker(ctx, tx, digest(Schema), expected); err != nil {
		return initialized, err
	}
	if err = validateCatalog(ctx, tx, expected); err != nil {
		return initialized, err
	}
	if err = tx.QueryRow(ctx, "SELECT initialized_at FROM public.app_schema_state WHERE id=1").Scan(&initialized); err != nil {
		return initialized, err
	}
	return initialized, tx.Commit(ctx)
}
