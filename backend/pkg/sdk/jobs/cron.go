package jobs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSearchYears bounds how far ahead Next looks for a matching time.
const cronSearchYears = 5

// CronExpr is a parsed 5-field cron expression (minute hour day-of-month
// month day-of-week). It is immutable and safe for concurrent use.
type CronExpr struct {
	minute, hour, dom, month, dow uint64
	domStar, dowStar              bool
	src                           string
}

type cronField struct {
	name     string
	min, max int
	names    map[string]int
}

var (
	monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	dowNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

	fMinute = cronField{"minute", 0, 59, nil}
	fHour   = cronField{"hour", 0, 23, nil}
	fDom    = cronField{"day-of-month", 1, 31, nil}
	fMonth  = cronField{"month", 1, 12, monthNames}
	fDow    = cronField{"day-of-week", 0, 7, dowNames} // 7 is folded onto 0

	cronMacros = map[string]string{
		"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
		"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
	}
)

// ParseCron parses a standard 5-field cron expression or one of the macros
// @yearly, @annually, @monthly, @weekly, @daily, @midnight, @hourly.
func ParseCron(expr string) (*CronExpr, error) {
	src := strings.TrimSpace(expr)
	if src == "" {
		return nil, fmt.Errorf("cron: empty expression")
	}
	body := src
	if strings.HasPrefix(src, "@") {
		m, ok := cronMacros[strings.ToLower(src)]
		if !ok {
			return nil, fmt.Errorf("cron: unknown macro %q", src)
		}
		body = m
	}
	parts := strings.Fields(body)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields, got %d in %q", len(parts), src)
	}
	c := &CronExpr{src: src}
	var err error
	if c.minute, err = parseCronField(parts[0], fMinute); err != nil {
		return nil, err
	}
	if c.hour, err = parseCronField(parts[1], fHour); err != nil {
		return nil, err
	}
	if c.dom, err = parseCronField(parts[2], fDom); err != nil {
		return nil, err
	}
	if c.month, err = parseCronField(parts[3], fMonth); err != nil {
		return nil, err
	}
	if c.dow, err = parseCronField(parts[4], fDow); err != nil {
		return nil, err
	}
	if c.dow&(1<<7) != 0 { // 7 = Sunday
		c.dow = c.dow&^(1<<7) | 1
	}
	c.domStar = strings.HasPrefix(parts[2], "*")
	c.dowStar = strings.HasPrefix(parts[4], "*")
	return c, nil
}

func parseCronField(s string, f cronField) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(s, ",") {
		b, err := parseCronItem(item, f)
		if err != nil {
			return 0, fmt.Errorf("cron: %s field %q: %w", f.name, s, err)
		}
		bits |= b
	}
	return bits, nil
}

func parseCronItem(item string, f cronField) (uint64, error) {
	rangePart, stepPart, hasStep := strings.Cut(item, "/")
	step := 1
	if hasStep {
		n, err := strconv.Atoi(stepPart)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("invalid step %q", stepPart)
		}
		// A step beyond the field's span selects one value at most, and a
		// huge one would overflow the loop below.
		if n > f.max-f.min {
			return 0, fmt.Errorf("step %d is larger than the range %d-%d", n, f.min, f.max)
		}
		step = n
	}
	var lo, hi int
	switch {
	case rangePart == "*":
		lo, hi = f.min, f.max
		if f.name == "day-of-week" {
			hi = 6
		}
	case strings.Contains(rangePart, "-"):
		a, b, _ := strings.Cut(rangePart, "-")
		var err error
		if lo, err = parseCronValue(a, f); err != nil {
			return 0, err
		}
		if hi, err = parseCronValue(b, f); err != nil {
			return 0, err
		}
		if lo > hi {
			return 0, fmt.Errorf("range start %d is after end %d", lo, hi)
		}
	default:
		v, err := parseCronValue(rangePart, f)
		if err != nil {
			return 0, err
		}
		lo, hi = v, v
		if hasStep { // a/n means a-max/n
			hi = f.max
		}
	}
	var bits uint64
	for v := lo; v <= hi; v += step {
		bits |= 1 << uint(v)
	}
	return bits, nil
}

func parseCronValue(s string, f cronField) (int, error) {
	if v, ok := f.names[strings.ToLower(s)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", s)
	}
	if v < f.min || v > f.max {
		return 0, fmt.Errorf("value %d out of range %d-%d", v, f.min, f.max)
	}
	return v, nil
}

// String returns the expression as it was given (trimmed).
func (c *CronExpr) String() string { return c.src }

func (c *CronExpr) dayMatches(t time.Time) bool {
	domOK := c.dom&(1<<uint(t.Day())) != 0
	dowOK := c.dow&(1<<uint(t.Weekday())) != 0
	if !c.domStar && !c.dowStar { // Vixie rule: either may match
		return domOK || dowOK
	}
	return domOK && dowOK
}

// Next returns the first matching time strictly after `after`, evaluated on
// the wall clock of loc (nil means UTC), or false if there is none within five
// years. A wall time that does not exist in loc (DST gap) is skipped; one that
// occurs twice fires once, at its first occurrence.
func (c *CronExpr) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	a := after.In(loc)
	// Walk "civil" time: wall-clock fields held in a UTC time, which has no DST.
	civil := time.Date(a.Year(), a.Month(), a.Day(), a.Hour(), a.Minute(), 0, 0, time.UTC).Add(time.Minute)
	limit := civil.AddDate(cronSearchYears, 0, 0)
	for civil.Before(limit) {
		switch {
		case c.month&(1<<uint(civil.Month())) == 0:
			civil = time.Date(civil.Year(), civil.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !c.dayMatches(civil):
			civil = time.Date(civil.Year(), civil.Month(), civil.Day()+1, 0, 0, 0, 0, time.UTC)
		case c.hour&(1<<uint(civil.Hour())) == 0:
			civil = civil.Truncate(time.Hour).Add(time.Hour)
		case c.minute&(1<<uint(civil.Minute())) == 0:
			civil = civil.Add(time.Minute)
		default:
			t := time.Date(civil.Year(), civil.Month(), civil.Day(), civil.Hour(), civil.Minute(), 0, 0, loc)
			if t.Hour() == civil.Hour() && t.Minute() == civil.Minute() && t.Day() == civil.Day() {
				// Ambiguous wall time: prefer the first occurrence.
				if e := t.Add(-time.Hour); e.Hour() == civil.Hour() && e.Minute() == civil.Minute() && e.Day() == civil.Day() {
					t = e
				}
				if t.After(after) {
					return t, true
				}
			}
			// Nonexistent (gap) or already fired: move on to the next minute.
			civil = civil.Add(time.Minute)
		}
	}
	return time.Time{}, false
}
