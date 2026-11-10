package logger

import (
	"errors"
	"testing"
	"time"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
)

func recordingLogger() (*zap.Logger, *problemRing) {
	ring := &problemRing{}
	return zap.New(&problemCore{ring: ring}), ring
}

func TestOnlyWarningsAndErrorsAreRecordedAsProblems(t *testing.T) {
	log, ring := recordingLogger()

	log.Debug("cache miss")
	log.Info("server started")
	log.Warn("retrying", zap.Int("attempt", 2))
	log.Error("teleport failed", zap.Error(errors.New("pod is in CrashLoopBackOff")), zap.String("caller_hint", "ignored"))

	problems := ring.since(time.Time{})
	if len(problems) != 2 {
		t.Fatalf("recorded %d problems, want the warning and the error", len(problems))
	}
	if problems[0].Severity != "warn" || problems[0].Message != "retrying" || problems[0].Detail != "" {
		t.Fatalf("unexpected warning %+v", problems[0])
	}
	if problems[1].Severity != "error" || problems[1].Detail != "pod is in CrashLoopBackOff" {
		t.Fatalf("unexpected error %+v", problems[1])
	}
}

func TestAProblemKeepsTheConversationItsLoggerWasTaggedWith(t *testing.T) {
	log, ring := recordingLogger()

	log.With(zap.String("conversation_id", "conv-1")).Warn("approval request failed")
	log.Warn("gateway restarted")

	problems := ring.since(time.Time{})
	if problems[0].ConversationID != "conv-1" {
		t.Fatalf("the tagged logger's problem belongs to %q, want conv-1", problems[0].ConversationID)
	}
	if problems[1].ConversationID != "" {
		t.Fatalf("an untagged problem must belong to no conversation, got %q", problems[1].ConversationID)
	}
}

func TestProblemsAreReadFromAMomentOn(t *testing.T) {
	ring := &problemRing{}
	start := time.Now()
	ring.add(Problem{Time: start.Add(-time.Minute), Message: "before"})
	ring.add(Problem{Time: start, Message: "at"})
	ring.add(Problem{Time: start.Add(time.Minute), Message: "after"})

	recent := ring.since(start)
	if len(recent) != 2 || recent[0].Message != "at" || recent[1].Message != "after" {
		t.Fatalf("since the moment got %+v, want the two problems from it on", recent)
	}
}

func TestTheProblemRecordDropsItsOldestWhenFull(t *testing.T) {
	ring := &problemRing{}
	for i := range maxRecentProblems + 3 {
		ring.add(Problem{Time: time.Unix(int64(i), 0)})
	}

	kept := ring.since(time.Time{})
	if len(kept) != maxRecentProblems {
		t.Fatalf("kept %d problems, want the cap of %d", len(kept), maxRecentProblems)
	}
	if !kept[0].Time.Equal(time.Unix(3, 0)) {
		t.Fatalf("the oldest kept problem is from %v, want the fourth one logged", kept[0].Time)
	}
}

func TestTheRecorderDoesNotChangeWhatTheLogWrites(t *testing.T) {
	var written []string
	sink := zapcore.AddSync(writerFunc(func(p []byte) (int, error) {
		written = append(written, string(p))
		return len(p), nil
	}))
	base := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), sink, zapcore.InfoLevel)
	log := zap.New(recordingProblems(base))

	log.Info("kept in the log")
	log.Warn("kept in both")

	if len(written) != 2 {
		t.Fatalf("the log wrote %d lines, want both", len(written))
	}
}

type writerFunc func(p []byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }
