package dataexport

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/db"
)

func TestExportSchemaCoverage(t *testing.T) {
	// This lists fields with a different representation and fields intentionally
	// omitted from an otherwise exported row.
	omitted := map[string]string{
		"booking_public_details":      "challenge_hash",
		"clinical_access_events":      "request_id",
		"clinical_document_issues":    "pdf",
		"notification_outbox":         "dedup_key recovery_id payload attempts lease_until lease_id last_code portal_code_id",
		"patient_medical_studies":     "media_asset_id stored_file_name",
		"patients":                    "media_identity",
		"staff_accounts":              "password_hash recovery_hash created_at",
		"wizard_approvals":            "actor reference",
		"wizard_approval_revocations": "actor",
		"wizard_defaults":             "draft_experience draft_definition version updated_at",
	}
	// Media bytes and safe metadata travel through the study's attachment.
	// These tables contain operational state or are used by that storage path.
	otherHandling := strings.Fields(`app_schema_state booking_confirmation_access booking_idempotency
		booking_portal_codes clinic_entitlement_epoch media_assets media_events public_booking_rate_windows
		staff_email_recovery staff_invitations staff_rate_windows staff_sessions study_file_deletions`)

	tablePattern := regexp.MustCompile(`(?ms)^CREATE TABLE public\.(\w+) \(\r?\n(.*?)^\);`)
	columnPattern := regexp.MustCompile(`(?m)^ +(\w+) +\w+`)
	tables := make(map[string][]string)
	for _, matches := range tablePattern.FindAllStringSubmatch(db.Schema, -1) {
		table, definition := matches[1], matches[2]
		for _, column := range columnPattern.FindAllStringSubmatch(definition, -1) {
			switch column[1] {
			case "CONSTRAINT", "UNIQUE", "CHECK", "PRIMARY", "FOREIGN":
			default:
				tables[table] = append(tables[table], column[1])
			}
		}
	}
	for _, table := range otherHandling {
		if _, ok := tables[table]; !ok {
			t.Errorf("handling refers to missing table %s", table)
		}
		delete(tables, table)
	}
	for _, data := range [][]section{workspaceSections, contextSections} {
		for _, section := range data {
			actual, ok := tables[section.table]
			if !ok {
				t.Errorf("unexpected or duplicate projection %s", section.table)
			}
			documented := append(strings.Fields(section.fields), strings.Fields(omitted[section.table])...)
			slices.Sort(actual)
			slices.Sort(documented)
			if !slices.Equal(actual, documented) {
				t.Errorf("%s fields must be explicitly projected or omitted\nactual %v\ncovered %v", section.table, actual, documented)
			}
			delete(tables, section.table)
		}
	}
	if len(tables) != 0 {
		t.Errorf("tables missing from export inventory: %v", slices.Sorted(maps.Keys(tables)))
	}
}
