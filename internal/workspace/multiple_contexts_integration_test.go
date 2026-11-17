package workspace_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/testfixture"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
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

func TestCatalogPublicationControlsNewContextsOnly(t *testing.T) {
	s, pool := contextFixture(t)
	ctx := t.Context()
	var general int64
	var definition []byte
	if err := pool.QueryRow(ctx, `SELECT s.id,w.published_definition FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE s.code='general_medicine'`).Scan(&general, &definition); err != nil {
		t.Fatal(err)
	}
	form, err := wizard.Decode(definition)
	if err != nil {
		t.Fatal(err)
	}
	assertCatalog := func(want int, code string) {
		t.Helper()
		entries, err := s.Specialties(ctx, false)
		if err != nil || len(entries) != want {
			t.Fatalf("catalog size: %+v %v", entries, err)
		}
		if code != "" {
			for _, entry := range entries {
				if entry.Code == code {
					return
				}
			}
			t.Fatalf("published entry missing: %s", code)
		}
	}
	assertCatalog(3, "")
	request := map[string]any{"code": "Invalid Code", "name": "Especialidad de prueba", "definition": form}
	_, err = s.CreateSpecialty(ctx, request)
	assertFailure(t, err, 400, "INVALID_REQUEST")
	request["code"] = "sp01_fixture"
	created, err := s.CreateSpecialty(ctx, request)
	if err != nil || created.Active {
		t.Fatalf("catalog draft: %+v %v", created, err)
	}
	_, err = s.CreateSpecialty(ctx, request)
	assertFailure(t, err, 409, "SPECIALTY_CODE_EXISTS")
	if _, err := s.Select(ctx, "multi", map[string]any{"specialtyId": number(general), "expectedVersion": nil}); err != nil {
		t.Fatal(err)
	}
	selectRequest := map[string]any{"specialtyId": number(created.ID), "expectedVersion": nil}
	addRequest := map[string]any{"specialtyId": number(created.ID)}
	assertUnavailable := func() {
		t.Helper()
		assertCatalog(3, "")
		_, err := s.Select(ctx, "new-owner", selectRequest)
		assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
		_, err = s.AddContext(ctx, "multi", addRequest)
		assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
		_, err = s.Wizard(ctx, created.ID, true)
		assertFailure(t, err, 404, "SPECIALTY_NOT_FOUND")
	}
	assertUnavailable()
	_, err = s.Select(ctx, "missing", map[string]any{"specialtyId": number(9999), "expectedVersion": nil})
	assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
	_, err = s.AddContext(ctx, "multi", map[string]any{"specialtyId": number(9999)})
	assertFailure(t, err, 404, "SPECIALTY_NOT_FOUND")
	_, err = s.UpdateSpecialty(ctx, created.ID, map[string]any{"expectedVersion": number(created.Version), "active": true})
	assertFailure(t, err, 409, "TEMPLATE_APPROVAL_REQUIRED")
	if err := testfixture.ApproveDraft(ctx, pool, created.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.SaveWizard(ctx, created.ID, map[string]any{"expectedVersion": number(0)}, true)
	if err != nil || published.PublishedVersion != 1 {
		t.Fatalf("publish: %+v %v", published, err)
	}
	assertCatalog(4, created.Code)
	if _, err = s.Wizard(ctx, created.ID, true); err != nil {
		t.Fatal(err)
	}
	initial, err := s.Select(ctx, "first-choice", selectRequest)
	if err != nil || initial.Specialty.ID != created.ID {
		t.Fatalf("first selection: %+v %v", initial, err)
	}
	extra, err := s.AddContext(ctx, "multi", addRequest)
	if err != nil || extra.DraftSourceVersion == nil || *extra.DraftSourceVersion != published.PublishedVersion {
		t.Fatalf("new context has no published draft: %+v %v", extra, err)
	}
	draft, err := wizard.Decode(extra.Draft)
	if err != nil || !reflect.DeepEqual(draft, form) {
		t.Fatalf("new context definition differs from publication: %v", err)
	}
	version := map[string]any{"specialtyContextId": number(extra.ContextID), "expectedVersion": number(extra.Version)}
	if extra, err = s.Edit(ctx, "multi", "finish", version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSpecialty(ctx, created.ID, map[string]any{"expectedVersion": number(created.Version + 1), "active": false}); err != nil {
		t.Fatal(err)
	}
	// A different doctor with an existing workspace still cannot add the inactive specialty.
	if _, err = s.Select(ctx, "after-deactivation", map[string]any{"specialtyId": number(general), "expectedVersion": nil}); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddContext(ctx, "after-deactivation", addRequest)
	assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
	_, err = s.Select(ctx, "new-owner", selectRequest)
	assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
	assertCatalog(3, "")
	scope := api.WithSpecialtyContext(ctx, extra.ContextID)
	saved, err := s.Setup(scope, "multi")
	if err != nil || !saved.Completed || saved.Version != extra.Version {
		t.Fatalf("historical context %d changed: %+v %v", extra.ContextID, saved, err)
	}
	savedForm, err := wizard.Decode(saved.Definition)
	if err != nil || !reflect.DeepEqual(savedForm, form) {
		t.Fatalf("historical context lost its definition: %v", err)
	}
	_, err = s.Setup(scope, "first-choice")
	assertFailure(t, err, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	if admin, err := s.Wizard(ctx, created.ID, false); err != nil || admin == nil {
		t.Fatalf("inactive publication not administrable: %+v %v", admin, err)
	}
}
