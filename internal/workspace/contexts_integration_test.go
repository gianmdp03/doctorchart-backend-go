package workspace_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func contextFixture(t *testing.T) (*workspace.Store, *pgxpool.Pool) {
	t.Helper()
	raw := os.Getenv("BASELINE_TEST_ADMIN_DSN")
	if raw == "" {
		t.Skip("owned disposable PostgreSQL admin DSN required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "127.0.0.1" || (u.Port() != "55439" && u.Port() != "55440" && u.Port() != "55442" && u.Port() != "55444" && u.Port() != "55468" && u.Port() != "55470") || u.Path != "/postgres" || u.User.Username() != "migration_fixture" {
		t.Fatal("owned specialty M1 fixture required")
	}
	admin, err := pgxpool.New(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("specialty_m1_%d", time.Now().UnixNano())
	if _, err = admin.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	v, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err = bootstrap.Run(t.Context(), pool, v); err != nil {
		t.Fatal(err)
	}
	return workspace.New(pool, v), pool
}
func number(n int64) json.Number { return json.Number(fmt.Sprint(n)) }
func assertFailure(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Status != status || failure.Code != code {
		t.Fatalf("expected %d/%s, got %v", status, code, err)
	}
}
func TestSpecialtyContextLifecycleAndOwnership(t *testing.T) {
	for _, code := range []string{"pediatrics", "general_medicine", "dermatology"} {
		t.Run(code, func(t *testing.T) {
			s, pool := contextFixture(t)
			ctx := t.Context()
			var id int64
			if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code=$1", code).Scan(&id); err != nil {
				t.Fatal(err)
			}
			a, err := s.Select(ctx, "owner", map[string]any{"specialtyId": number(id), "expectedVersion": nil})
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.Select(ctx, "other", map[string]any{"specialtyId": number(id), "expectedVersion": nil})
			if err != nil {
				t.Fatal(err)
			}
			if a.ID == b.ID || a.ContextID == b.ContextID || a.ContextID == 0 || a.Version != 0 || a.Completed || len(a.Draft) == 0 {
				t.Fatal("missing independent initial context")
			}
			if _, err := pool.Exec(ctx, "INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,status) VALUES($1,'Shared clinic','Argentina','Buenos Aires','Mar del Plata','Fixture 123','America/Argentina/Buenos_Aires','ACTIVE')", a.ID); err != nil {
				t.Fatal(err)
			}
			// Backend HTTP route resolves ownership, not a client-selected context.
			mux := http.NewServeMux()
			s.Routes(mux)
			handler := api.Gateway("context-fixture", api.Dispatch(mux))
			r := httptest.NewRequest("POST", "/api/doctorchart/workspace/finish-onboarding", strings.NewReader(fmt.Sprintf(`{"expectedVersion":0,"specialtyContextId":%d}`, b.ContextID)))
			r.Header.Set("X-Gateway-Secret", "context-fixture")
			r.Header.Set("X-User-Id", "owner")
			r.Header.Set("X-User-Role", "SUPER_ADMIN")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 404 || !strings.Contains(w.Body.String(), "SPECIALTY_CONTEXT_NOT_FOUND") {
				t.Fatalf("foreign context HTTP: %d %s", w.Code, w.Body)
			}
			draft, err := wizard.Decode(a.Draft)
			if err != nil {
				t.Fatal(err)
			}
			// A personal module choice must not change defaults or another doctor's draft.
			modules := draft["modulesConfig"].(map[string]any)
			modules["AI_ASSISTANT"] = modules["AI_ASSISTANT"] != true
			saved, err := s.Edit(ctx, "owner", "draft", map[string]any{"specialtyContextId": number(a.ContextID), "expectedVersion": number(a.Version), "definition": draft, "currentStepKey": a.Step})
			if err != nil {
				t.Fatal(err)
			}
			reloaded, err := s.Setup(ctx, "owner")
			if err != nil || reloaded.Version != saved.Version || reloaded.ContextID != a.ContextID || string(reloaded.Draft) != string(saved.Draft) {
				t.Fatal("resume failed", err)
			}
			other, err := s.Setup(ctx, "other")
			if err != nil || string(other.Draft) != string(b.Draft) || other.Version != b.Version {
				t.Fatal("cross-owner mutation", err)
			}
			_, err = s.Edit(ctx, "owner", "finish", map[string]any{"expectedVersion": number(a.Version)})
			assertFailure(t, err, 409, "VERSION_CONFLICT")
			applied, err := s.Edit(ctx, "owner", "finish", map[string]any{"specialtyContextId": number(a.ContextID), "expectedVersion": number(saved.Version)})
			if err != nil {
				t.Fatal(err)
			}
			if !applied.Completed || len(applied.Definition) == 0 || len(applied.Draft) != 0 || applied.SourceVersion == nil || *applied.SourceVersion != 1 {
				t.Fatal("configuration not applied")
			}
			prefs, err := s.UpdatePreferences(ctx, "owner", map[string]any{"content": "administrative note"}, true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.UpdatePreferences(ctx, "owner", map[string]any{"expectedVersion": number(prefs.Version), "patientColumnsConfig": map[string]any{"phone": false}}, false)
			if err != nil {
				t.Fatal(err)
			}
			reloaded, err = s.Setup(ctx, "owner")
			if err != nil || reloaded.Version != applied.Version || string(reloaded.Definition) != string(applied.Definition) {
				t.Fatal("preferences changed clinical definition/version", err)
			}
			// Row locks used by clinical operations still fence configuration application.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err = tx.Exec(ctx, "SELECT id FROM doctor_workspaces WHERE id=$1 FOR SHARE", a.ID); err != nil {
				t.Fatal(err)
			}
			blocked, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			_, err = s.Edit(blocked, "owner", "reset", map[string]any{"expectedVersion": number(applied.Version)})
			cancel()
			if err == nil {
				t.Fatal("clinical lock ignored")
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			reset, err := s.Edit(ctx, "owner", "reset", map[string]any{"expectedVersion": number(applied.Version)})
			if err != nil {
				t.Fatal(err)
			}
			if string(reset.Definition) != string(applied.Definition) || len(reset.Draft) == 0 {
				t.Fatal("reset changed effective definition")
			}
			discarded, err := s.Edit(ctx, "owner", "discard", map[string]any{"expectedVersion": number(reset.Version)})
			if err != nil || len(discarded.Draft) != 0 || !discarded.Completed {
				t.Fatal("discard failed", err)
			}
			prefsAfter, err := s.Preferences(ctx, "owner")
			if err != nil || prefsAfter.Version != 2 || prefsAfter.Notes == nil || *prefsAfter.Notes != "administrative note" {
				t.Fatal("wizard changed administrative preferences", err)
			}
			_, err = s.Select(ctx, "owner", map[string]any{"specialtyId": number(id), "expectedVersion": number(discarded.Version)})
			assertFailure(t, err, 409, "SPECIALTY_LOCKED")
			var contexts, workspaces, clinics int
			if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM doctor_workspaces WHERE user_id='owner'),(SELECT count(*) FROM doctor_specialty_contexts WHERE workspace_id=$1),(SELECT count(*) FROM clinics WHERE workspace_id=$1)", a.ID).Scan(&workspaces, &contexts, &clinics); err != nil || workspaces != 1 || contexts != 1 || clinics != 1 {
				t.Fatal("M1 cardinality/clinic relationship", err)
			}
			var differentSpecialty int64
			if err := pool.QueryRow(ctx, "SELECT min(id) FROM specialties WHERE id<>$1", id).Scan(&differentSpecialty); err != nil {
				t.Fatal(err)
			}
			for _, specialtyID := range []int64{id, differentSpecialty} {
				if _, err := pool.Exec(ctx, "INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,version,current_step_key,onboarding_completed,created_at,updated_at) VALUES($1,$2,0,'specialty',false,now(),now())", a.ID, specialtyID); (err == nil) == (specialtyID == id) {
					t.Fatal("workspace-specialty uniqueness or M5 multiple contexts")
				}
			}
		})
	}
}

func TestSpecialtyContextConcurrentCreationAndUpdate(t *testing.T) {
	s, pool := contextFixture(t)
	ctx := t.Context()
	var id int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='pediatrics'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := s.Select(ctx, "owner", map[string]any{"specialtyId": number(id), "expectedVersion": nil})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			assertFailure(t, err, 409, "VERSION_CONFLICT")
		}
	}
	if successes != 1 {
		t.Fatal("concurrent creation did not choose one winner")
	}
	a, err := s.Setup(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	results = make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := s.Edit(ctx, "owner", "finish", map[string]any{"expectedVersion": number(a.Version)})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes = 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			assertFailure(t, err, 409, "VERSION_CONFLICT")
		}
	}
	if successes != 1 {
		t.Fatal("concurrent application lost version fence")
	}
}
