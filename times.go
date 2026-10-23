package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // The runtime image may not have zoneinfo.
)

// Times are stored as naive UTC and shown in US/Eastern.
var eastern = mustLoadLocation("US/Eastern")

const timeLayout = "2006-01-02 15:04:05"

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// localtimeStr formats a stored (UTC) time in US/Eastern, like localtime_str()
// in models.py.
func localtimeStr(t time.Time) string {
	return t.In(eastern).Format(timeLayout)
}

// loadTimePattern is what Python's strptime() matches for '%Y-%m-%d %H:%M:%S':
// unpadded and (for the day) space-padded fields, and any whitespace between
// date and time. Python's \d also matches other scripts' digits; this doesn't.
var loadTimePattern = regexp.MustCompile(`^([0-9]{4})-(1[0-2]|0[1-9]|[1-9])-` +
	`(3[01]|[12][0-9]|0[1-9]|[1-9]| [1-9])` +
	`[\t\n\v\f\r \x1c-\x1f\x85\xa0\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}` +
	`\x{202f}\x{205f}\x{3000}]+` +
	`(2[0-3]|[0-1][0-9]|[0-9]):([0-5][0-9]|[0-9]):(6[0-1]|[0-5][0-9]|[0-9])$`)

// errLoadTimeRange is where pytz raises for a load time, which makes the
// workspace page a 500 in Python.
var errLoadTimeRange = errors.New("load time out of range")

// parseLoadTime parses the workspace page's time parameter, a US/Eastern time
// like "2017-12-30 12:00:00", into the UTC time of the revision to load, the
// way get_notes_list() in models.py does. ok is false where Python falls back
// to the latest revision.
func parseLoadTime(s string) (t time.Time, ok bool, err error) {
	m := loadTimePattern.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false, nil
	}
	var f [6]int
	for i := range f {
		// The day may be space-padded.
		f[i], _ = strconv.Atoi(strings.TrimLeft(m[i+1], " "))
	}
	year, month, day := f[0], time.Month(f[1]), f[2]
	naive := time.Date(year, month, day, f[3], f[4], f[5], 0, time.UTC)
	// datetime() rejects year 0, days past the end of the month and seconds 60
	// and 61, which time.Date would normalize.
	if year == 0 || naive.Month() != month || naive.Day() != day || f[5] > 59 {
		return time.Time{}, false, nil
	}
	// pytz's localize() looks a day either side, which overflows.
	if year == 1 && month == 1 && day == 1 || year == 9999 && month == 12 && day == 31 {
		return time.Time{}, false, errLoadTimeRange
	}
	return easternToUTC(naive), true, nil
}

// easternToUTC converts a US/Eastern wall-clock time (given as fields in UTC)
// to UTC the way pytz's localize() does with its default is_dst=False: an
// ambiguous time (the repeated hour in the fall) is standard time, and a time
// that doesn't exist (the skipped hour in the spring) is read with the
// standard-time offset. time.Date doesn't guarantee either.
func easternToUTC(naive time.Time) time.Time {
	var dst, std time.Time
	var stdOffset int
	for _, probe := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		_, offset := naive.Add(probe).In(eastern).Zone()
		isDST := naive.Add(probe).In(eastern).IsDST()
		if !isDST {
			stdOffset = offset
		}
		t := naive.Add(-time.Duration(offset) * time.Second)
		if _, actual := t.In(eastern).Zone(); actual == offset {
			if isDST {
				dst = t
			} else {
				std = t
			}
		}
	}
	switch {
	case !std.IsZero():
		return std
	case !dst.IsZero():
		return dst
	default:
		return naive.Add(-time.Duration(stdOffset) * time.Second)
	}
}
