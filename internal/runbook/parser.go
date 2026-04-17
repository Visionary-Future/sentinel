package runbook

import (
	"strings"
)

// Parse extracts structured fields from a Markdown runbook.
// The expected format has top-level H1 title and H2 sections:
//
//	# Runbook Title
//	## Trigger
//	- alert.title contains "P99 latency"
//	## Steps
//	- 查询 $service 过去 30 分钟 ...
//	## Escalation
//	- team: platform-team
func Parse(content string) *Runbook {
	rb := &Runbook{Content: content}

	sections := splitSections(content)

	// Title: first H1 line
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			rb.Name = strings.TrimPrefix(line, "# ")
			break
		}
	}

	if desc, ok := sections["description"]; ok {
		rb.Description = strings.TrimSpace(desc)
	}

	if raw, ok := sections["trigger"]; ok {
		rb.Triggers = parseTriggers(raw)
	}

	if raw, ok := sections["steps"]; ok {
		rb.Steps = parseListItems(raw)
	}

	if raw, ok := sections["escalation"]; ok {
		rb.Escalation = parseEscalation(raw)
	}

	return rb
}

// splitSections returns the body text of each ## section, keyed by lowercased name.
func splitSections(content string) map[string]string {
	sections := make(map[string]string)
	var currentSection string
	var buf strings.Builder

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			if currentSection != "" {
				sections[currentSection] = buf.String()
				buf.Reset()
			}
			currentSection = strings.ToLower(strings.TrimPrefix(line, "## "))
			continue
		}
		if currentSection != "" {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	if currentSection != "" {
		sections[currentSection] = buf.String()
	}

	return sections
}

// parseListItems extracts bullet-point items from a section body.
func parseListItems(raw string) []string {
	var items []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimPrefix(line, "- ")
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// parseTriggers converts bullet items into Trigger structs.
// Supported formats:
//
//	- alert.title contains "some text"
//	- alert.severity in [critical, warning]
//	- alert.service matches "order-*"
//	- alert.severity equals "critical"
func parseTriggers(raw string) []Trigger {
	var triggers []Trigger
	for _, item := range parseListItems(raw) {
		t := parseTriggerLine(item)
		if t != nil {
			triggers = append(triggers, *t)
		}
	}
	return triggers
}

func parseTriggerLine(line string) *Trigger {
	operators := []string{"contains", "matches", "equals", "in"}
	for _, op := range operators {
		idx := strings.Index(line, " "+op+" ")
		if idx < 0 {
			continue
		}
		field := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+len(op)+2:])
		// Strip surrounding quotes or brackets
		value = strings.Trim(value, `"'[]`)
		return &Trigger{Field: field, Operator: op, Value: value}
	}
	return nil
}

// parseEscalation parses key: value lines from the escalation section.
func parseEscalation(raw string) Escalation {
	var e Escalation
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "team":
			e.Team = val
		case "channel":
			e.Channel = val
		case "timeout":
			e.Timeout = val
		}
	}
	return e
}
