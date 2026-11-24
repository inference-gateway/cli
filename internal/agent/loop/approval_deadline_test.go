package loop

import (
	"testing"
	"testing/synctest"
	"time"

	config "github.com/inference-gateway/cli/config"
)

func TestAnApprovalWaitsAsLongAsApprovalTimeoutSays(t *testing.T) {
	withTimeout := func(timeout time.Duration) *config.Config {
		cfg := config.DefaultConfig()
		cfg.Tools.Safety.ApprovalTimeout = timeout
		return cfg
	}
	tests := []struct {
		name  string
		agent *Agent
		want  time.Duration
	}{
		{"the default waits five minutes", &Agent{config: config.DefaultConfig()}, 5 * time.Minute},
		{"an agent without config waits the default", &Agent{}, config.DefaultApprovalTimeout},
		{"a set timeout bounds the wait", &Agent{config: withTimeout(time.Hour)}, time.Hour},
		{"zero waits until the approval is decided", &Agent{config: withTimeout(0)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				deadline := tt.agent.approvalDeadline()
				if tt.want == 0 {
					if deadline != nil {
						t.Fatal("an approval without a timeout gave up after a deadline")
					}
					return
				}
				started := time.Now()
				<-deadline
				if waited := time.Since(started); waited != tt.want {
					t.Fatalf("the approval gave up after %v, want %v", waited, tt.want)
				}
			})
		})
	}
}
