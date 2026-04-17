package runbook

import (
	"path/filepath"
	"strings"

	"github.com/sentinelai/sentinel/internal/alert"
)

// Match returns the first enabled runbook whose triggers all match the alert.
// Returns nil if no runbook matches.
func Match(runbooks []*Runbook, evt *alert.Event) *Runbook {
	for _, rb := range runbooks {
		if !rb.Enabled {
			continue
		}
		if matchesAll(rb.Triggers, evt) {
			return rb
		}
	}
	return nil
}

// matchesAll returns true only when every trigger condition is satisfied.
func matchesAll(triggers []Trigger, evt *alert.Event) bool {
	if len(triggers) == 0 {
		return true // no conditions = match everything
	}
	for _, t := range triggers {
		if !matchesTrigger(t, evt) {
			return false
		}
	}
	return true
}

func matchesTrigger(t Trigger, evt *alert.Event) bool {
	fieldVal := resolveField(t.Field, evt)
	switch t.Operator {
	case "contains":
		return strings.Contains(strings.ToLower(fieldVal), strings.ToLower(t.Value))
	case "equals":
		return strings.EqualFold(fieldVal, t.Value)
	case "matches":
		// glob-style matching (e.g. "order-*")
		matched, _ := filepath.Match(t.Value, fieldVal)
		return matched
	case "in":
		// value is a comma-separated list: "critical, warning"
		for _, v := range strings.Split(t.Value, ",") {
			if strings.EqualFold(strings.TrimSpace(v), fieldVal) {
				return true
			}
		}
		return false
	}
	return false
}

// resolveField extracts a string value from the alert for a given field path.
func resolveField(field string, evt *alert.Event) string {
	switch field {
	case "alert.title":
		return evt.Title
	case "alert.description":
		return evt.Description
	case "alert.severity":
		return string(evt.Severity)
	case "alert.service":
		return evt.Service
	case "alert.source":
		return string(evt.Source)
	}
	// Support label lookups: alert.labels.env
	if strings.HasPrefix(field, "alert.labels.") {
		key := strings.TrimPrefix(field, "alert.labels.")
		return evt.Labels[key]
	}
	return ""
}
