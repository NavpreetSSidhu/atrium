package scheduling

import (
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	at := func(day, hhmm string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, ist)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	fmtAll := func(ts []time.Time) (s []string) {
		for _, v := range ts {
			s = append(s, v.In(ist).Format("01-02 15:04"))
		}
		return s
	}
	// 2026-12-07 is a Monday.
	week := Availability{Overrides: map[string][]Hours{}}
	week.Week[time.Monday] = []Hours{{9 * 60, 11 * 60}, {14 * 60, 15 * 60}}
	wide := SlotRule{Duration: 30 * time.Minute, NotBefore: at("2026-01-01", "00:00"), NotAfter: at("2027-01-01", "00:00")}

	cases := []struct {
		name     string
		a        Availability
		from, to string
		rule     SlotRule
		busy     []Busy
		want     []string
	}{
		{"weekly hours, two ranges", week, "2026-12-07", "2026-12-07", wide, nil,
			[]string{"12-07 09:00", "12-07 09:30", "12-07 10:00", "12-07 10:30", "12-07 14:00", "12-07 14:30"}},
		{"buffer widens the step", week, "2026-12-07", "2026-12-07", SlotRule{30 * time.Minute, 15 * time.Minute, wide.NotBefore, wide.NotAfter}, nil,
			[]string{"12-07 09:00", "12-07 09:45", "12-07 10:30", "12-07 14:00"}},
		{"slot must end inside hours", week, "2026-12-07", "2026-12-07", SlotRule{Duration: 45 * time.Minute, NotBefore: wide.NotBefore, NotAfter: wide.NotAfter}, nil,
			[]string{"12-07 09:00", "12-07 09:45", "12-07 14:00"}},
		{"busy time and its buffer are skipped", week, "2026-12-07", "2026-12-07", wide,
			[]Busy{{at("2026-12-07", "09:30"), at("2026-12-07", "10:10")}},
			[]string{"12-07 09:00", "12-07 10:30", "12-07 14:00", "12-07 14:30"}},
		{"minimum notice and horizon", week, "2026-12-07", "2026-12-14", SlotRule{Duration: 30 * time.Minute, NotBefore: at("2026-12-07", "10:15"), NotAfter: at("2026-12-07", "14:00")}, nil,
			[]string{"12-07 10:30", "12-07 14:00"}},
		{"days without hours are closed", week, "2026-12-08", "2026-12-13", wide, nil, nil},
		{"override closes a day", Availability{Week: week.Week, Overrides: map[string][]Hours{"2026-12-07": {}}}, "2026-12-07", "2026-12-07", wide, nil, nil},
		{"override replaces hours", Availability{Week: week.Week, Overrides: map[string][]Hours{"2026-12-07": {{18 * 60, 19 * 60}}}}, "2026-12-07", "2026-12-07", wide, nil,
			[]string{"12-07 18:00", "12-07 18:30"}},
		{"range to midnight", func() Availability {
			a := Availability{}
			a.Week[time.Monday] = []Hours{{23 * 60, 24 * 60}}
			return a
		}(), "2026-12-07", "2026-12-07", wide, nil, []string{"12-07 23:00", "12-07 23:30"}},
	}
	for _, c := range cases {
		got := fmtAll(Slots(ist, c.a, at(c.from, "00:00"), at(c.to, "00:00"), c.rule, c.busy))
		if len(got) != len(c.want) || (len(got) > 0 && !equal(got, c.want)) {
			t.Errorf("%s:\n got  %v\n want %v", c.name, got, c.want)
		}
	}

	// DST: in New York 2026-03-08 jumps 02:00 -> 03:00; 09:00 local must still be 09:00 local.
	ny, _ := time.LoadLocation("America/New_York")
	a := Availability{}
	a.Week[time.Sunday] = []Hours{{9 * 60, 10 * 60}}
	day := time.Date(2026, 3, 8, 0, 0, 0, 0, ny)
	got := Slots(ny, a, day, day, wide, nil)
	if len(got) != 2 || got[0].In(ny).Hour() != 9 {
		t.Errorf("DST day: %v", got)
	}
}

func equal(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
