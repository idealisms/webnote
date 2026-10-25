package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// readGolden reads [input, expected] pairs written by tools/gen_golden.py from
// the Python app (git show 85a04b5:tools/gen_golden.py).
func readGolden(t *testing.T, name string) [][2]string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var cases [][2]string
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// Times before 1970 aren't matched: pytz uses local mean time before 1902 and
// formats years before 1000 unpadded. Webnote has no data before 2006.
func before1970(s string) bool {
	return s < "1970"
}

func TestLocaltimeStr(t *testing.T) {
	// Times are stored as naive UTC and displayed in US/Eastern.
	winter := time.Date(2017, 12, 31, 5, 0, 0, 0, time.UTC)
	if got := localtimeStr(winter); got != "2017-12-31 00:00:00" {
		t.Errorf("localtimeStr(winter) = %q", got)
	}
	summer := time.Date(2017, 7, 4, 16, 30, 15, 0, time.UTC)
	if got := localtimeStr(summer); got != "2017-07-04 12:30:15" {
		t.Errorf("localtimeStr(summer) = %q", got)
	}
}

func TestLocaltimeStrMatchesPython(t *testing.T) {
	n := 0
	for _, c := range readGolden(t, "localtime.json") {
		if before1970(c[0]) {
			continue
		}
		utc, err := time.Parse(timeLayout, c[0])
		if err != nil {
			t.Fatal(err)
		}
		if got := localtimeStr(utc); got != c[1] {
			t.Errorf("localtimeStr(%s UTC) = %q, want %q", c[0], got, c[1])
		}
		n++
	}
	t.Logf("checked %d times", n)
}

func TestParseLoadTimeMatchesPython(t *testing.T) {
	n := 0
	for _, c := range readGolden(t, "load_time.json") {
		input, want := c[0], c[1]
		if want == "error" {
			want = "fallback" // Python's page was a 500.
		} else if want != "fallback" && before1970(want) {
			continue
		}
		got := "fallback"
		if utc, ok := parseLoadTime(input); ok {
			got = utc.Format(timeLayout)
		}
		if got != want {
			t.Errorf("parseLoadTime(%q) = %q, want %q", input, got, want)
		}
		n++
	}
	t.Logf("checked %d inputs", n)
}
