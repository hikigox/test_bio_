package engine

import "time"

// eventWindow es la ventana ±24h alrededor del punto de cambio en la que se
// buscan eventos del medidor (02 §4).
const eventWindow = 24 * time.Hour

// MatchEvents busca eventos dentro de ±24h del punto de cambio (02 §4).
// variationPct > 0 (aumento) casa con eventos de aumento de carga;
// variationPct < 0 (caída) casa con eventos de parada/mantenimiento.
// Los eventos que caen en la ventana pero no explican la dirección del cambio
// se devuelven igualmente, marcados como Related.
func MatchEvents(events []Event, cp ChangePoint, variationPct float64) []RelatedEvent {
	if !cp.Found {
		return nil
	}
	var out []RelatedEvent
	for _, e := range events {
		offset := e.Timestamp.Sub(cp.At)
		if offset < -eventWindow || offset > eventWindow {
			continue
		}
		relation := Related
		if explainsChange(e.Type, variationPct) {
			relation = Explains
		}
		out = append(out, RelatedEvent{Event: e, Relation: relation, OffsetHours: offset.Hours()})
	}
	return out
}

// explainsChange decide si el tipo de evento explica semánticamente la
// dirección del cambio de consumo (02 §4).
func explainsChange(eventType string, variationPct float64) bool {
	switch eventType {
	case "OPERATIONAL_CHANGE":
		return variationPct > 0
	case "SCHEDULED_OUTAGE":
		return variationPct < 0
	default:
		return false // DATA_QUALITY, UNKNOWN u otros no "explican" el cambio
	}
}
