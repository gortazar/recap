package cli

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// durationUnits is the whole vocabulary. Days are 24 hours and weeks are 7 days — no
// calendar arithmetic, so "1w ago" is the same length whatever the month or the timezone.
//
// Months and years are deliberately absent: a month would have to choose between 30 days and
// a calendar month, and "everything since a month ago" is what --all is for.
var durationUnits = map[string]time.Duration{
	"s": time.Second,
	"m": time.Minute,
	"h": time.Hour,
	"d": 24 * time.Hour,
	"w": 7 * 24 * time.Hour,
}

// errNotADuration is what every rejection says. It names the units and shows the forms
// someone is most likely to want, because a message that only says "not a duration" leaves
// the reader guessing at the vocabulary.
var errNotADuration = errors.New("expected a duration like 6h, 90m or 7d — a number and a unit (s, m, h, d, w), optionally chained: 2d12h")

// parseDuration reads a chain of <number><unit> terms: 90m, 6h, 7d, 2w, 1.5d, 2d12h, 6H.
//
// A bare number is an error. "6" is as likely to have meant six days as six hours, and
// guessing would silently report the wrong window.
//
// Negative values parse. The caller rejects them with a message about windows, which is more
// use than a complaint about grammar.
func parseDuration(s string) (time.Duration, error) {
	rest := strings.ToLower(strings.TrimSpace(s))
	if rest == "" {
		return 0, errNotADuration
	}

	negative := false
	if after, ok := strings.CutPrefix(rest, "-"); ok {
		negative, rest = true, after
	} else if after, ok := strings.CutPrefix(rest, "+"); ok {
		rest = after
	}
	if rest == "" {
		return 0, errNotADuration
	}

	var total float64
	for rest != "" {
		digits := 0
		for digits < len(rest) && (rest[digits] >= '0' && rest[digits] <= '9' || rest[digits] == '.') {
			digits++
		}
		if digits == 0 {
			return 0, errNotADuration // a unit with no number in front of it
		}
		value, err := strconv.ParseFloat(rest[:digits], 64)
		if err != nil {
			return 0, errNotADuration // "1.2.3", and anything else ParseFloat dislikes
		}
		rest = rest[digits:]

		if rest == "" {
			return 0, errNotADuration // a number with no unit after it
		}
		unit, ok := durationUnits[rest[:1]]
		if !ok {
			return 0, errNotADuration
		}
		rest = rest[1:]

		total += value * float64(unit)
	}

	// float64 counts nanoseconds exactly only up to 2^53 of them, about 104 days; beyond
	// that this is approximate, which is fine for a report window. What is not fine is
	// wrapping round into a negative window, so anything out of range is an error.
	if math.IsInf(total, 0) || math.IsNaN(total) || math.Abs(total) > math.MaxInt64 {
		return 0, fmt.Errorf("%w (that one is too large to represent)", errNotADuration)
	}
	if negative {
		total = -total
	}
	return time.Duration(total), nil
}
