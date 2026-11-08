package hobalight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"gopkg.in/yaml.v3"
)

type sessionFile struct {
	relative string
	id       string
	metadata File
}

// Hidden files include macOS AppleDouble sidecars, not session references.
func isSessionReference(entry fs.DirEntry) bool {
	return entry.Type().IsRegular() && !strings.HasPrefix(entry.Name(), ".") && strings.HasSuffix(entry.Name(), ".yaml")
}

func sessionReferenceName(started time.Time, id string) string {
	return started.UTC().Format("2006-01-02_15-04-05Z") + "-" + id[:8] + ".yaml"
}

func (s *Store) sessionReferences() ([]Reference, error) {
	entries, err := storageEntries(s.backend, "sessions")
	if err != nil {
		return nil, err
	}
	listings := make(map[string]Reference)
	var refs []Reference
	for _, entry := range entries {
		if !isSessionReference(entry) {
			continue
		}
		data, err := s.backend.Read(path.Join("sessions", entry.Name()))
		if err != nil {
			return nil, err
		}
		var ref Reference
		if err = yaml.Unmarshal(data, &ref); err != nil {
			return nil, fmt.Errorf("session reference %s: %w", entry.Name(), err)
		}
		if !validID(ref.Session, 'S') {
			return nil, fmt.Errorf("session reference %s: invalid session hash", entry.Name())
		}
		if ref.Files != nil {
			listings[ref.Session] = ref
		}
		refs = append(refs, ref)
	}
	// RFC3339Nano omits trailing zeros, so timestamps do not sort as strings.
	started := func(r Reference) time.Time { t, _ := time.Parse(time.RFC3339Nano, r.Started); return t }
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := started(refs[i]), started(refs[j])
		if a.Equal(b) {
			return refs[i].Session < refs[j].Session
		}
		return a.After(b)
	})
	s.sessionListings = listings
	return refs, nil
}

// groupedReferences keeps newest-first ordering within alphabetically sorted sources.
func groupedReferences(refs []Reference) []Reference {
	refs = append([]Reference(nil), refs...)
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Directory < refs[j].Directory })
	return refs
}

func sessionTime(ref Reference) string {
	started, err := time.Parse(time.RFC3339Nano, ref.Started)
	if err != nil {
		return ref.Started
	}
	return started.In(time.Local).Format("2006-01-02 15:04:05 -07:00")
}

func sessionLabel(ref Reference) string {
	return sessionTime(ref) + "  " + ref.Session[:8] + commentSuffix(ref.Comment)
}

func commentSuffix(comment string) string {
	if comment == "" {
		return ""
	}
	return "  — " + browserText(comment)
}

