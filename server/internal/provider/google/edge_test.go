package google

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"calendar/server/internal/domain"
)

// TestEdgeCaseGoldens maps raw Google events (testdata/edge/*.json) and compares the fields each
// file lists under "want". Add a file to add a case.
func TestEdgeCaseGoldens(t *testing.T) {
	files, _ := filepath.Glob("testdata/edge/*.json")
	if len(files) < 20 {
		t.Fatalf("only %d golden files", len(files))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var c struct {
				Calendar struct{ Access, TZID string }
				Event    gEvent
				Want     map[string]any
			}
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			if c.Calendar.TZID == "" {
				c.Calendar.TZID = "Europe/London"
			}
			e := mapEvent(c.Event, domain.Calendar{ID: "c", Access: c.Calendar.Access, TZID: c.Calendar.TZID})
			got := map[string]any{
				"title": e.Title, "kind": string(e.Kind), "status": string(e.Status), "busy": e.Busy, "allDay": e.When.AllDay,
				"canEdit": e.Caps.CanEdit, "canRSVP": e.Caps.CanRSVP, "canAddConference": e.Caps.CanAddConference,
				"remoteSeriesId": e.RemoteSeriesID, "tzid": e.When.TZID,
			}
			if e.When.AllDay {
				got["startDate"], got["endDate"] = e.When.StartDate.String(), e.When.EndDate.String()
			} else if !e.When.Start.IsZero() {
				got["start"], got["end"] = e.When.Start.Format(time.RFC3339), e.When.End.Format(time.RFC3339)
			}
			if e.RecurrenceKey != nil {
				got["recurrenceKey"] = float64(*e.RecurrenceKey)
			}
			for k, want := range c.Want {
				if g, ok := got[k]; !ok || g != want {
					t.Errorf("%s: got %v (%T) want %v", k, got[k], got[k], want)
				}
			}
		})
	}
}
