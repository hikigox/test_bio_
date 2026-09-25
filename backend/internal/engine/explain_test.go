package engine

import "testing"

func TestBuildReasonRealAnomalyIncludesNumbers(t *testing.T) {
	reason := BuildReason(RealAnomaly, 103.7, []VariableDelta{
		{Variable: "current_a", DeltaPct: 210, Changed: true},
	})
	if reason == "" {
		t.Fatal("expected non-empty reason")
	}
	if !containsAll(reason, "103.7", "corriente") {
		t.Fatalf("reason must cite the actual numbers, got %q", reason)
	}
}

func TestBuildReasonNoChangedVariablesUsesFallbackPhrase(t *testing.T) {
	reason := BuildReason(RealAnomaly, 50.0, nil)
	if !contains(reason, "sin cambios eléctricos coherentes") {
		t.Fatalf("expected fallback phrase when no variables changed, got %q", reason)
	}
}

// I6: las ramas 3/4 de 02 §5 no restringen el signo, así que una caída
// inexplicada es un REAL_ANOMALY válido. La plantilla decía siempre "por encima
// del baseline", produciendo textos que se contradicen ("-0.1% por encima").
func TestBuildReasonRealAnomalyNegativeVariationSaysBelow(t *testing.T) {
	reason := BuildReason(RealAnomaly, -42.5, nil)
	if !contains(reason, "por debajo") {
		t.Fatalf("a negative variation must read as \"por debajo\", got %q", reason)
	}
	if contains(reason, "por encima") {
		t.Fatalf("a negative variation must not read as \"por encima\", got %q", reason)
	}
	if !contains(reason, "42.5") {
		t.Fatalf("the magnitude must be printed, got %q", reason)
	}
	if contains(reason, "-42.5") {
		t.Fatalf("the sign is carried by the wording, not by the number, got %q", reason)
	}
}

// La misma coherencia de signo en las otras tres plantillas.
func TestBuildReasonAllTemplatesAreSignAware(t *testing.T) {
	for _, typ := range []AnomalyType{RealAnomaly, TypeDataQuality, ExplainableAnomaly, FalsePositive} {
		down := BuildReason(typ, -30, nil)
		if !contains(down, "por debajo") || contains(down, "por encima") {
			t.Errorf("%v: negative variation must read as \"por debajo\", got %q", typ, down)
		}
		if contains(down, "-30") {
			t.Errorf("%v: must print the magnitude, not the signed number, got %q", typ, down)
		}
		up := BuildReason(typ, 30, nil)
		if !contains(up, "por encima") || contains(up, "por debajo") {
			t.Errorf("%v: positive variation must read as \"por encima\", got %q", typ, up)
		}
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
