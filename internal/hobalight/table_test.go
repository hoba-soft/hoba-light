package hobalight

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestListingTableAlignedColumns(t *testing.T) {
	stats := []tableStats{numericStats(123, 2, 402329), numericStats(5, 0, 4096)}
	table := newListingTable([]string{"2026-10-31 23:41:51 UTC"}, stats)
	for _, width := range []int{60, 100} {
		header := table.header(true, width)
		for _, st := range stats {
			row := table.row(strings.Repeat("目录", 40), "12345678", st, width)
			if runewidth.StringWidth(row) != width {
				t.Fatal(row)
			}
			for _, pair := range [][2]string{{"Session", "12345678"}, {"Files", st.files}, {"Errors", st.errors}, {"Size", st.size}} {
				end := strings.LastIndex(header, pair[0]) + len(pair[0])
				suffixWidth := len(header) - end
				valueEnd := width - suffixWidth
				// Numeric values and hashes end exactly under their headings.
				actual := runewidth.Truncate(browserSkipColumns(row, valueEnd-runewidth.StringWidth(pair[1])), runewidth.StringWidth(pair[1]), "")
				if actual != pair[1] {
					t.Fatalf("column %s: want %q got %q in %s", pair[0], pair[1], actual, row)
				}
			}
			if strings.Contains(row, "files") || strings.Contains(row, "errors") {
				t.Fatal(row)
			}
		}
	}
	file := entryStats(browserEntry{count: 1, errors: 1, size: 10})
	if file.files != "—" || file.errors != "1" {
		t.Fatal(file)
	}
}
