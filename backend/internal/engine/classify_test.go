package engine

import "testing"

// --- Ramas del árbol (02 §5), una por test ---

func TestClassifyDataQualityWhenStableButElectricallyInconsistent(t *testing.T) {
	signals := []Signal{{Signal: ElectricalInconsistency, Observed: 40, Threshold: 15}}
	issues := []DataQualityIssue{{Kind: "OUT_OF_RANGE", Count: 20}}
	typ, sev, _, _, path := Classify(signals, issues, nil, nil, 2.0)
	if typ != TypeDataQuality {
		t.Fatalf("expected DATA_QUALITY, got %v (path=%v)", typ, path)
	}
	if sev != High {
		t.Fatalf("expected HIGH severity for strong+persistent inconsistency, got %v", sev)
	}
}

func TestClassifyExplainableAnomalyOnLoadIncreaseWithEvent(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 45, Threshold: 15}}
	events := []RelatedEvent{{Event: Event{Type: "OPERATIONAL_CHANGE"}, Relation: Explains}}
	typ, sev, _, _, _ := Classify(signals, nil, events, nil, 45.0)
	if typ != ExplainableAnomaly || sev != Medium {
		t.Fatalf("expected EXPLAINABLE_ANOMALY/MEDIUM, got %v/%v", typ, sev)
	}
}

func TestClassifyFalsePositiveOnScheduledOutage(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: -60, Threshold: 15}}
	events := []RelatedEvent{{Event: Event{Type: "SCHEDULED_OUTAGE"}, Relation: Explains}}
	typ, sev, _, _, _ := Classify(signals, nil, events, nil, -60.0)
	if typ != FalsePositive || sev != Low {
		t.Fatalf("expected FALSE_POSITIVE/LOW, got %v/%v", typ, sev)
	}
}

func TestClassifyRealAnomalyOnUnexplainedShiftWithElectricalChange(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 103.7, Threshold: 15}}
	variables := []VariableDelta{{Variable: "current_a", Changed: true}}
	typ, sev, confidence, _, _ := Classify(signals, nil, nil, variables, 103.7)
	if typ != RealAnomaly || sev != High {
		t.Fatalf("expected REAL_ANOMALY/HIGH, got %v/%v", typ, sev)
	}
	if confidence < 0.9 {
		t.Fatalf("expected confidence >= 0.9 for a strong unexplained shift, got %v", confidence)
	}
}

func TestClassifyRealAnomalyMediumWithoutElectricalChanges(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 60, Threshold: 15}}
	variables := []VariableDelta{
		{Variable: "voltage_v", Changed: false},
		{Variable: "current_a", Changed: false},
	}
	typ, sev, _, _, path := Classify(signals, nil, nil, variables, 60.0)
	if typ != RealAnomaly || sev != Medium {
		t.Fatalf("expected REAL_ANOMALY/MEDIUM, got %v/%v (path=%v)", typ, sev, path)
	}
}

func TestClassifyNoSignalsMeansNoAnomaly(t *testing.T) {
	typ, _, _, _, _ := Classify(nil, nil, nil, nil, 2.0)
	if typ != "" {
		t.Fatalf("expected empty type when there are no signals, got %v", typ)
	}
}

// --- Detalles de rama 1 (DATA_QUALITY) ---

// Una anomalía real de consumo no debe quedar enmascarada como DATA_QUALITY solo
// porque el medidor además tenga lecturas fuera de rango: 02 §5.1 exige "consumo
// estable" para entrar en esa rama.
func TestClassifyRealAnomalyNotMaskedByDataQualityIssues(t *testing.T) {
	signals := []Signal{
		{Signal: PersistentShift, Observed: 103.7, Threshold: 15},
		{Signal: DataQuality, Observed: 5, Threshold: 2},
	}
	issues := []DataQualityIssue{{Kind: "OUT_OF_RANGE", Count: 3}}
	variables := []VariableDelta{{Variable: "current_a", Changed: true}}
	typ, sev, _, _, path := Classify(signals, issues, nil, variables, 103.7)
	if typ != RealAnomaly || sev != High {
		t.Fatalf("expected REAL_ANOMALY/HIGH despite data issues, got %v/%v (path=%v)", typ, sev, path)
	}
}

// Inconsistencia eléctrica presente pero débil (no "fuerte") -> Medium, no High.
func TestClassifyDataQualityMediumWhenInconsistencyIsWeak(t *testing.T) {
	signals := []Signal{{Signal: ElectricalInconsistency, Observed: 18, Threshold: 15}}
	typ, sev, _, _, _ := Classify(signals, nil, nil, nil, 1.0)
	if typ != TypeDataQuality || sev != Medium {
		t.Fatalf("expected DATA_QUALITY/MEDIUM for a weak inconsistency, got %v/%v", typ, sev)
	}
}

// Solo problemas de calidad de datos (sin señales) sigue siendo DATA_QUALITY.
func TestClassifyDataQualityFromIssuesAloneWithoutSignals(t *testing.T) {
	issues := []DataQualityIssue{{Kind: "OUT_OF_RANGE", Count: 12}}
	typ, sev, _, _, _ := Classify(nil, issues, nil, nil, 1.0)
	if typ != TypeDataQuality || sev != Medium {
		t.Fatalf("expected DATA_QUALITY/MEDIUM, got %v/%v", typ, sev)
	}
}

// --- Confianza (02 §7) ---

