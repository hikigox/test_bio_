package engine

import (
	"testing"
	"time"
)

func TestMatchEventsWithinWindowExplains(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-104", Timestamp: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
			Type: "OPERATIONAL_CHANGE", Description: "New production line activated"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, 45.0) // variación positiva -> aumento
	if len(related) != 1 || related[0].Relation != Explains {
		t.Fatalf("expected event to explain the increase, got %+v", related)
	}
	if related[0].OffsetHours != -6 {
		t.Fatalf("expected offset of -6h (event before the change point), got %v", related[0].OffsetHours)
	}
	if related[0].Event.MeterID != "M-104" {
		t.Fatalf("expected the original event to be carried through, got %+v", related[0].Event)
	}
}

func TestMatchEventsOutsideWindowIsIgnored(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-X", Timestamp: changeAt.Add(-30 * time.Hour), // fuera de ±24h
			Type: "OPERATIONAL_CHANGE", Description: "irrelevant, too far"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, 45.0)
	if len(related) != 0 {
		t.Fatalf("expected no related events beyond 24h window, got %+v", related)
	}
}

func TestMatchEventsScheduledOutageWithDropIsFalsePositiveCandidate(t *testing.T) {
	changeAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-106", Timestamp: changeAt,
			Type: "SCHEDULED_OUTAGE", Description: "Scheduled maintenance outage for 12 hours"},
	}
	cp := ChangePoint{Found: true, At: changeAt}
	related := MatchEvents(events, cp, -60.0) // caída
	if len(related) != 1 || related[0].Relation != Explains {
		t.Fatalf("expected scheduled outage to explain the drop, got %+v", related)
	}
}

func TestMatchEventsWindowBoundsAreInclusive(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	cp := ChangePoint{Found: true, At: changeAt}
	cases := []struct {
		name     string
		offset   time.Duration
		included bool
	}{
		{"exactly -24h", -24 * time.Hour, true},
		{"exactly +24h", 24 * time.Hour, true},
		{"one second past -24h", -24*time.Hour - time.Second, false},
		{"one second past +24h", 24*time.Hour + time.Second, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := []Event{{MeterID: "M-1", Timestamp: changeAt.Add(tc.offset), Type: "OPERATIONAL_CHANGE"}}
			got := MatchEvents(events, cp, 45.0)
			if tc.included && len(got) != 1 {
				t.Fatalf("expected event at %v to be inside the window, got %+v", tc.offset, got)
			}
			if !tc.included && len(got) != 0 {
				t.Fatalf("expected event at %v to be outside the window, got %+v", tc.offset, got)
			}
		})
	}
}

func TestMatchEventsRelationByTypeAndDirection(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	cp := ChangePoint{Found: true, At: changeAt}
	cases := []struct {
		eventType    string
		variationPct float64
		want         EventRelation
	}{
		{"OPERATIONAL_CHANGE", 45.0, Explains},
		{"OPERATIONAL_CHANGE", -45.0, Related},
		{"OPERATIONAL_CHANGE", 0, Related},
		{"SCHEDULED_OUTAGE", -60.0, Explains},
		{"SCHEDULED_OUTAGE", 60.0, Related},
		{"SCHEDULED_OUTAGE", 0, Related},
		{"DATA_QUALITY", 45.0, Related},
		{"DATA_QUALITY", -45.0, Related},
		{"UNKNOWN", 45.0, Related},
		{"UNKNOWN", -45.0, Related},
	}
	for _, tc := range cases {
		events := []Event{{MeterID: "M-1", Timestamp: changeAt, Type: tc.eventType}}
		got := MatchEvents(events, cp, tc.variationPct)
		if len(got) != 1 {
			t.Fatalf("%s @ %v: expected exactly 1 matched event, got %+v", tc.eventType, tc.variationPct, got)
		}
		if got[0].Relation != tc.want {
			t.Fatalf("%s @ %v: expected relation %s, got %s", tc.eventType, tc.variationPct, tc.want, got[0].Relation)
		}
	}
}

func TestMatchEventsWithoutChangePointReturnsNothing(t *testing.T) {
	events := []Event{
		{MeterID: "M-1", Timestamp: time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC), Type: "OPERATIONAL_CHANGE"},
	}
	if got := MatchEvents(events, ChangePoint{Found: false}, 45.0); len(got) != 0 {
		t.Fatalf("expected no matches without a change point, got %+v", got)
	}
}

func TestMatchEventsKeepsOnlyEventsInsideWindow(t *testing.T) {
	changeAt := time.Date(2026, 9, 11, 6, 0, 0, 0, time.UTC)
	events := []Event{
		{MeterID: "M-1", Timestamp: changeAt.Add(-72 * time.Hour), Type: "OPERATIONAL_CHANGE", Description: "too early"},
		{MeterID: "M-1", Timestamp: changeAt.Add(-2 * time.Hour), Type: "SCHEDULED_OUTAGE", Description: "in window"},
		{MeterID: "M-1", Timestamp: changeAt.Add(12 * time.Hour), Type: "OPERATIONAL_CHANGE", Description: "in window"},
		{MeterID: "M-1", Timestamp: changeAt.Add(96 * time.Hour), Type: "SCHEDULED_OUTAGE", Description: "too late"},
	}
	got := MatchEvents(events, ChangePoint{Found: true, At: changeAt}, 45.0)
	if len(got) != 2 {
		t.Fatalf("expected the 2 in-window events, got %+v", got)
	}
	if got[0].Relation != Related || got[0].OffsetHours != -2 {
		t.Fatalf("expected outage+increase to be RELATED at -2h, got %+v", got[0])
	}
	if got[1].Relation != Explains || got[1].OffsetHours != 12 {
		t.Fatalf("expected operational change+increase to EXPLAIN at +12h, got %+v", got[1])
	}
}
