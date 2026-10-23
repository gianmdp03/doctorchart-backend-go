package clinical

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
)

// This test exercises the production reader against the real embedded baseline.
// It can also run independently of unrelated clinical/storage integrations:
// go test internal/clinical/follow_up.go internal/clinical/follow_up_query_test.go
func TestDashboardFollowUpQuery(t *testing.T) {
	raw := os.Getenv("DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set DOCTORCHART_FOLLOWUP_TEST_DATABASE_URL to an owned disposable loopback doctorchart_followups_query_test database")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "doctorchart_followups_query_test" || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" && config.ConnConfig.Host != "::1") {
		t.Fatal("follow-up query tests require a loopback doctorchart_followups_query_test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Ensure(ctx, pool, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	var specialty int64
	if err := tx.QueryRow(ctx, `INSERT INTO specialties(active,code,display_name,created_at,updated_at,version,navigation,patient_record_actions,patient_record_tabs) VALUES(true,'followup-query-fixture','Prueba descartable',now(),now(),0,'[]','[]','[]') RETURNING id`).Scan(&specialty); err != nil {
		t.Fatal(err)
	}
	patient := func(owner, name string) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO patients(full_name,birth_date,user_id,updated_at,version,workspace_version,form_definition_snapshot,form_values,formula_context,formula_results) VALUES($1,'2000-01-01',$2,now(),0,0,'{}','{}','{}','{}') RETURNING id`, name, owner).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	encounter := func(patientID int64, date any) int64 {
		t.Helper()
		var id int64
		// Deliberate equal instants test the deterministic id tie-breaker.
		if err := tx.QueryRow(ctx, `INSERT INTO clinical_encounters(patient_id,specialty_id,encounter_date,consultation_type,follow_up_date,updated_at,version,workspace_version,form_definition_snapshot,form_values,formula_context,formula_results) VALUES($1,$2,'2026-12-01T12:00:00Z','FOLLOW_UP',$3::date,now(),0,0,'{}','{}','{}','{}') RETURNING id`, patientID, specialty, date).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for i := 0; i < 12; i++ {
		encounter(patient("owner-a", fmt.Sprintf("Paciente %02d Apellido", i)), "2027-01-01")
	}
	encounter(patient("owner-a", "Hoy Apellido"), "2026-12-31")
	updatedPatient := patient("owner-a", "Nombre Anterior")
	encounter(updatedPatient, "2027-01-01")
	latest := encounter(updatedPatient, "2027-01-02")
	for _, date := range []any{nil, "2026-12-30", "2027-01-03", "2027-01-09"} {
		id := patient("owner-a", fmt.Sprintf("No recordar %v", date))
		encounter(id, "2027-01-01")
		encounter(id, date)
	}
	encounter(patient("owner-b", "Paciente Privado"), "2027-01-01")
	if _, err := tx.Exec(ctx, "UPDATE patients SET full_name='Nombre Completo Actual' WHERE id=$1", updatedPatient); err != nil {
		t.Fatal(err)
	}
	check := func(want int) []upcomingFollowUp {
		t.Helper()
		items, err := dashboardFollowUps(ctx, tx, "owner-a", "2026-12-31", "2027-01-03")
		if err != nil || len(items) != want {
			t.Fatalf("wrong reminders: %d, expected %d: %v", len(items), want, err)
		}
		for i, item := range items {
			if item.Date < "2026-12-31" || item.Date > "2027-01-02" || item.PatientName == "Paciente Privado" {
				t.Fatal("out-of-window or private reminder", item)
			}
			if i > 0 && items[i-1].Date > item.Date {
				t.Fatal("unstable chronological order")
			}
			if item.PatientID == updatedPatient && (item.EncounterID != latest || item.PatientName != "Nombre Completo Actual") {
				t.Fatal("stale consultation or name", item)
			}
		}
		return items
	}
	for _, zone := range []string{"America/Argentina/Buenos_Aires", "Pacific/Kiritimati", "America/New_York"} {
		if _, err := tx.Exec(ctx, "SELECT set_config('TimeZone',$1,true)", zone); err != nil {
			t.Fatal(err)
		}
		items := check(14)
		if items[len(items)-1].Date != "2027-01-02" {
			t.Fatal("date-only follow-up shifted with the connection zone")
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE clinical_encounters SET follow_up_date=NULL WHERE id=$1", latest); err != nil {
		t.Fatal(err)
	}
	check(13)
	if _, err := tx.Exec(ctx, "UPDATE clinical_encounters SET follow_up_date='2027-01-01' WHERE id=$1", latest); err != nil {
		t.Fatal(err)
	}
	check(14)
	private, err := dashboardFollowUps(ctx, tx, "owner-b", "2026-12-31", "2027-01-03")
	if err != nil || len(private) != 1 || private[0].PatientName != "Paciente Privado" {
		t.Fatal("ownership isolation failed", private, err)
	}
	empty, err := dashboardFollowUps(ctx, tx, "empty-owner", "2026-12-31", "2027-01-03")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty response must be []", empty, err)
	}
}
