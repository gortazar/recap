package cli

import (
	"strings"
	"testing"
	"time"
)

func TestParseDurationAccepts(t *testing.T) {
	const day = 24 * time.Hour
	cases := []struct {
		in   string
		want time.Duration
	}{
		// Everything that parsed before this grammar existed still parses, and to the same
		// thing. Anything else here would be a regression, not a feature.
		{"24h", 24 * time.Hour},
		{"90m", 90 * time.Minute},
		{"2d", 2 * day},
		{"1.5d", 36 * time.Hour},
		{"30s", 30 * time.Second},
		{"1h30m", 90 * time.Minute},

		// What the idea actually asked for.
		{"6h", 6 * time.Hour},
		{"7d", 7 * day},

		// New: weeks, chained terms, fractions and case.
		{"2w", 14 * day},
		{"1w", 7 * day},
		{"2d12h", 60 * time.Hour},
		{"1w2d", 9 * day},
		{"1d1h1m1s", 25*time.Hour + time.Minute + time.Second},
		{"6H", 6 * time.Hour},
		{"7D", 7 * day},
		{"1W", 7 * day},
		{"0.5h", 30 * time.Minute},
		{".5h", 30 * time.Minute},
		{"1.5w", 10*day + 12*time.Hour},
		{" 6h ", 6 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseDuration(c.in)
			if err != nil {
				t.Fatalf("parseDuration(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("parseDuration(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseDurationRejects(t *testing.T) {
	cases := []struct {
		in   string
		want string // a fragment the message must contain
	}{
		// The answered question: a bare number is an error, because 6 is as likely to have
		// meant days as hours.
		{"6", "6h"},
		{"0", "6h"},
		{"7.5", "6h"},

		// The other answered question: --since is relative only.
		{"yesterday", "6h"},
		{"09:00", "6h"},
		{"2026-08-10", "6h"},

		{"", "6h"},
		{"h", "6h"},
		{"6x", "6h"},
		{"6 h", "6h"},
		{"6hh", "6h"},
		{"h6", "6h"},
		{"--6h", "6h"},
		{"6h-", "6h"},
		{"1.2.3h", "6h"},
	}
	for _, c := range cases {
		t.Run("rejects "+c.in, func(t *testing.T) {
			_, err := parseDuration(c.in)
			if err == nil {
				t.Fatalf("parseDuration(%q) succeeded, want an error", c.in)
			}
			// Every rejection has to say what would have been accepted: "not a duration"
			// leaves the user guessing at the vocabulary.
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("parseDuration(%q) said %q, want it to show an example like %q", c.in, err, c.want)
			}
			if !strings.Contains(err.Error(), "s, m, h, d, w") {
				t.Errorf("parseDuration(%q) said %q, want it to list the units", c.in, err)
			}
		})
	}
}

// Negative values parse — the window rule below rejects them with a message about windows,
// which is more useful than a grammar complaint.
func TestParseDurationAcceptsNegativesForTheWindowRuleToReject(t *testing.T) {
	got, err := parseDuration("-6h")
	if err != nil {
		t.Fatalf("parseDuration(-6h): %v", err)
	}
	if got != -6*time.Hour {
		t.Errorf("parseDuration(-6h) = %v, want -6h", got)
	}
}

func TestParseDurationOverflowIsAnErrorNotAWrappedNumber(t *testing.T) {
	// Large enough to overflow an int64 of nanoseconds many times over. Silently wrapping
	// to a negative window would be the worst possible answer.
	if _, err := parseDuration("999999999999w"); err == nil {
		t.Error("parseDuration accepted a duration too large to represent")
	}
}
