package clinical

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func TestSummarySnapshotCachePreservesEveryInterpretation(t *testing.T) {
	cache := map[string]map[string]any{}
	for i := range 12 {
		raw := []byte(fmt.Sprintf(`{"categories":[{"key":"synthetic_%d","enabled":true,"fields":[{"key":"followUp","type":"TEXTAREA"}]}]}`, i))
		for range 2 {
			uncached, err := wizard.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			cached, err := summarySnapshot(raw, cache)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]any{"followUp": "Synthetic oldest history"}
			before, _ := json.Marshal(cached)
			if !reflect.DeepEqual(wizard.ClinicalProjections(cached, values), wizard.ClinicalProjections(uncached, values)) || !reflect.DeepEqual(wizard.VisibleValues(cached, values), wizard.VisibleValues(uncached, values)) {
				t.Fatal("cache changed interpretation")
			}
			after, _ := json.Marshal(cached)
			if string(before) != string(after) {
				t.Fatal("reader mutated cached snapshot")
			}
		}
	}
	if len(cache) != 8 {
		t.Fatalf("cache grew without bound: %d", len(cache))
	}
	if _, err := summarySnapshot([]byte(`{"invalid"`), cache); err == nil {
		t.Fatal("full cache concealed invalid snapshot")
	}
}
