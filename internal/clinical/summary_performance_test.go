package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

// This experiment measures the service method, excluding HTTP, audit middleware,
// fixture creation, GC preparation and correctness assertions. It intentionally
// has no timing threshold: machine-dependent timings are evidence, not a gate.
// The existing fixture additionally requires an explicitly opted-in, loopback
// database named doctorchart_integrity_test and never uses the main database.
func TestClinicalSummaryPerformance(t *testing.T) {
	if os.Getenv("DOCTORCHART_SUMMARY_BENCHMARK") != "1" {
		t.Skip("set DOCTORCHART_SUMMARY_BENCHMARK=1 for the disposable summary experiment")
	}
	services, pool := integrityFixture(t, 100, 10001)
	base := services[0]
	ctx := context.Background()
	// Use the same real history block as the existing persistence regression.
	if _, err := pool.Exec(ctx, `WITH history AS (
	 SELECT w.published_definition->'patientForm'->'categories'->1 AS category
	 FROM wizard_defaults w JOIN specialties s ON s.id=w.specialty_id WHERE s.code='general_medicine'
	) UPDATE doctor_specialty_contexts SET definition=jsonb_set(definition,
	 '{encounterForm,categories}',(definition->'encounterForm'->'categories') || jsonb_build_array(history.category))
	 FROM history WHERE workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner')`); err != nil {
		t.Fatal(err)
	}
	tracer := &summaryMeasurementTracer{}
	config := pool.Config().Copy()
	config.ConnConfig.Tracer = tracer
	measuredPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(measuredPool.Close)
	service, err := New(measuredPool, workspace.New(measuredPool, base.forms), base.forms, base.rules, base.files)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{10, 100, 1000, 10000} {
		for _, snapshots := range []int{1, 3} {
			for _, annulledPercent := range []int{0, 50} {
				t.Run(fmt.Sprintf("records_%d/snapshots_%d/annulled_%d", count, snapshots, annulledPercent), func(t *testing.T) {
					patient := integrityPatient(t, base)
					scope := api.WithSpecialtyContext(ctx, patient.ContextID)
					request := integrityEncounterRequest(patient.ID, "FIRST_VISIT")
					values := request["values"].(map[string]any)
					values["diagnosis"] = "Diagnóstico sintético de rendimiento"
					values["sys_allergyStatus"] = "PRESENT"
					values["sys_allergies"] = "Antecedente sintético presente sólo en la consulta más antigua"
					oldest, err := base.CreateEncounter(scope, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, request)
					if err != nil {
						t.Fatal(err)
					}
					// The public API assigns the clinical date. Date this explicitly
					// synthetic fixture before generating later consultations, using
					// the normal revision binding rather than bypassing its trigger.
					if err := base.transaction(scope, false, func(tx pgx.Tx) error {
						if err := bindRevision(scope, tx, "integrity-owner", "Preparación sintética del experimento SP-04F"); err != nil {
							return err
						}
						_, err := tx.Exec(scope, `UPDATE clinical_encounters SET encounter_date=encounter_date-interval '1 year',
						 version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1`, oldest.ID)
						return err
					}); err != nil {
						t.Fatal(err)
					}
					// Bulk inserts are synthetic fixture setup, outside the measurement.
					// Three snapshots differ in a saved category label while retaining
					// exactly the same clinical semantics and history visibility.
					if _, err := pool.Exec(scope, `INSERT INTO clinical_encounters (
					 patient_id,specialty_id,encounter_date,consultation_type,chief_complaint,
					 primary_diagnosis_description,form_values,form_definition_snapshot,
					 formula_context,formula_results,workspace_version,version,updated_at,annulled_at)
					 SELECT e.patient_id,e.specialty_id,e.encounter_date+i*interval '1 minute','FOLLOW_UP',
					 e.chief_complaint,e.primary_diagnosis_description,
					 e.form_values-'sys_allergyStatus'-'sys_allergies',
					 CASE WHEN $3::integer=1 OR i%3=0 THEN e.form_definition_snapshot ELSE
					 jsonb_set(e.form_definition_snapshot,'{categories,0,label}',to_jsonb('Snapshot sintético de rendimiento ' || (i%3)::text)) END,
					 e.formula_context,e.formula_results,e.workspace_version,0,e.updated_at,
					 CASE WHEN $4::integer=50 AND i%2=1 THEN e.updated_at ELSE NULL END
					 FROM clinical_encounters e CROSS JOIN generate_series(1,$2::integer-1) i WHERE e.id=$1`,
						oldest.ID, count, snapshots, annulledPercent); err != nil {
						t.Fatal(err)
					}
					var total, active, distinctSnapshots int
					var latestID int64
					if err := pool.QueryRow(scope, `SELECT count(*),count(*) FILTER (WHERE annulled_at IS NULL),
					 count(DISTINCT form_definition_snapshot),max(id) FILTER (WHERE annulled_at IS NULL)
					 FROM clinical_encounters WHERE patient_id=$1`, patient.ID).Scan(&total, &active, &distinctSnapshots, &latestID); err != nil {
						t.Fatal(err)
					}
					if total != count || distinctSnapshots != snapshots || (annulledPercent == 50 && active != count/2) {
						t.Fatalf("invalid experiment fixture: total=%d active=%d snapshots=%d", total, active, distinctSnapshots)
					}
					warmup, err := service.ClinicalSummary(scope, "integrity-owner", patient.ID)
					if err != nil {
						t.Fatal(err)
					}
					expected, err := json.Marshal(warmup)
					if err != nil {
						t.Fatal(err)
					}
					const repetitions = 5
					durations := make([]int64, 0, repetitions)
					var allocated, statements, selects uint64
					var heapDelta int64
					for i := range repetitions {
						runtime.GC()
						var before, after runtime.MemStats
						runtime.ReadMemStats(&before)
						tracer.reset()
						started := time.Now()
						result, err := service.ClinicalSummary(scope, "integrity-owner", patient.ID)
						elapsed := time.Since(started)
						runtime.ReadMemStats(&after)
						if err != nil {
							t.Fatal(err)
						}
						calls, queries := tracer.statements.Load(), tracer.selects.Load()
						if i == 0 {
							statements, selects = calls, queries
						} else if statements != calls || selects != queries {
							t.Fatalf("unstable SQL count: %d/%d, expected %d/%d", calls, queries, statements, selects)
						}
						encoded, err := json.Marshal(result)
						if err != nil || string(encoded) != string(expected) {
							t.Fatal("summary changed between repeated reads", err)
						}
						allergy := result.History["ALLERGY"]
						if allergy.Status != "PRESENT" || allergy.SourceID != oldest.ID || len(allergy.Items) != 1 ||
							result.LastEncounter == nil || result.LastEncounter.ID != latestID {
							t.Fatal("old history lost or annulled encounter presented as current")
						}
						durations = append(durations, elapsed.Nanoseconds())
						allocated += after.TotalAlloc - before.TotalAlloc
						heapDelta += int64(after.HeapAlloc) - int64(before.HeapAlloc)
					}
					sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
					measurement := struct {
						Records, Active, Snapshots, AnnulledPercent, Repetitions int
						MinNS, MedianNS, MaxNS, HeapDeltaBytesPerCall            int64
						AllocatedBytesPerCall, SQLStatements, SQLSelects         uint64
					}{count, active, snapshots, annulledPercent, repetitions,
						durations[0], durations[repetitions/2], durations[repetitions-1], heapDelta / repetitions,
						allocated / repetitions, statements, selects}
					encoded, err := json.Marshal(measurement)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("SP04F_SUMMARY %s", encoded)
				})
			}
		}
	}
}

// Collect counts only: never retain SQL arguments, statements or clinical data.
type summaryMeasurementTracer struct {
	statements atomic.Uint64
	selects    atomic.Uint64
}

func (tracer *summaryMeasurementTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	tracer.statements.Add(1)
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(data.SQL)), "SELECT ") {
		tracer.selects.Add(1)
	}
	return ctx
}

func (*summaryMeasurementTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (tracer *summaryMeasurementTracer) reset() {
	tracer.statements.Store(0)
	tracer.selects.Store(0)
}
