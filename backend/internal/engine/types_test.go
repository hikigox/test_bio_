package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfidenceBreakdownJSONTags(t *testing.T) {
	breakdown := ConfidenceBreakdown{
		SignalStrength:    0.4,
		ConcordantSignals: 0.3,
		EventPresence:     0.2,
		DataQuality:       0.1,
	}

	data, err := json.Marshal(breakdown)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)

	// Check that lowercase keys are present
	requiredKeys := []string{"signal_strength", "concordant_signals", "event_presence", "data_quality"}
	for _, key := range requiredKeys {
		if !strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("expected lowercase key %q in JSON, got: %s", key, jsonStr)
		}
	}

	// Check that capitalized Go field names are NOT present
	capitalizedKeys := []string{"SignalStrength", "ConcordantSignals", "EventPresence", "DataQuality"}
	for _, key := range capitalizedKeys {
		if strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("unexpected capitalized key %q in JSON, got: %s", key, jsonStr)
		}
	}

	// Verify values are correct
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if v, ok := decoded["signal_strength"].(float64); !ok || v != 0.4 {
		t.Errorf("signal_strength: expected 0.4, got %v", decoded["signal_strength"])
	}
	if v, ok := decoded["concordant_signals"].(float64); !ok || v != 0.3 {
		t.Errorf("concordant_signals: expected 0.3, got %v", decoded["concordant_signals"])
	}
	if v, ok := decoded["event_presence"].(float64); !ok || v != 0.2 {
		t.Errorf("event_presence: expected 0.2, got %v", decoded["event_presence"])
	}
	if v, ok := decoded["data_quality"].(float64); !ok || v != 0.1 {
		t.Errorf("data_quality: expected 0.1, got %v", decoded["data_quality"])
	}
}

func TestDataQualityIssueJSONTags(t *testing.T) {
	issue := DataQualityIssue{
		Kind:  "NULLS",
		Count: 5,
	}

	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)

	// Check that lowercase keys are present
	requiredKeys := []string{"kind", "count"}
	for _, key := range requiredKeys {
		if !strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("expected lowercase key %q in JSON, got: %s", key, jsonStr)
		}
	}

	// Check that capitalized Go field names are NOT present
	capitalizedKeys := []string{"Kind", "Count"}
	for _, key := range capitalizedKeys {
		if strings.Contains(jsonStr, `"`+key+`"`) {
			t.Errorf("unexpected capitalized key %q in JSON, got: %s", key, jsonStr)
		}
	}

	// Verify values are correct
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if v, ok := decoded["kind"].(string); !ok || v != "NULLS" {
		t.Errorf("kind: expected 'NULLS', got %v", decoded["kind"])
	}
	if v, ok := decoded["count"].(float64); !ok || v != 5 {
		t.Errorf("count: expected 5, got %v", decoded["count"])
	}
}
