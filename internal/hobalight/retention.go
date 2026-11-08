package hobalight

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type Retention struct {
	Days     int `yaml:"days,omitempty"`
	Sessions int `yaml:"sessions,omitempty"`
}

func (r Retention) enabled() bool { return r.Days > 0 || r.Sessions > 0 }
func (r Retention) validate() error {
	if r.Days < 0 || r.Days > 365000 || r.Sessions < 0 {
		return errors.New("retention.days must be between 0 and 365000; retention.sessions must be nonnegative")
	}
	return nil
}

type retentionSession struct {
	id, source string
	started    time.Time
	references []string
}

// Read authoritative roots, including sessions without a derived reference.
// Both rules retain independently; zero disables a rule.
func (s *Store) expiredSessions(order []string, now time.Time) ([]retentionSession, error) {
	if err := s.retention.validate(); err != nil {
		return nil, err
	}
	if !s.retention.enabled() {
		return nil, nil
	}
	groups := map[string][]retentionSession{}
	for _, id := range order {
		if !validID(id, 'S') {
			continue
		}
		var session Session
		if err := s.read(id, 'S', &session); err != nil {
			return nil, err
		}
		source := session.Source
		if source == "" {
			source = session.Directory
		}
		if _, err := historyPath(source); err != nil {
			return nil, fmt.Errorf("retention session %s: %w", id, err)
		}
		started, err := time.Parse(time.RFC3339Nano, session.Started)
		if err != nil {
			return nil, err
		}
		groups[source] = append(groups[source], retentionSession{id: id, source: source, started: started})
	}
	cutoff := now.UTC().AddDate(0, 0, -s.retention.Days)
	var expired []retentionSession
	for _, sessions := range groups {
		sort.Slice(sessions, func(i, j int) bool {
			if sessions[i].started.Equal(sessions[j].started) {
				return sessions[i].id > sessions[j].id
			}
			return sessions[i].started.After(sessions[j].started)
		})
		for index, session := range sessions {
			keepDays := s.retention.Days > 0 && !session.started.Before(cutoff)
			keepCount := s.retention.Sessions > 0 && index < s.retention.Sessions
			if !keepDays && !keepCount {
				expired = append(expired, session)
			}
		}
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i].id < expired[j].id })
	if len(expired) == 0 {
		return expired, nil
	}
	byID := map[string]int{}
	for i, session := range expired {
		byID[session.id] = i
	}
	entries, err := storageEntries(s.backend, "sessions")
	if errors.Is(err, fs.ErrNotExist) {
		return expired, nil
	}
	if err != nil {
		return nil, err
	}
	// Validate reference cleanup before deleting any session. A single scan also
	// handles duplicated references without quadratic work per expired session.
	for _, entry := range entries {
		if !isSessionReference(entry) {
			continue
		}
		name := path.Join("sessions", entry.Name())
		data, err := s.backend.Read(name)
		if err != nil {
			return nil, err
		}
		var ref struct {
			Session string `yaml:"session"`
		}
		if err := yaml.Unmarshal(data, &ref); err != nil {
			return nil, err
		}
		if !validID(ref.Session, 'S') {
			return nil, fmt.Errorf("invalid session reference %s", name)
		}
		if index, ok := byID[ref.Session]; ok {
			expired[index].references = append(expired[index].references, name)
		}
	}
	return expired, nil
}

// YAML's default integer decoder accepts fractional numeric values by coercion.
// Reject those and misspelled fields before a destructive policy can be applied.
func (r *Retention) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		*r = Retention{}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("retention must be a mapping of days and sessions")
	}
	var decoded Retention
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if key != "days" && key != "sessions" {
			return fmt.Errorf("unknown retention option %q", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate retention option %q", key)
		}
		seen[key] = true
		if value.Tag != "!!int" {
			return fmt.Errorf("retention.%s must be a nonnegative integer", key)
		}
		var count int
		if err := value.Decode(&count); err != nil {
			return err
		}
		if key == "days" {
			decoded.Days = count
		} else {
			decoded.Sessions = count
		}
	}
	if err := decoded.validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}
