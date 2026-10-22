package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestPediatricAgeBoundaries(t *testing.T) {
	today := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ birth, group string }{
		{"2026-10-21", "NEONATAL"}, {"2026-09-24", "NEONATAL"},
		{"2026-09-23", "INFANT"}, {"2025-10-22", "INFANT"},
		{"2025-10-21", "EARLY_CHILDHOOD"}, {"2024-07-11", "EARLY_CHILDHOOD"},
		{"2021-10-22", "EARLY_CHILDHOOD"}, {"2021-10-21", "CHILD"},
		{"2017-10-22", "CHILD"}, {"2017-10-21", "CHILD"},
		{"2016-10-22", "CHILD"}, {"2016-10-21", "ADOLESCENT"},
		{"2008-10-21", "ADOLESCENT"}, {"2006-10-22", "ADOLESCENT"},
		{"2006-10-21", "ADULT"}, {"2026-10-22", "UNKNOWN"},
	} {
		t.Run(test.birth, func(t *testing.T) {
			birth, err := time.Parse("2006-01-02", test.birth)
			if err != nil {
				t.Fatal(err)
			}
			if got := pediatricAgeGroup(birth, today); got != test.group {
				t.Fatalf("group=%s want=%s", got, test.group)
			}
		})
	}
	if _, ok := pediatricRange("unrecognized", today); ok {
		t.Fatal("accepted an unknown category")
	}
}

func TestPediatricLeapDayAndExclusiveFilters(t *testing.T) {
	today := time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)
	if got := yearsBefore(today, 1).Format("2006-01-02"); got != "2027-02-28" {
		t.Fatal(got)
	}
	for _, group := range []string{"NEONATAL", "INFANT", "EARLY_CHILDHOOD", "CHILD", "ADOLESCENT", "ADULT"} {
		range_, _ := pediatricRange(group, today)
		if range_.after != nil {
			if range_.contains(*range_.after) || !range_.contains(range_.after.AddDate(0, 0, 1)) {
				t.Fatalf("exclusive lower bound: %s", group)
			}
		}
		if range_.through != nil {
			if !range_.contains(*range_.through) || range_.contains(range_.through.AddDate(0, 0, 1)) {
				t.Fatalf("inclusive upper bound: %s", group)
			}
		}
	}
	child := yearsBefore(today, 2)
	for _, filter := range []string{"EARLY_CHILDHOOD", "CHILD"} {
		bounds, _ := pediatricRange(filter, today)
		if got, want := bounds.contains(child), filter == "EARLY_CHILDHOOD"; got != want {
			t.Fatalf("two-year-old in %s=%v, want %v", filter, got, want)
		}
	}
}

func TestPediatricFiltersPartitionEveryAge(t *testing.T) {
	groups := []string{"NEONATAL", "INFANT", "EARLY_CHILDHOOD", "CHILD", "ADOLESCENT", "ADULT"}
	for _, date := range []string{"2026-10-22", "2028-02-29", "2029-02-28"} {
		today, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatal(err)
		}
		for birth := today.AddDate(-25, 0, 0); !birth.After(today); birth = birth.AddDate(0, 0, 1) {
			matches := 0
			for _, group := range groups {
				bounds, _ := pediatricRange(group, today)
				if bounds.contains(birth) {
					matches++
					if got := pediatricAgeGroup(birth, today); got != group {
						t.Fatalf("%s on %s: badge=%s, filter=%s", birth.Format("2006-01-02"), date, got, group)
					}
				}
			}
			if matches != 1 {
				t.Fatalf("%s on %s: belongs to %d filters, want exactly one", birth.Format("2006-01-02"), date, matches)
			}
		}
	}
}

