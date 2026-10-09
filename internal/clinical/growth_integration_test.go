package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

// Opt-in real PostgreSQL + HTTP test. It accepts only a local disposable database
// named doctorchart_growth_test and creates/drops its own unique schema.
func TestGrowthHTTPPostgres(t *testing.T) {
	dsn := os.Getenv("DOCTORCHART_GROWTH_TEST_DSN")
	if dsn == "" {
		t.Skip("set DOCTORCHART_GROWTH_TEST_DSN to an isolated local doctorchart_growth_test database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "doctorchart_growth_test" || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" && config.ConnConfig.Host != "::1") {
		t.Fatal("growth integration tests require a local disposable doctorchart_growth_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, config.ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	schema := pgx.Identifier{fmt.Sprintf("growth_test_%d", time.Now().UnixNano())}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	// Use the repository's exact PostgreSQL column types and CHECK constraints.
	ddl, err := os.ReadFile("../../migration/schema-reference.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"patients", "clinical_encounters", "specialties", "doctor_workspaces"} {
		statement := regexp.MustCompile(`(?s)CREATE TABLE public\.` + name + ` \(\n.*?\n\);`).FindString(strings.ReplaceAll(string(ddl), "\r\n", "\n"))
		if statement == "" {
			t.Fatal("missing reference schema", name)
		}
		if _, err := admin.Exec(ctx, strings.Replace(statement, "public."+name, schema+"."+name, 1)); err != nil {
			t.Fatal(err)
		}
	}
	config.ConnConfig.RuntimeParams["search_path"] = strings.Trim(schema, `"`)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	seed := []string{
		`INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(1,true,'PEDIATRICS',now(),'Pediatría','[]','[]','["GROWTH"]',now(),1)`,
		`INSERT INTO doctor_workspaces(id,created_at,current_step_key,definition,onboarding_completed,updated_at,user_id,version,specialty_id) VALUES(1,now(),'COMPLETE','{"modulesConfig":{"GROWTH_CHARTS":true}}',true,now(),'owner',1,1),(2,now(),'COMPLETE','{"modulesConfig":{"GROWTH_CHARTS":false}}',true,now(),'disabled',1,1)`,
		`INSERT INTO patients(id,version,full_name,birth_date,gender,form_values,formula_results,form_definition_snapshot,formula_context,workspace_version,user_id,updated_at,personal_history) VALUES
		(1,0,'Fixture male','2024-05-10','MALE','{"sys_sex":"MALE"}','{}','{}','{}',1,'owner',now(),'{"birthMeasurements":{"weightKg":3.4,"heightCm":50,"headCircumferenceCm":35}}'),
		(2,0,'Fixture female','2024-05-10','FEMALE','{"sys_sex":"FEMALE"}','{}','{}','{}',1,'owner',now(),null),
		(3,0,'Legacy sex','2024-05-10','MALE','{}','{}','{}','{}',1,'owner',now(),null),
		(4,0,'Other owner','2024-05-10','MALE','{"sys_sex":"MALE"}','{}','{}','{}',1,'other',now(),null),
		(5,0,'Invalid date',current_date+1,'MALE','{"sys_sex":"MALE"}','{}','{}','{}',1,'owner',now(),null),
		(6,0,'Adolescent','2011-05-10','FEMALE','{"sys_sex":"FEMALE"}','{}','{}','{}',1,'owner',now(),null),
		(7,0,'Disabled module','2024-05-10','MALE','{"sys_sex":"MALE"}','{}','{}','{}',1,'disabled',now(),null)`,
		`INSERT INTO clinical_encounters(id,patient_id,specialty_id,version,workspace_version,consultation_type,encounter_date,updated_at,weight_kg,height_cm,head_circumference_cm,form_values,formula_results,form_definition_snapshot,formula_context) VALUES
		(10,1,1,0,1,'ROUTINE_CHECKUP','2024-05-10 01:00Z',now(),3.3,49,34,'{}','{}','{}','{}'),
		(11,1,1,0,1,'ROUTINE_CHECKUP','2024-05-10 04:00Z',now(),3.5,50,35,'{}','{}','{}','{}'),
		(12,1,1,0,1,'ROUTINE_CHECKUP','2025-05-10 02:00Z',now(),null,75,null,'{"heightMeasurementMode":"RECUMBENT"}','{}','{}','{}'),
		(13,1,1,0,1,'ROUTINE_CHECKUP','2026-05-11 04:00Z',now(),12,89,48,'{"heightMeasurementMode":"STANDING"}','{}','{}','{}'),
		(14,1,1,0,1,'ROUTINE_CHECKUP',now()+interval '1 day',now(),13,90,49,'{}','{}','{}','{}'),
		(15,4,1,0,1,'ROUTINE_CHECKUP','2026-05-11 04:00Z',now(),20,100,50,'{}','{}','{}','{}')`,
	}
	for _, statement := range seed {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() string {
		t.Helper()
		var value string
		err := pool.QueryRow(ctx, `SELECT md5((SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM patients p)||(SELECT jsonb_agg(to_jsonb(e) ORDER BY id)::text FROM clinical_encounters e))`).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	service, err := New(pool, workspace.New(pool, nil), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service.GrowthRoutes(mux)
	const secret = "isolated-growth-integration-secret"
	handler := api.Gateway(secret, mux)
	request := func(path, user string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if user != "" {
			r.Header.Set("X-Gateway-Secret", secret)
			r.Header.Set("X-User-Id", user)
			r.Header.Set("X-User-Role", "DOCTOR")
			r.Header.Set("X-User-Plan", "PRO")
			r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).Format(time.RFC3339Nano))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: HTTP %d expected %d: %s", path, w.Code, want, w.Body)
		}
		return w
	}
	decode := func(path string) patientGrowth {
		t.Helper()
		var value patientGrowth
		if err := json.Unmarshal(request(path, "owner", 200).Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	base := "/api/doctorchart/patients/"
	request(base+"1/growth", "", 401)
	request(base+"4/growth", "owner", 403)
	request(base+"4/measurements", "owner", 403)
	request(base+"1/growth", "other", 403)
	request(base+"999/growth", "owner", 404)
	request(base+"7/growth", "disabled", 403)
	request(base+"1/growth?referenceSex=UNKNOWN", "owner", 400)
	male := decode(base + "1/growth")
	if male.Status != "READY" || male.ReferenceSex != "MALE" || male.SexSource != "SYSTEM_SEX" || len(male.Charts) != 10 || len(male.Observations) != 4 {
		t.Fatalf("unexpected patient contract: %+v", male)
	}
	if male.Observations[0].AgeFormatted != "Recién nacido" || male.Observations[2].Date != "2025-05-09" || male.Observations[2].AgeDays != 364 || male.Observations[3].AgeDays != 731 {
		t.Fatal("birth/Argentine midnight/historical age incorrect", male.Observations)
	}
	for _, chart := range male.Charts {
		if chart.Definition.Sex != "MALE" {
			t.Fatal("mixed sex references")
		}
		if chart.Definition.Indicator == "BMI" && chart.Definition.Reference == "WHO 2006" {
			if len(chart.Points) != 1 || *chart.Points[0].EncounterID != 13 || chart.Points[0].Evaluation == nil {
				t.Fatal("BMI fabricated or mixed between encounters", chart.Points)
			}
		}
	}
	confirmed := decode(base + "1/growth?measurementModes=%7B%22BIRTH%22:%22RECUMBENT%22%7D")
	if confirmed.Observations[0].Mode != "RECUMBENT" || confirmed.Observations[1].Mode != "UNKNOWN" {
		t.Fatal("confirmation escaped its observation")
	}
	if decode(base+"3/growth").Status != "REFERENCE_SEX_REQUIRED" || decode(base+"3/growth?referenceSex=FEMALE").SexSource != "VIEW_CONFIRMATION" {
		t.Fatal("legacy gender guessed")
	}
	if decode(base+"5/growth").Status != "INVALID_BIRTH_DATE" {
		t.Fatal("future birth accepted")
	}
	for _, chart := range decode(base + "6/growth").Charts {
		if chart.Definition.Sex != "FEMALE" || (chart.Applicable && chart.Definition.Indicator != "HEIGHT" && chart.Definition.Indicator != "BMI") {
			t.Fatal("adolescent received invalid indicator")
		}
	}
	var legacy []growthMeasurement
	if err := json.Unmarshal(request(base+"1/measurements", "owner", 200).Body.Bytes(), &legacy); err != nil || len(legacy) != 4 || legacy[2].Date != "2025-05-09" {
		t.Fatal("legacy measurement contract", err)
	}
	if snapshot() != before {
		t.Fatal("read/confirmation endpoint modified the clinical history")
	}
}
