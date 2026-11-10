package scheduling

import (
	"slices"
	"time"
)

// Hours is a working range in minutes from local midnight, [Start, End).
type Hours struct {
	Start int `json:"start_min"`
	End   int `json:"end_min"`
}

// Week holds weekly hours by weekday. Overrides replace a date's hours ("2006-01-02" -> hours);
// a present-but-empty entry closes that day.
type Availability struct {
	Week      [7][]Hours
	Overrides map[string][]Hours
}

// Busy is a blocked range [Start, End): a booking plus its buffer.
type Busy struct{ Start, End time.Time }

type SlotRule struct {
	Duration, Buffer    time.Duration
	NotBefore, NotAfter time.Time // minimum notice and booking horizon, absolute
}

// Slots returns bookable start times for local dates [from, to] in loc. Slots step by duration +
// buffer from the start of each working range, must end inside it, and their duration + buffer must
// not overlap busy time.
func Slots(loc *time.Location, a Availability, from, to time.Time, r SlotRule, busy []Busy) []time.Time {
	var out []time.Time
	step := r.Duration + r.Buffer
	if step <= 0 {
		return nil
	}
	y, m, d := from.In(loc).Date()
	for day := time.Date(y, m, d, 0, 0, 0, 0, loc); !day.After(to); day = day.AddDate(0, 0, 1) {
		hours, ok := a.Overrides[day.Format(time.DateOnly)]
		if !ok {
			hours = a.Week[day.Weekday()]
		}
		for _, h := range hours {
			// time.Date normalizes minute overflow and handles DST, unlike adding durations to midnight.
			end := time.Date(day.Year(), day.Month(), day.Day(), 0, h.End, 0, 0, loc)
			for t := time.Date(day.Year(), day.Month(), day.Day(), 0, h.Start, 0, 0, loc); !t.Add(r.Duration).After(end); t = t.Add(step) {
				if t.Before(r.NotBefore) || t.After(r.NotAfter) || overlaps(t, t.Add(step), busy) {
					continue
				}
				out = append(out, t)
			}
		}
	}
	slices.SortFunc(out, time.Time.Compare)
	return slices.Compact(out)
}

func overlaps(start, end time.Time, busy []Busy) bool {
	for _, b := range busy {
		if start.Before(b.End) && b.Start.Before(end) {
			return true
		}
	}
	return false
}