func TestComputeConfidenceBreakdownForStrongUnexplainedShift(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 103.7, Threshold: 15}}
	variables := []VariableDelta{{Variable: "current_a", Changed: true}}
	total, b := computeConfidence(signals, nil, nil, variables, 103.7)
	if b.SignalStrength != 0.4 {
		t.Fatalf("SignalStrength: want 0.4, got %v", b.SignalStrength)
	}
	if b.ConcordantSignals != 0.2 {
		t.Fatalf("ConcordantSignals: want 0.2 (1 señal + 1 variable coherente de 3), got %v", b.ConcordantSignals)
	}
	if b.EventPresence != 0.2 {
		t.Fatalf("EventPresence: want 0.2 (ausencia total de eventos = cuadro limpio), got %v", b.EventPresence)
	}
	if b.DataQuality != 0.1 {
		t.Fatalf("DataQuality: want 0.1, got %v", b.DataQuality)
	}
	if total != 0.9 {
		t.Fatalf("total confidence: want 0.9, got %v", total)
	}
}

// Eventos solo coincidentes en el tiempo (Related, sin explicar) hacen el cuadro
// ambiguo: el término de evento no suma.
func TestComputeConfidenceDropsEventTermForMerelyRelatedEvents(t *testing.T) {
	signals := []Signal{{Signal: PersistentShift, Observed: 30, Threshold: 15}}
	events := []RelatedEvent{{Event: Event{Type: "UNKNOWN"}, Relation: Related}}
	_, b := computeConfidence(signals, events, nil, nil, 30.0)
	if b.EventPresence != 0 {
		t.Fatalf("EventPresence: want 0 for merely-related events, got %v", b.EventPresence)
	}
}

func TestComputeConfidenceIsCappedAtOne(t *testing.T) {
	signals := []Signal{
		{Signal: PersistentShift}, {Signal: Outlier}, {Signal: HourlyPattern},
		{Signal: ElectricalInconsistency},
	}
	events := []RelatedEvent{{Event: Event{Type: "OPERATIONAL_CHANGE"}, Relation: Explains}}
	variables := []VariableDelta{{Changed: true}, {Changed: true}, {Changed: true}}
	total, _ := computeConfidence(signals, events, nil, variables, 5000)
	if total != 1.0 {
		t.Fatalf("confidence must saturate at 1.0, got %v", total)
	}
}

// --- Prioridad (02 §6) ---

func TestPriorityScoreOrdersRealAboveFalsePositive(t *testing.T) {
	real := PriorityScore(RealAnomaly, High, 0.9, 103.7)
	fp := PriorityScore(FalsePositive, Low, 0.5, 60.0)
	if fp >= real {
		t.Fatalf("FALSE_POSITIVE (%v) must never outrank a REAL_ANOMALY (%v)", fp, real)
	}
}

// 02 §6 exige el invariante para CUALQUIER combinación, no solo para una:
// el peor REAL_ANOMALY debe seguir por encima del mejor FALSE_POSITIVE.
func TestPriorityScoreFalsePositiveNeverOutranksAnyRealAnomaly(t *testing.T) {
	confidences := []float64{0, 0.16, 0.3, 0.5, 0.64, 0.9, 1}
	variations := []float64{0, 0.5, 15, 45, 60, 103.7, 250, -60, -400}
	severities := []Severity{High, Medium, Low}

	worstReal := 1e18
	bestFP := -1e18
	for _, c := range confidences {
		for _, v := range variations {
			for _, s := range severities {
				if r := PriorityScore(RealAnomaly, s, c, v); r < worstReal {
					worstReal = r
				}
				if f := PriorityScore(FalsePositive, s, c, v); f > bestFP {
					bestFP = f
				}
			}
		}
	}
	if bestFP >= worstReal {
		t.Fatalf("best FALSE_POSITIVE (%v) must stay below worst REAL_ANOMALY (%v)", bestFP, worstReal)
	}
}

// Dentro de un mismo tipo, el orden sigue la fórmula de 02 §6.
func TestPriorityScoreOrdersWithinTypeByImpact(t *testing.T) {
	big := PriorityScore(RealAnomaly, High, 0.9, 103.7)
	small := PriorityScore(RealAnomaly, Medium, 0.6, 20)
	if small >= big {
		t.Fatalf("higher severity/confidence/magnitude must score higher: %v vs %v", small, big)
	}
	lowConf := PriorityScore(RealAnomaly, High, 0.3, 103.7)
	if lowConf >= big {
		t.Fatalf("lower confidence must score lower: %v vs %v", lowConf, big)
	}
}

// La magnitud satura en 2 (min(|variation|/100, 2)).
func TestPriorityScoreMagnitudeSaturates(t *testing.T) {
	at200 := PriorityScore(RealAnomaly, High, 1, 200)
	at900 := PriorityScore(RealAnomaly, High, 1, 900)
	if at200 != at900 {
		t.Fatalf("magnitude must saturate at |variation| = 200%%: %v vs %v", at200, at900)
	}
}

func TestPriorityScoreExplainableStaysBelowReal(t *testing.T) {
	bestExplainable := PriorityScore(ExplainableAnomaly, High, 1, 900)
	worstReal := PriorityScore(RealAnomaly, Low, 0, 0)
	if bestExplainable >= worstReal {
		t.Fatalf("EXPLAINABLE (%v) must stay below any REAL_ANOMALY (%v)", bestExplainable, worstReal)
	}
}
