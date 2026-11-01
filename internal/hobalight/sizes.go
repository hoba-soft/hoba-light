package hobalight

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// displaySize reports logical file bytes, independent of compression or deduplication.
func displaySize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return "0 B"
}

func filesSize(files []sessionFile, current string) int64 {
	var total int64
	for _, f := range files {
		if current == "" || f.relative == current || len(f.relative) > len(current) && f.relative[:len(current)+1] == current+"/" {
			total += f.metadata.Size
		}
	}
	return total
}

// Keep damaged snapshots visible so browsing healthy sessions still works.
// Metadata shared between snapshots is read only once per listing/refresh.
func (s *Store) referenceStats(refs []Reference) map[string]tableStats {
	stats := make(map[string]tableStats)

	metadata := make(map[string]File)
	for _, ref := range refs {
		if _, ok := stats[ref.Session]; ok {
			continue
		}
		stats[ref.Session] = tableStats{files: "?", errors: "?", size: "?"}
		if validReferenceListing(ref) {
			var total int64
			errors := 0
			for _, file := range *ref.Files {
				total += file.Size
				if file.Failed {
					errors++
				}
			}
			stats[ref.Session] = numericStats(len(*ref.Files), errors, total)
			continue
		}
		var session Session
		if err := s.read(ref.Session, 'S', &session); err != nil {
			continue
		}
		stats[ref.Session] = tableStats{files: fmt.Sprint(len(session.Files)), errors: "?", size: "?"}
		var total int64
		valid := true
		errors := 0
		for _, id := range session.Files {
			file, ok := metadata[id]
			if !ok {
				if err := s.read(id, 'F', &file); err != nil || file.Size < 0 {
					valid = false
					break
				}
				metadata[id] = file
			}
			total += file.Size
			errors += fileErrors(file)
		}
		if valid {
			stats[ref.Session] = numericStats(len(session.Files), errors, total)
		}
	}
	return stats
}

// sizeColumn preserves the right edge, truncating only the name when necessary.
func sizeColumn(name, size string, width int) string {
	name = browserText(name)
	suffix := size
	if width <= runewidth.StringWidth(suffix) {
		return browserClip(suffix, width)
	}
	available := max(0, width-runewidth.StringWidth(suffix)-2)
	name = browserClip(name, available)
	return name + strings.Repeat(" ", max(0, width-runewidth.StringWidth(name)-runewidth.StringWidth(suffix))) + suffix
}

func sizeColumnWidth(names, sizes []string) int {
	nameWidth, sizeWidth := 0, 0
	for _, name := range names {
		nameWidth = max(nameWidth, runewidth.StringWidth(browserText(name)))
	}
	for _, size := range sizes {
		sizeWidth = max(sizeWidth, runewidth.StringWidth(size))
	}
	return nameWidth + 2 + sizeWidth
}

// hashSizeColumn shares column widths across every row, including directories.
func hashSizeColumn(name, hash, size string, width, sizeWidth int) string {
	suffix := size
	sizeWidth = max(sizeWidth, runewidth.StringWidth(suffix))
	right := fmt.Sprintf("%8s  %s%s", hash, strings.Repeat(" ", sizeWidth-runewidth.StringWidth(suffix)), suffix)
	available := width - runewidth.StringWidth(right) - 2
	if available < 1 {
		return sizeColumn(name, size, width)
	}
	name = browserClip(name, available)
	return name + strings.Repeat(" ", available-runewidth.StringWidth(name)+2) + right
}

func entryColumns(entry browserEntry) (string, string) {
	if entry.directory {
		return entry.name + "/", ""
	}
	return markedFileName(entry.name, entry.file.metadata), entry.file.id[:8]
}

func sizeWidth(sizes []string) int {
	width := 0
	for _, size := range sizes {
		width = max(width, runewidth.StringWidth(size))
	}
	return width
}

func displayCount(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
}

func filesCount(files []sessionFile, current string) int {
	count := 0
	for _, f := range files {
		if current == "" || f.relative == current || strings.HasPrefix(f.relative, current+"/") {
			count++
		}
	}
	return count
}

func fileErrors(f File) int {
	if f.Error != "" {
		return 1
	}
	return 0
}

func markedFileName(name string, f File) string {
	if f.Error != "" {
		return "[error] " + name
	}
	return name
}

func displayFileCounts(count, errors int) string {
	text := displayCount(count)
	if errors > 0 {
		text += fmt.Sprintf(", %d errors", errors)
	}
	return text
}

func filesErrors(files []sessionFile, current string) int {
	count := 0
	for _, f := range files {
		if current == "" || f.relative == current || strings.HasPrefix(f.relative, current+"/") {
			count += fileErrors(f.metadata)
		}
	}
	return count
}
