package workspace_test

import (
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"sync"
	"testing"
)

func TestMultipleContextsIndependentWizardPreferencesAndConcurrentCreation(t *testing.T) {
	s, pool := contextFixture(t)
	ctx := t.Context()
	var general, dermatology, pediatrics int64
	for code, target := range map[string]*int64{"general_medicine": &general, "dermatology": &dermatology, "pediatrics": &pediatrics} {
		if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code=$1", code).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	original, err := s.Select(ctx, "multi", map[string]any{"specialtyId": number(general), "expectedVersion": nil})
	if err != nil {
		t.Fatal(err)
	}
	original, err = s.Edit(ctx, "multi", "finish", map[string]any{"specialtyContextId": number(original.ContextID), "expectedVersion": number(original.Version)})
	if err != nil {
		t.Fatal(err)
	}
	originalScope := api.WithSpecialtyContext(ctx, original.ContextID)
	if _, err = s.UpdatePreferences(originalScope, "multi", map[string]any{"content": "Sólo medicina general"}, true); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := s.AddContext(ctx, "multi", map[string]any{"specialtyId": number(dermatology)})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			assertFailure(t, err, 409, "SPECIALTY_ALREADY_ADDED")
		}
	}
	if winners != 1 {
		t.Fatal("duplicate context race", winners)
	}
	third, err := s.AddContext(ctx, "multi", map[string]any{"specialtyId": number(pediatrics)})
	if err != nil {
		t.Fatal(err)
	}
	contexts, err := s.Contexts(ctx, "multi")
	if err != nil || len(contexts) != 3 {
		t.Fatal(contexts, err)
	}
	ready := 0
	for _, c := range contexts {
		if c.Ready {
			ready++
		}
	}
	if ready != 1 {
		t.Fatal("pending wizard blocked/finished another context", contexts)
	}
	if third.ID != original.ID || third.ContextID == original.ContextID || third.Completed || len(third.Draft) == 0 || len(third.Definition) != 0 {
		t.Fatal("new context did not get independent published draft")
	}
	_, err = s.Setup(ctx, "multi")
	assertFailure(t, err, 400, "SPECIALTY_CONTEXT_REQUIRED")
	_, err = s.Setup(api.WithSpecialtyContext(ctx, original.ContextID), "stranger")
	assertFailure(t, err, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	pref, err := s.Preferences(api.WithSpecialtyContext(ctx, third.ContextID), "multi")
	if err != nil || pref.Notes != nil {
		t.Fatal("preferences leaked", pref, err)
	}
	if _, err = s.Edit(ctx, "multi", "finish", map[string]any{"specialtyContextId": number(third.ContextID), "expectedVersion": number(third.Version)}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Setup(originalScope, "multi")
	if err != nil || first.Version != original.Version || !first.Completed {
		t.Fatal("original wizard changed", first, err)
	}
	pref, err = s.Preferences(originalScope, "multi")
	if err != nil || pref.Notes == nil || *pref.Notes != "Sólo medicina general" {
		t.Fatal("original notes lost", pref, err)
	}
	var workspaces int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM doctor_workspaces WHERE user_id='multi'").Scan(&workspaces); err != nil || workspaces != 1 {
		t.Fatal(workspaces, err)
	}
}
