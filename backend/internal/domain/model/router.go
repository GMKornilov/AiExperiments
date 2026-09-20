package model

import "strings"

// TaskRouter is a replaceable conservative local matching policy.
type TaskRouter struct{}

func (TaskRouter) Match(tasks []Task, input string) []Task {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(input)), "новая задача") {
		return nil
	}
	paused := []Task{}
	active := []Task{}
	matches := []Task{}
	for _, t := range tasks {
		switch t.Status {
		case TaskStatusPaused:
			paused = append(paused, t)
		case TaskStatusActive:
			active = append(active, t)
		}
	}
	if len(paused) > 0 {
		return paused
	}
	words := strings.Fields(strings.ToLower(input))
	for _, t := range active {
		for _, word := range words {
			word = strings.Trim(word, `.,!?;:"'()[]`)
			if len([]rune(word)) > 2 && strings.Contains(strings.ToLower(t.Title+" "+t.Description), word) {
				matches = append(matches, t)
				break
			}
		}
	}
	if len(matches) == 0 && len(active) == 1 && strings.HasPrefix(active[0].ExpectedAction, "user") && !strings.HasPrefix(strings.ToLower(input), "новая задача") {
		return active
	}
	return matches
}
