package clinical

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

type ClinicalHistoryGroup struct {
	Status    string   `json:"status"`
	Items     []string `json:"items"`
	Certainty string   `json:"certainty"`
	Date      string   `json:"date"`
	Source    string   `json:"source"`
	SourceID  int64    `json:"sourceId"`
}

func recordHistory(values map[string]any, source string, id int64, dates map[string]string) map[string]ClinicalHistoryGroup {
	groups := map[string]ClinicalHistoryGroup{}
	for _, field := range wizard.ClinicalHistoryGroups() {
		status, _ := values[field.Status].(string)
		if status != "PRESENT" && status != "NONE" {
			continue
		}
		items := []string{}
		details, _ := values[field.Details].(string)
		for _, line := range strings.Split(strings.ReplaceAll(details, "\r\n", "\n"), "\n") {
			if text := strings.TrimSpace(line); text != "" {
				items = append(items, text)
			}
		}
		certainty, _ := values["sys_historySource"].(string)
		if certainty != "CONFIRMED" {
			certainty = "REPORTED"
		}
		groups[field.Kind] = ClinicalHistoryGroup{Status: status, Items: items, Certainty: certainty, Date: dates[field.Kind], Source: source, SourceID: id}
	}
	return groups
}

func mergeRecordHistory(current, next map[string]ClinicalHistoryGroup) {
	for key, item := range next {
		old, exists := current[key]
		// Unknown dates cannot displace a dated observation. For equal timestamps,
		// the caller orders encounters by descending ID and keeps the first record.
		if !exists || (item.Date != "" && (old.Date == "" || historyInstant(item.Date).After(historyInstant(old.Date)))) {
			current[key] = item
		}
	}
}
func historyInstant(value string) time.Time {
	instant, _ := time.Parse(time.RFC3339Nano, value)
	return instant
}

func patientHistoryDates(previous, next map[string]any, dates map[string]string, stamp string) map[string]string {
	if dates == nil {
		dates = map[string]string{}
	}
	for _, field := range wizard.ClinicalHistoryGroups() {
		status, _ := next[field.Status].(string)
		if status != "PRESENT" && status != "NONE" {
			continue
		}
		if previous[field.Status] != next[field.Status] || previous[field.Details] != next[field.Details] || previous["sys_historySource"] != next["sys_historySource"] {
			dates[field.Kind] = stamp
		}
	}
	return dates
}

func decodeHistoryDates(raw json.RawMessage) (map[string]string, error) {
	var personal struct {
		Dates map[string]string `json:"clinicalHistoryDates"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &personal); err != nil {
			return nil, err
		}
	}
	return personal.Dates, nil
}
