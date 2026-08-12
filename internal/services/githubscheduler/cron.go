// Package githubscheduler implements the "github" scheduling backend: each
// scheduled job is materialized as a GitHub Actions scheduled workflow in a
// user-configured repository, deployed via pull requests. GitHub runs the
// jobs; nothing executes locally except the optional artifact poller.
package githubscheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	scheduler "github.com/inference-gateway/cli/internal/services/scheduler"
)

// ghCronConstraints names the GitHub Actions scheduling limits for error text.
const ghCronConstraints = "GitHub Actions cron is UTC-only, 5-field, minimum 5-minute interval"

var descriptorTable = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

// TranslateCron converts a robfig/cron expression (including @descriptors and
// @every durations) into a GitHub Actions compatible 5-field UTC cron, or
// returns an error explaining why the expression cannot be scheduled on GitHub.
func TranslateCron(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	if translated, ok := descriptorTable[strings.ToLower(expr)]; ok {
		return translated, nil
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(expr), "@every "); ok {
		return translateEvery(strings.TrimSpace(rest))
	}
	if strings.HasPrefix(expr, "@") {
		return "", fmt.Errorf("cron descriptor %q is not supported by the github backend (%s)", expr, ghCronConstraints)
	}
	if err := scheduler.ParseCron(expr); err != nil {
		return "", fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return "", fmt.Errorf("cron expression %q must have exactly 5 fields for the github backend (%s)", expr, ghCronConstraints)
	}
	if err := checkMinuteField(fields[0]); err != nil {
		return "", fmt.Errorf("cron expression %q: %w (%s)", expr, err, ghCronConstraints)
	}
	return strings.Join(fields, " "), nil
}

// checkMinuteField rejects the obvious sub-5-minute minute fields.
// ponytail: only catches * and */N; exotic lists (1,2,3 * * * *) pass through - GitHub throttles them.
func checkMinuteField(minute string) error {
	if minute == "*" {
		return fmt.Errorf("fires every minute")
	}
	if step, ok := strings.CutPrefix(minute, "*/"); ok {
		n, err := strconv.Atoi(step)
		if err == nil && n < 5 {
			return fmt.Errorf("fires every %d minute(s)", n)
		}
	}
	return nil
}

// translateEvery maps `@every <duration>` onto a */N cron when the duration
// divides an hour (or a day) evenly; anything else is rejected.
func translateEvery(durStr string) (string, error) {
	d, err := time.ParseDuration(durStr)
	if err != nil {
		return "", fmt.Errorf("invalid @every duration %q: %w", durStr, err)
	}
	reject := func() (string, error) {
		return "", fmt.Errorf("@every %s cannot be expressed as a GitHub Actions cron (%s; use a whole number of minutes dividing 60, or hours dividing 24)", durStr, ghCronConstraints)
	}
	if d%time.Minute != 0 || d < 5*time.Minute {
		return reject()
	}
	if d%time.Hour == 0 {
		h := int(d / time.Hour)
		switch {
		case h == 1:
			return "0 * * * *", nil
		case h == 24:
			return "0 0 * * *", nil
		case h < 24 && 24%h == 0:
			return fmt.Sprintf("0 */%d * * *", h), nil
		default:
			return reject()
		}
	}
	if d < time.Hour {
		m := int(d / time.Minute)
		if 60%m == 0 {
			return fmt.Sprintf("*/%d * * * *", m), nil
		}
	}
	return reject()
}
