package cron

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CronExpr represents a parsed cron expression.
type CronExpr struct {
	minutes    []int // 0-59
	hours      []int // 0-23
	daysOfMonth []int // 1-31
	months     []int // 1-12
	daysOfWeek []int // 0-6 (0=Sunday)
	every      time.Duration // for @every syntax
}

// ParseCron parses a cron expression string.
// Supports 5-field standard format and predefined macros.
func ParseCron(expr string) (*CronExpr, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty cron expression")
	}

	// Handle predefined macros
	if strings.HasPrefix(expr, "@") {
		return parseMacro(expr)
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	c := &CronExpr{}
	var err error

	if c.minutes, err = parseField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if c.hours, err = parseField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if c.daysOfMonth, err = parseField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	if c.months, err = parseField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if c.daysOfWeek, err = parseField(fields[4], 0, 6); err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}

	return c, nil
}

func parseMacro(expr string) (*CronExpr, error) {
	switch expr {
	case "@hourly":
		return ParseCron("0 * * * *")
	case "@daily":
		return ParseCron("0 0 * * *")
	case "@weekly":
		return ParseCron("0 0 * * 0")
	case "@monthly":
		return ParseCron("0 0 1 * *")
	}

	if durStr, ok := strings.CutPrefix(expr, "@every "); ok {
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("invalid @every duration %q: %w", durStr, err)
		}
		if dur < time.Minute {
			return nil, fmt.Errorf("@every duration must be at least 1 minute, got %v", dur)
		}
		return &CronExpr{every: dur}, nil
	}

	return nil, fmt.Errorf("unknown macro %q", expr)
}

// parseField parses a single cron field (e.g., "*/15", "1-5", "1,3,5", "*").
func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return makeRange(min, max, 1), nil
	}

	// Handle */step
	if stepStr, ok := strings.CutPrefix(field, "*/"); ok {
		step, err := strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid step %q", stepStr)
		}
		return makeRange(min, max, step), nil
	}

	// Handle comma-separated list
	if strings.Contains(field, ",") {
		var vals []int
		for part := range strings.SplitSeq(field, ",") {
			v, err := parseSingle(part, min, max)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v...)
		}
		return vals, nil
	}

	// Handle range (e.g., "1-5")
	if strings.Contains(field, "-") {
		return parseRange(field, min, max)
	}

	// Single value
	v, err := strconv.Atoi(field)
	if err != nil {
		return nil, fmt.Errorf("invalid value %q", field)
	}
	if v < min || v > max {
		return nil, fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
	}
	return []int{v}, nil
}

func parseSingle(part string, min, max int) ([]int, error) {
	part = strings.TrimSpace(part)
	if strings.Contains(part, "-") {
		return parseRange(part, min, max)
	}
	v, err := strconv.Atoi(part)
	if err != nil {
		return nil, fmt.Errorf("invalid value %q", part)
	}
	if v < min || v > max {
		return nil, fmt.Errorf("value %d out of range [%d, %d]", v, min, max)
	}
	return []int{v}, nil
}

func parseRange(field string, min, max int) ([]int, error) {
	parts := strings.SplitN(field, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range %q", field)
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid range start %q", parts[0])
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid range end %q", parts[1])
	}
	if start < min || end > max || start > end {
		return nil, fmt.Errorf("range %d-%d out of bounds [%d, %d]", start, end, min, max)
	}
	return makeRange(start, end, 1), nil
}

func makeRange(min, max, step int) []int {
	var result []int
	for i := min; i <= max; i += step {
		result = append(result, i)
	}
	return result
}

// NextAfter returns the next time after t that matches this cron expression.
func (c *CronExpr) NextAfter(t time.Time) time.Time {
	if c.every > 0 {
		return t.Add(c.every)
	}

	// Start from the next minute boundary
	next := t.Truncate(time.Minute).Add(time.Minute)

	// Search forward up to 366 days (covers all cron patterns)
	limit := next.Add(366 * 24 * time.Hour)

	for next.Before(limit) {
		if c.matches(next) {
			return next
		}
		next = next.Add(time.Minute)
	}

	// Should not happen for valid expressions
	return time.Time{}
}

// matches reports whether t matches this cron expression.
func (c *CronExpr) matches(t time.Time) bool {
	return slices.Contains(c.minutes, t.Minute()) &&
		slices.Contains(c.hours, t.Hour()) &&
		slices.Contains(c.daysOfMonth, t.Day()) &&
		slices.Contains(c.months, int(t.Month())) &&
		slices.Contains(c.daysOfWeek, int(t.Weekday()))
}
