package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func readString(t *testing.T, pool *sql.DB, query string, args ...any) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func clinicalFixture(t *testing.T, pool *sql.DB) {
	t.Helper()
	// Synthetic data only, including two owners and a complete attachment chain.
	_, err := pool.Exec(`
INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version)
 VALUES(1,true,'M1_FIXTURE',now(),'Fixture','[]','[]','[]',now(),7);
INSERT INTO doctor_workspaces(id,created_at,updated_at,user_id,version)
 VALUES(1,now(),now(),'m1-owner',3),(2,now(),now(),'m1-other',2);
INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,definition,version,current_step_key,onboarding_completed,created_at,updated_at)
 VALUES(1,1,'{}',3,'COMPLETE',true,now(),now()),(2,1,'{}',2,'COMPLETE',true,now(),now());
INSERT INTO patients(id,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version,personal_history,workspace_id,specialty_context_id)
 VALUES(1,'1990-01-01','{}','{}','{}','{}','Preserve patient',now(),'m1-owner',4,3,'{"history":"preserve"}',1,1),
       (2,'2000-01-01','{}','{}','{}','{}','Other patient',now(),'m1-other',1,2,null,2,2);
INSERT INTO clinical_encounters(id,consultation_type,encounter_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version,patient_id,specialty_id)
 VALUES(1,'FIRST_VISIT',now(),'{}','{}','{}','{}',now(),2,3,1,1);
INSERT INTO patient_prescription_records(id,author_user_id,created_at,diagnosis_description,general_instructions,type,patient_id)
 VALUES(1,'m1-owner',now(),'Synthetic diagnosis','Preserve instructions','PRESCRIPTION',1);
INSERT INTO prescription_medications(record_id,administration_route,dosage,frequency,instructions,medication_name,item_position)
 VALUES(1,'oral','synthetic','daily','preserve','Fixture medicine',0);
INSERT INTO prescription_diagnostic_orders(record_id,study_description,item_position) VALUES(1,'Preserve diagnostic order',0);
INSERT INTO media_assets(asset_id,document) VALUES('00000000-0000-0000-0000-000000000001','{"asset_id":"00000000-0000-0000-0000-000000000001","version":"1","service":"doctorchart","storage_key":"synthetic-preserved-file"}');
INSERT INTO media_events(event_id,document) VALUES('00000000-0000-0000-0000-000000000002','{"event":"preserve"}');
INSERT INTO patient_medical_studies(id,study_name,media_asset_id,content_type,file_size,original_file_name,stored_file_name,uploaded_at,patient_id)
 VALUES(1,'Synthetic study','00000000-0000-0000-0000-000000000001','application/pdf',42,'original.pdf','00000000-0000-0000-0000-000000000003',now(),1);
INSERT INTO vaccination_calendars(id,definition) VALUES('fixture','{}');
INSERT INTO vaccination_doses(id,calendar_id) VALUES('fixture-dose','fixture');
INSERT INTO patient_vaccinations(id,applied_at,dose_id,patient_id) VALUES(1,'2020-01-01','fixture-dose',1);
INSERT INTO study_file_deletions(storage_key,requested_at) VALUES('00000000-0000-0000-0000-000000000004',now());
SELECT nextval('patients_id_seq');
`)
	if err != nil {
		t.Fatal(err)
	}
}

func initializedFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := fixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	return pool
}

func scheduleFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := initializedFixture(t)
	clinicalFixture(t, pool)
	if _, err := pool.Exec(`
INSERT INTO clinics(id,workspace_id,name,country,province,city,address,timezone,status)
 VALUES(1,1,'Centro','Argentina','Buenos Aires','City','Synthetic','UTC','ACTIVE'),
       (2,2,'Other','Argentina','Buenos Aires','City','Synthetic','UTC','ACTIVE');
INSERT INTO clinic_schedules(clinic_id,workspace_id,min_notice_minutes,max_advance_days,version) VALUES(1,1,60,45,7);
INSERT INTO schedule_exceptions(id,clinic_id,workspace_id,from_date,through_date,label)
 VALUES(1,1,1,'2027-01-01','2027-01-03','Cierre'),(2,1,1,'2027-01-05','2027-01-05','Especial');
INSERT INTO schedule_blocks(clinic_id,workspace_id,specialty_context_id,weekday,start_minute,end_minute,slot_minutes)
 VALUES(1,1,1,1,480,600,20),(1,1,1,1,600,720,20),(1,1,1,3,840,915,20);
INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name,specialty_context_id,specialty_snapshot,clinic_snapshot,provenance)
 SELECT 1,1,'2027-01-04 08:00Z','2027-01-04 08:20Z','Synthetic',1,
        jsonb_build_object('id',s.id::text,'code',s.code,'name',s.display_name),
        public.booking_clinic_snapshot(1),public.booking_block_snapshot(1)
 FROM specialties s WHERE s.id=1;
`); err != nil {
		t.Fatal(err)
	}
	return pool
}
func TestClinicsIntegrityAndRestart(t *testing.T) {
	pool := initializedFixture(t)
	clinicalFixture(t, pool)
	insert := "INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,status) VALUES($1,$2,'Argentina','Buenos Aires','Mar del Plata','Synthetic 123',$3,$4) RETURNING id"
	var first, second int64
	if err := pool.QueryRow(insert, 1, "A", "America/Argentina/Buenos_Aires", "ACTIVE").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(insert, 1, "B", "UTC", "INACTIVE").Scan(&second); err != nil || second <= first {
		t.Fatal("identity sequence failed", err)
	}
	if _, err := pool.Exec(insert, 2, "A", "Europe/Madrid", "ARCHIVED"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query, code string
		args              []any
	}{
		{"unknown owner", insert, "23503", []any{999, "C", "UTC", "ACTIVE"}},
		{"empty name", insert, "23514", []any{1, "  ", "UTC", "ACTIVE"}},
		{"unknown zone", insert, "23514", []any{1, "C", "America/Unknown", "ACTIVE"}},
		{"offset zone", insert, "23514", []any{1, "C", "+03:00", "ACTIVE"}},
		{"abbreviation zone", insert, "23514", []any{1, "C", "ART", "ACTIVE"}},
		{"invalid status", insert, "23514", []any{1, "C", "UTC", "DELETED"}},
		{"negative version", "UPDATE clinics SET version=-1 WHERE id=$1", "23514", []any{first}},
		{"empty address", "UPDATE clinics SET address=' ' WHERE id=$1", "23514", []any{first}},
		{"backwards timestamp", "UPDATE clinics SET updated_at=created_at-interval '1 second' WHERE id=$1", "23514", []any{first}},
		{"owner deletion", "DELETE FROM doctor_workspaces WHERE id=$1", "23503", []any{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(tc.query, tc.args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
	before := readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c")
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	if before != readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c") {
		t.Fatal("restart changed places")
	}
	if readString(t, pool, "SELECT count(*)::text FROM patients WHERE user_id='m1-owner'") != "1" {
		t.Fatal("creating multiple places changed the owner's patient list")
	}
}

func TestPatientInsertionFencesConcurrentSpecialtyRebinding(t *testing.T) {
	pool := initializedFixture(t)
	clinicalFixture(t, pool)
	if _, err := pool.Exec(`INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(3,true,'fixture-concurrent',now(),'Synthetic','[]','[]','[]',now(),0);
 INSERT INTO doctor_workspaces(id,user_id,version,created_at,updated_at) VALUES(3,'race-owner',0,now(),now());
 INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,version,current_step_key,onboarding_completed,created_at,updated_at) VALUES(3,1,0,'completed',true,now(),now())`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO patients(id,full_name,birth_date,user_id,version,workspace_version,updated_at,form_definition_snapshot,form_values,formula_context,formula_results,workspace_id,specialty_context_id)
 SELECT 3,'Synthetic','2000-01-01','race-owner',0,0,now(),'{}','{}','{}','{}',workspace_id,id FROM doctor_specialty_contexts WHERE workspace_id=3`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var pid int
	if err = conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := conn.ExecContext(ctx, "UPDATE doctor_specialty_contexts SET specialty_id=3 WHERE workspace_id=3")
		done <- err
	}()
	waiting := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err = pool.QueryRowContext(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("specialty update did not wait for patient insertion")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pg *pgconn.PgError
	if err = <-done; !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatal("concurrent rebinding changed a used context", err)
	}
}

func TestPatientContextDatabaseOwnershipAndImmutability(t *testing.T) {
	pool := initializedFixture(t)
	clinicalFixture(t, pool)
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || len(pg.Code) < 2 || pg.Code[:2] != "23" {
			t.Fatal("ownership invariant was not enforced", err)
		}
	}
	base := `INSERT INTO patients(full_name,birth_date,user_id,version,workspace_version,updated_at,form_definition_snapshot,form_values,formula_context,formula_results,workspace_id,specialty_context_id) VALUES('Synthetic','2000-01-01','m1-owner',0,0,now(),'{}','{}','{}','{}',`
	reject(base + "1,NULL)")
	reject(base + "1,999999)")
	reject(base + "1,(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2))")
	reject(base + "2,(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2))")
	reject("UPDATE patients SET specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2) WHERE id=1")
	reject("UPDATE patients SET user_id='m1-other',workspace_id=2 WHERE id=1")
	for _, table := range []string{"clinical_encounters", "patient_prescription_records", "patient_medical_studies", "patient_vaccinations"} {
		reject("UPDATE " + table + " SET patient_id=2 WHERE id=1")
	}
	reject("UPDATE patient_prescription_records SET author_user_id='m1-other' WHERE id=1")
	reject("UPDATE doctor_specialty_contexts SET workspace_id=2 WHERE workspace_id=1")
	if _, err := pool.Exec("INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(3,true,'fixture-other',now(),'Synthetic','[]','[]','[]',now(),0)"); err != nil {
		t.Fatal(err)
	}
	reject("UPDATE doctor_specialty_contexts SET specialty_id=3 WHERE workspace_id=1")
	reject("UPDATE clinical_encounters SET specialty_id=3 WHERE id=1")
	reject("INSERT INTO patient_calculation_logs(patient_id,user_id,encounter_id,calculation_code,calculation_name,input_snapshot,formatted_result,created_at) VALUES(2,'m1-other',1,'fixture','Synthetic','{}','Synthetic',now())")
	reject("INSERT INTO patient_calculation_logs(patient_id,user_id,calculation_code,calculation_name,input_snapshot,formatted_result,created_at) VALUES(1,'m1-other','fixture','Synthetic','{}','Synthetic',now())")
	// No new deletion cascades: existing historical children still prevent raw deletion.
	reject("DELETE FROM patients WHERE id=1")
}

func TestScheduleBlocksRelationalIntegrity(t *testing.T) {
	pool := scheduleFixture(t)
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code[:2] != "23" {
			t.Fatal("invalid block admitted", err)
		}
	}
	base := "INSERT INTO schedule_blocks(clinic_id,workspace_id,specialty_context_id,weekday,start_minute,end_minute,slot_minutes) SELECT 1,1,id,2,540,600,"
	reject(base + "0 FROM doctor_specialty_contexts WHERE workspace_id=1")
	reject(base + "20 FROM doctor_specialty_contexts WHERE workspace_id=2")
	reject("UPDATE schedule_blocks SET start_minute=end_minute WHERE id=(SELECT min(id) FROM schedule_blocks)")
	reject("UPDATE schedule_blocks SET weekday=NULL WHERE exception_id IS NULL")
	reject("UPDATE schedule_blocks SET exception_id=(SELECT id FROM schedule_exceptions LIMIT 1) WHERE weekday IS NOT NULL")
	reject("UPDATE schedule_blocks SET clinic_id=2 WHERE id=(SELECT min(id) FROM schedule_blocks)")
	reject("UPDATE schedule_exceptions SET through_date=from_date-1")
	reject("UPDATE schedule_blocks SET start_minute=480,end_minute=720 WHERE weekday=1")
	reject("UPDATE schedule_exceptions SET from_date='2027-01-01',through_date='2027-01-05'")
	reject("DELETE FROM doctor_specialty_contexts WHERE workspace_id=1")
}

func TestBookingContextsSQLIntegrity(t *testing.T) {
	pool := scheduleFixture(t)
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code[:2] != "23" {
			t.Fatal("invalid booking mutation accepted", err)
		}
	}
	reject("UPDATE bookings SET specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2),version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
	reject("UPDATE bookings SET patient_id=2,version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
	reject(`UPDATE bookings SET specialty_snapshot=jsonb_set(specialty_snapshot,'{name}','"Reclassified"'),version=version+1 WHERE id=(SELECT min(id) FROM bookings)`)
	reject(`UPDATE bookings SET provenance='{"kind":"SCHEDULE_BLOCK","blockId":"999999"}',version=version+1 WHERE id=(SELECT min(id) FROM bookings)`)
	reject(`INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,status,contact_name,specialty_context_id,specialty_snapshot,clinic_snapshot,provenance)
 SELECT workspace_id,clinic_id,start_at,end_at,'CANCELLED','Legacy cannot be newly inserted',specialty_context_id,specialty_snapshot,clinic_snapshot,'{"kind":"LEGACY_CONTEXT_ONLY"}'::jsonb FROM bookings LIMIT 1`)
	if _, err := pool.Exec("UPDATE bookings SET patient_id=1,version=version+1 WHERE id=(SELECT min(id) FROM bookings)"); err != nil {
		t.Fatal(err)
	}
	reject("UPDATE bookings SET patient_id=NULL,version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
}