func (s *Store) sessionFiles(id string) (Session, []sessionFile, error) {
	var session Session
	if err := s.read(id, 'S', &session); err != nil {
		return session, nil, err
	}
	files := make([]sessionFile, 0, len(session.Files))
	for _, fileID := range session.Files {
		var f File
		if err := s.read(fileID, 'F', &f); err != nil {
			return session, nil, err
		}
		rel, err := relative(session.Directory, f.Path)
		if err != nil {
			return session, nil, err
		}
		files = append(files, sessionFile{relative: rel, id: fileID, metadata: f})
	}
	return session, files, nil
}
func (s *Store) list(args []string, output io.Writer) error {
	if len(args) > 2 {
		return errors.New("usage: list [session-hash] [relative/path]")
	}
	if len(args) == 0 {
		refs, err := s.sessionReferences()
		if err != nil {
			return err
		}
		refs, err = filterSessionDates(refs, s.sessionDate)
		if err != nil {
			return err
		}
		stats := s.referenceStats(refs)
		var names []string
		var values []tableStats
		for _, ref := range refs {
			names = append(names, sessionTime(ref))
			values = append(values, stats[ref.Session])
		}
		table := newListingTable(names, values)
		fmt.Fprintln(output, "  "+table.header(true, 0))
		directory := ""
		for i, ref := range groupedReferences(refs) {
			if i == 0 || ref.Directory != directory {
				fmt.Fprintln(output)
				fmt.Fprintln(output, browserText(ref.Directory))
			}
			fmt.Fprintln(output, "  "+table.row(sessionTime(ref), ref.Session[:8], stats[ref.Session], 0)+commentSuffix(ref.Comment))
			directory = ref.Directory
		}
		return nil
	}
	id, err := s.resolve(args[0], 'S')
	if err != nil {
		return err
	}
	session, files, err := s.listingFiles(id)
	if err != nil {
		return err
	}
	if matches, err := matchesSessionDate(session.Started, s.sessionDate); err != nil {
		return err
	} else if !matches {
		return errors.New("session is outside the selected date")
	}
	current := ""
	if len(args) == 2 {
		if current, err = userRelative(args[1]); err != nil {
			return err
		}
	}
	directory := session.Directory
	if current != "" {
		directory = strings.TrimRight(directory, "/") + "/" + current
	}
	header := fmt.Sprintf("Session %s  %s  %s  %s", id[:8], directory, displayFileCounts(filesCount(files, current), filesErrors(files, current)), displaySize(filesSize(files, current)))
	fmt.Fprintln(output, header)
	fmt.Fprintln(output, "Status: "+session.completionStatus())
	if session.Comment != "" {
		fmt.Fprintln(output, "Comment: "+browserText(session.Comment))
	}
	for _, message := range session.Errors {
		fmt.Fprintln(output, "Backup error: "+browserText(message))
	}
	fmt.Fprintln(output, strings.Repeat("─", runewidth.StringWidth(header)))
	for _, f := range files {
		if current != "" && f.relative == current {
			st := numericStats(1, fileErrors(f.metadata), f.metadata.Size)
			st.files = "—"
			table := newListingTable([]string{markedFileName(f.relative, f.metadata)}, []tableStats{st})
			fmt.Fprintln(output, table.header(false, 0))
			fmt.Fprintln(output, table.row(markedFileName(f.relative, f.metadata), f.id[:8], st, 0))
			if f.metadata.Error != "" {
				var metadata File
				if err := s.read(f.id, 'F', &metadata); err != nil {
					return err
				}
				fmt.Fprintf(output, "Error: %s\n", browserText(metadata.Error))
			}
			return nil
		}
	}
	entries := browserEntries(files, current)
	if current != "" && len(entries) == 0 {
		return errors.New("directory not found in session")
	}
	var names []string
	var values []tableStats
	for _, entry := range entries {
		name, _ := entryColumns(entry)
		names = append(names, name)
		values = append(values, entryStats(entry))
	}
	table := newListingTable(names, values)
	fmt.Fprintln(output, table.header(false, 0))
	for i, entry := range entries {
		_, hash := entryColumns(entry)
		fmt.Fprintln(output, table.row(names[i], hash, values[i], 0))
	}

	return nil
}

// Listings are display data only. Full operations always read original objects.
func validReferenceListing(ref Reference) bool {
	if ref.Format < 0 || ref.Format > referenceFormat || ref.Files == nil {
		return false
	}
	seen := make(map[string]bool, len(*ref.Files))
	for _, f := range *ref.Files {
		if !validID(f.ID, 'F') || !validRelative(f.Path) || f.Size < 0 || seen[f.Path] {
			return false
		}
		seen[f.Path] = true
	}
	return true
}

func referenceFiles(ref Reference) ([]sessionFile, bool) {
	if !validReferenceListing(ref) {
		return nil, false
	}
	files := make([]sessionFile, 0, len(*ref.Files))
	for _, f := range *ref.Files {
		files = append(files, sessionFile{relative: f.Path, id: f.ID, metadata: File{Identity: Identity{Size: f.Size, Error: referenceError(f.Failed)}}})
	}
	return files, true
}

func (s *Store) listingFiles(id string) (Session, []sessionFile, error) {
	if s.sessionListings == nil {
		_, _ = s.sessionReferences()
	}
	if ref, ok := s.sessionListings[id]; ok {
		var session Session
		if err := s.read(id, 'S', &session); err != nil {
			return session, nil, err
		}
		if files, valid := referenceFiles(ref); valid && len(files) == len(session.Files) && ref.Directory == session.Directory {
			match := true
			for i := range files {
				if files[i].id != session.Files[i] {
					match = false
					break
				}
			}
			if match {
				return session, files, nil
			}
		}
	}
	return s.sessionFiles(id)
}

func referenceError(failed bool) string {
	if failed {
		return "backup failed"
	}
	return ""
}
