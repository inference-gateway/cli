package domain

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"every minute", "* * * * *", false},
		{"daily at 8", "0 8 * * *", false},
		{"weekday range", "30 9 * * 1-5", false},
		{"step values", "*/15 * * * *", false},
		{"list values", "0 0,12 1 */2 *", false},
		{"named month and day", "0 0 1 jan sun", false},
		{"descriptor @daily", "@daily", false},
		{"descriptor @hourly", "@hourly", false},
		{"descriptor @weekly", "@weekly", false},
		{"descriptor @every", "@every 1h30m", false},
		{"empty", "", true},
		{"garbage", "not a cron", true},
		{"too few fields", "0 8 * *", true},
		{"six fields (seconds not enabled)", "0 0 8 * * *", true},
		{"out of range minute", "60 * * * *", true},
		{"out of range hour", "* 24 * * *", true},
		{"unknown descriptor", "@fortnightly", true},
		{"bad @every duration", "@every banana", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ParseCron(tt.expr); (err != nil) != tt.wantErr {
				t.Fatalf("ParseCron(%q) err = %v, wantErr %v", tt.expr, err, tt.wantErr)
			}
		})
	}
}

func TestScheduledJobLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	if _, err := NewScheduledJob(ScheduledJob{CronExpression: "not a cron"}, now); err == nil {
		t.Fatal("NewScheduledJob accepted an invalid schedule")
	}
	job, err := NewScheduledJob(ScheduledJob{CronExpression: "0 8 * * *", Prompt: "p"}, now)
	if err != nil || job.ID == "" || !job.CreatedAt.Equal(now) || !job.UpdatedAt.Equal(now) {
		t.Fatalf("NewScheduledJob = %+v, %v", job, err)
	}

	later := now.Add(time.Hour)
	if changed, err := job.Apply(JobPatch{}, later); changed || err != nil || !job.UpdatedAt.Equal(now) {
		t.Fatalf("empty patch: changed=%v err=%v updated=%v", changed, err, job.UpdatedAt)
	}
	bad := "60 * * * *"
	if _, err := job.Apply(JobPatch{CronExpression: &bad}, later); err == nil || job.CronExpression != "0 8 * * *" {
		t.Fatalf("invalid cron patch: err=%v cron=%q", err, job.CronExpression)
	}
	name, once := "daily", true
	if changed, err := job.Apply(JobPatch{Name: &name, RunOnce: &once}, later); !changed || err != nil {
		t.Fatalf("patch: changed=%v err=%v", changed, err)
	}
	if job.Name != "daily" || !job.RunOnce || job.Prompt != "p" || !job.UpdatedAt.Equal(later) {
		t.Fatalf("after patch: %+v", job)
	}
}