func TestPediatricDirectoryFiltersAndSpecialtyIsolation(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	ctx := context.Background()
	// Keep growth enabled in the other workspace: capability must not opt it in.
	var otherSpecialty int64
	if err := pool.QueryRow(ctx, `INSERT INTO specialties(active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) SELECT active,'cardiology',now(),'Pediatría (editable name)',navigation,patient_record_actions,patient_record_tabs,now(),version FROM specialties WHERE code='pediatrics' RETURNING id`).Scan(&otherSpecialty); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE doctor_workspaces SET specialty_id=$1 WHERE user_id='integrity-other'`, otherSpecialty); err != nil {
		t.Fatal(err)
	}
	todayText := time.Now().In(services[0].viewZone(ctx)).Format("2006-01-02")
	today, err := time.Parse("2006-01-02", todayText)
	if err != nil {
		t.Fatal(err)
	}
	births := []time.Time{today, today.AddDate(0, 0, -27), today.AddDate(0, 0, -28)}
	for _, years := range []int{1, 2, 5, 6, 9, 10, 18, 20} {
		boundary := yearsBefore(today, years)
		births = append(births, boundary.AddDate(0, 0, 1), boundary, boundary.AddDate(0, 0, -1))
	}
	for _, owner := range []string{"integrity-owner", "integrity-other"} {
		for i, birth := range births {
			_, err := pool.Exec(ctx, `INSERT INTO patients(birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version) VALUES($1,'{}','{}','{}','{}',$2,now(),$3,0,0)`, birth, fmt.Sprintf("Fixture %02d", i), owner)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, filter := range []string{"ALL", "NEONATAL", "INFANT", "EARLY_CHILDHOOD", "CHILD", "ADOLESCENT", "ADULT"} {
		query := url.Values{"filter": {filter}, "size": {"100"}, "sort": {"fullName,asc"}}
		result, err := services[0].ListPatients(ctx, "integrity-owner", query)
		if err != nil {
			t.Fatalf("%s: %v", filter, err)
		}
		page := result.(map[string]any)
		items := page["content"].([]PatientList)
		want := 0
		bounds, _ := pediatricRange(filter, today)
		for _, birth := range births {
			if filter == "ALL" || bounds.contains(birth) {
				want++
			}
		}
		if len(items) != want || page["page"].(map[string]any)["totalElements"] != int64(want) {
			t.Fatalf("%s: rows=%d want=%d page=%v", filter, len(items), want, page["page"])
		}
		for _, item := range items {
			if filter != "ALL" && item.Group != filter {
				t.Fatalf("%s: patient %d has a different badge %s", filter, item.ID, item.Group)
			}
			birth, err := time.Parse("2006-01-02", item.Birth)
			if err != nil {
				t.Fatal(err)
			}
			if item.Group != pediatricAgeGroup(birth, today) {
				t.Fatalf("%s: inconsistent badge %s", filter, item.Group)
			}
			if item.Birth == todayText && item.Age != "0 días" {
				t.Fatalf("newborn age=%s", item.Age)
			}
		}
		if filter == "ALL" || filter == "INFANT" || filter == "ADULT" {
			other, err := services[0].ListPatients(ctx, "integrity-other", query)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range other.(map[string]any)["content"].([]PatientList) {
				if item.Group != ageGroup(item.Months) {
					t.Fatal("changed another specialty's classification")
				}
			}
		} else {
			query.Set("specialty", "pediatrics") // A query parameter cannot select the policy.
			_, err := services[0].ListPatients(ctx, "integrity-other", query)
			integrityError(t, err, 400, "INVALID_REQUEST")
		}
	}
	// Existing filters keep their original month-based semantics outside pediatrics.
	for _, filter := range []string{"ADULT", "MINOR", "INFANT", "TODDLER"} {
		result, err := services[0].ListPatients(ctx, "integrity-other", url.Values{"filter": {filter}, "size": {"100"}})
		if err != nil {
			t.Fatalf("other specialty %s: %v", filter, err)
		}
		want := 0
		for _, birth := range births {
			months := *ageMonths(birth, today)
			if (filter == "ADULT" && months >= 216) ||
				(filter == "MINOR" && months < 216) ||
				(filter == "INFANT" && months < 13) ||
				(filter == "TODDLER" && months >= 13 && months < 73) {
				want++
			}
		}
		page := result.(map[string]any)
		if len(page["content"].([]PatientList)) != want || page["page"].(map[string]any)["totalElements"] != int64(want) {
			t.Fatalf("changed other specialty's %s filter: %v, want %d", filter, page, want)
		}
	}
	// Filtering is server-side, so totals and pagination must match too.
	query := url.Values{"filter": {"EARLY_CHILDHOOD"}, "search": {"Fixture"}, "size": {"1"}, "page": {"1"}, "sort": {"fullName,asc"}}
	page, err := services[0].ListPatients(ctx, "integrity-owner", query)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.(map[string]any)["content"].([]PatientList)) != 1 {
		t.Fatal("filtered pagination failed")
	}

	// Reproduce the report through the HTTP route, with a real two-year-old row.
	const name = "Two-year-old regression"
	if _, err := pool.Exec(ctx, `INSERT INTO patients(birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version) VALUES($1,'{}','{}','{}','{}',$2,now(),'integrity-owner',0,0)`, yearsBefore(today, 2), name); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	services[0].DirectoryRoutes(mux)
	const secret = "pediatric-directory-fixture"
	handler := api.Gateway(secret, mux)
	for _, filter := range []string{"ALL", "NEONATAL", "INFANT", "EARLY_CHILDHOOD", "CHILD", "ADOLESCENT", "ADULT"} {
		query := url.Values{"filter": {filter}, "search": {name}, "size": {"1"}, "page": {"0"}}
		request := httptest.NewRequest(http.MethodGet, "/api/doctorchart/patients?"+query.Encode(), nil)
		request.Header.Set("X-Gateway-Secret", secret)
		request.Header.Set("X-User-Id", "integrity-owner")
		request.Header.Set("X-User-Plan", "BASIC")
		request.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).Format(time.RFC3339Nano))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d: %s", filter, response.Code, response.Body)
		}
		var result struct {
			Content []PatientList               `json:"content"`
			Page    struct{ TotalElements int } `json:"page"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		want := 0
		if filter == "ALL" || filter == "EARLY_CHILDHOOD" {
			want = 1
		}
		if len(result.Content) != want || result.Page.TotalElements != want {
			t.Fatalf("two-year-old: %s returned %d rows / %d total, want %d", filter, len(result.Content), result.Page.TotalElements, want)
		}
		if want == 1 && (result.Content[0].Name != name || result.Content[0].Group != "EARLY_CHILDHOOD") {
			t.Fatalf("two-year-old: wrong patient/category in %s: %+v", filter, result.Content)
		}
	}
}
