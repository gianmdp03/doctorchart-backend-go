package clinical

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// Clinical profile is business data in the existing personal_history document.
// No PostgreSQL object or baseline change is required. Resolved entries remain
// available as source records; missing historical dates are never invented.
type ClinicalEntry struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	Details    string `json:"details"`
	Date       string `json:"date"`
	Certainty  string `json:"certainty"`
	Status     string `json:"status"`
	RecordedAt string `json:"recordedAt"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
	VerifiedAt string `json:"verifiedAt,omitempty"`
	Author     string `json:"authorUserId"`
}

type ClinicalProfile struct {
	Entries            []ClinicalEntry `json:"entries"`
	NoKnownAllergiesAt string          `json:"noKnownAllergiesAt,omitempty"`
}

func readClinicalProfile(raw json.RawMessage) (ClinicalProfile, map[string]json.RawMessage, error) {
	p := ClinicalProfile{Entries: []ClinicalEntry{}}
	personal := map[string]json.RawMessage{}
	if len(raw) != 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &personal); err != nil {
			return p, nil, err
		}
	}
	if value := personal["clinicalProfile"]; len(value) != 0 {
		if err := json.Unmarshal(value, &p); err != nil {
			return p, nil, err
		}
	}
	if p.Entries == nil {
		p.Entries = []ClinicalEntry{}
	}
	return p, personal, nil
}

func historicalClinicalEntries(allergies, chronic, history json.RawMessage) ([]ClinicalEntry, error) {
	result := []ClinicalEntry{}
	seen := map[string]bool{}
	appendValues := func(kind string, raw json.RawMessage) error {
		if len(raw) == 0 || string(raw) == "null" {
			return nil
		}
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, text := range values {
			if text == "" || seen[kind+":"+text] {
				continue
			}
			seen[kind+":"+text] = true
			result = append(result, ClinicalEntry{ID: "historical-" + strconv.Itoa(len(result)), Kind: kind, Text: text, Certainty: "REPORTED", Status: "HISTORICAL"})
		}
		return nil
	}
	if err := appendValues("ALLERGY", allergies); err != nil {
		return nil, err
	}
	if err := appendValues("PROBLEM", chronic); err != nil {
		return nil, err
	}
	_, personal, err := readClinicalProfile(history)
	if err != nil {
		return nil, err
	}
	for _, field := range []struct{ key, kind string }{{"allergies", "ALLERGY"}, {"chronicOrPreviousDiseases", "PROBLEM"}, {"habitualMedication", "MEDICATION"}} {
		if err := appendValues(field.kind, personal[field.key]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func updateClinicalProfile(profile *ClinicalProfile, historical []ClinicalEntry, request map[string]any, birthDate, today, stamp, author string) error {
	operation, err := api.Text(request, "operation", 30)
	if err != nil {
		return err
	}
	switch operation {
	case "ADD":
		if err := api.Only(request, "expectedVersion", "operation", "kind", "text", "details", "date", "certainty"); err != nil {
			return err
		}
		if len(profile.Entries) >= 1000 {
			return api.Invalid("El registro clínico alcanzó el límite de entradas.")
		}
		kind, err := api.Text(request, "kind", 20)
		if err != nil {
			return err
		}
		if kind != "ALLERGY" && kind != "PROBLEM" && kind != "MEDICATION" && kind != "PENDING" {
			return api.Invalid("Seleccioná un tipo de registro válido.")
		}
		text, err := api.Text(request, "text", 300)
		if err != nil {
			return err
		}
		detailsValue, err := api.OptionalText(request, "details")
		details := ""
		if detailsValue != nil {
			details = *detailsValue
		}
		if api.JavaLength(details) > 2000 {
			return api.Invalid("El detalle supera el tamaño permitido.")
		}
		if err != nil {
			return err
		}
		date, err := api.Text(request, "date", 10)
		if err != nil {
			return err
		}
		if _, err := time.Parse("2006-01-02", date); err != nil || date < birthDate || (kind != "PENDING" && date > today) {
			return api.Invalid("La fecha debe ser válida, posterior al nacimiento y no futura salvo para un pendiente.")
		}
		certainty, err := api.Text(request, "certainty", 20)
		if err != nil {
			return err
		}
		if certainty != "CONFIRMED" && certainty != "REPORTED" {
			return api.Invalid("Indicá si el dato fue confirmado o informado.")
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		profile.Entries = append(profile.Entries, ClinicalEntry{ID: hex.EncodeToString(random[:]), Kind: kind, Text: text, Details: details, Date: date, Certainty: certainty, Status: "ACTIVE", RecordedAt: stamp, Author: author})
		if kind == "ALLERGY" {
			profile.NoKnownAllergiesAt = ""
		}
	case "RESOLVE", "CONFIRM":
		if err := api.Only(request, "expectedVersion", "operation", "entryId"); err != nil {
			return err
		}
		id, err := api.Text(request, "entryId", 50)
		if err != nil {
			return err
		}
		for i := range profile.Entries {
			entry := &profile.Entries[i]
			if entry.ID == id && entry.Status == "ACTIVE" {
				if operation == "CONFIRM" {
					if entry.Kind == "PENDING" {
						return api.Invalid("Los pendientes se marcan como realizados al completarlos.")
					}
					entry.Certainty = "CONFIRMED"
					entry.VerifiedAt = stamp
				} else {
					entry.Status = "RESOLVED"
					entry.ResolvedAt = stamp
				}
				return nil
			}
		}
		return api.Failure(409, "VERSION_CONFLICT", "El registro cambió. Actualizá antes de guardar.")
	case "NO_KNOWN_ALLERGIES":
		if err := api.Only(request, "expectedVersion", "operation"); err != nil {
			return err
		}
		for _, entry := range append(append([]ClinicalEntry{}, profile.Entries...), historical...) {
			if entry.Kind == "ALLERGY" && entry.Status != "RESOLVED" {
				return api.Invalid("Hay alergias registradas. Revisalas antes de confirmar ausencia de alergias conocidas.")
			}
		}
		profile.NoKnownAllergiesAt = stamp
	default:
		return api.Invalid("La operación clínica no es válida.")
	}
	return nil
}
