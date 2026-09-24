package engine

import "testing"

func TestBuildReasonRealAnomalyIncludesNumbers(t *testing.T) {
	reason := BuildReason(RealAnomaly, 103.7, []VariableDelta{
		{Variable: "current_a", DeltaPct: 210, Changed: true},
	})
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
	if !containsAll(reason, "103.7", "current_a") {
		t.Fatalf("reason must cite the actual numbers, got %q", reason)
	}
}

func TestBuildReasonNoChangedVariablesUsesFallbackPhrase(t *testing.T) {
	reason := BuildReason(RealAnomaly, 50.0, nil)
	if !contains(reason, "sin cambios eléctricos coherentes") {
		t.Fatalf("expected fallback phrase when no variables changed, got %q", reason)
	}
}

func TestBuildReasonUnknownTypeReturnsEmpty(t *testing.T) {
	if got := BuildReason(AnomalyType(""), 10, nil); got != "" {
		t.Fatalf("expected empty reason for zero-value type, got %q", got)
	}
}

func TestBuildRecommendationByType(t *testing.T) {
	cases := map[AnomalyType]string{
		RealAnomaly:        "Investigar medidor e instalación",
		TypeDataQuality:    "Validar sensor/cableado",
		ExplainableAnomaly: "Validar operación",
		FalsePositive:      "No escalar",
	}
	for typ, want := range cases {
		got := BuildRecommendation(typ)
		if got != want {
			t.Errorf("BuildRecommendation(%v) = %q, want %q", typ, got, want)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
