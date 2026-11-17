// Package testfixture contains helpers called only by disposable database tests.
package testfixture

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed synthetic-template-approvals.sql
var syntheticTemplates string

func ApprovePublishedTemplates(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, syntheticTemplates)
	return err
}

func ApproveDraft(ctx context.Context, pool *pgxpool.Pool, specialty int64) error {
	_, err := pool.Exec(ctx, `WITH draft AS (
		SELECT w.specialty_id,w.version,public.template_review_content(w.draft_definition,
			coalesce(w.draft_experience,public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))) content
		FROM wizard_defaults w JOIN specialties s ON s.id=w.specialty_id WHERE s.id=$1
	) INSERT INTO wizard_approvals(specialty_id,revision,content,fingerprint,actor,kind,reference)
	SELECT specialty_id,version,content,encode(sha256(convert_to(content::text,'UTF8')),'hex'),
		'fixture-reviewer','SYNTHETIC','Disposable Go fixture' FROM draft`, specialty)
	return err
}
