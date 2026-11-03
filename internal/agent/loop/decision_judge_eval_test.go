package loop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	decisiongateway "github.com/inference-gateway/cli/internal/decision/infrastructure/gateway"
)

// The eval runs the labelled judge cases through a real decision model. It is
// skipped unless INFER_JUDGE_EVAL_MODEL names one, since it needs a gateway
// started with DECISIONS_ENABLED=true at INFER_GATEWAY_URL.
const (
	judgeEvalModelEnv   = "INFER_JUDGE_EVAL_MODEL"
	judgeEvalGatewayEnv = "INFER_GATEWAY_URL"
	judgeEvalAPIKeyEnv  = "INFER_GATEWAY_API_KEY"
	judgeEvalDefaultURL = "http://localhost:8080/v1"
	judgeEvalFile       = "judge_eval.jsonl"
	judgeEvalTimeout    = 60 * time.Second
)

type judgeEvalCase struct {
	Name     string `json:"name"`
	Root     string `json:"root"`
	Intent   string `json:"intent"`
	Action   string `json:"action"`
	Expected string `json:"expected"`
}

func TestDecisionJudge_Eval(t *testing.T) {
	model := os.Getenv(judgeEvalModelEnv)
	if model == "" {
		t.Skipf("set %s=provider/model to run the judge eval against a gateway", judgeEvalModelEnv)
	}
	cfg := config.DefaultConfig()
	cfg.Judge = config.JudgeConfig{Backend: config.JudgeBackendDecisions, Timeout: int(judgeEvalTimeout.Seconds())}
	client := sdk.NewClient(&sdk.ClientOptions{
		BaseURL: judgeEvalURL(),
		APIKey:  os.Getenv(judgeEvalAPIKeyEnv),
		Timeout: judgeEvalTimeout,
	})
	judge := NewDecisionJudge(decisiongateway.New(client), cfg)

	var correct, falseApproves, tokens int
	var latencies []time.Duration
	cases := loadJudgeEvalCases(t)
	for _, c := range cases {
		started := time.Now()
		out, err := judge.Judge(context.Background(), agentdomain.JudgeInput{
			Model: model, RootIntent: c.Root, Intent: c.Intent,
			Actions: []agentdomain.JudgeAction{{ID: c.Name, Action: c.Action}},
		})
		latencies = append(latencies, time.Since(started))
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		verdict := out.Verdicts[0]
		got := string(verdict.Decision)
		if out.Usage != nil {
			tokens += int(out.Usage.PromptTokens)
		}
		if got == c.Expected {
			correct++
		} else {
			t.Logf("MISS %-32s expected %-8s got %-8s %s", c.Name, c.Expected, got, verdict.Reason)
		}
		if verdict.Approved() && c.Expected == string(agentdomain.JudgeDecisionRejected) {
			falseApproves++
		}
	}

	slices.Sort(latencies)
	t.Logf("model=%s cases=%d accuracy=%.1f%% false_approves=%d p50=%s p95=%s tokens_per_decision=%d",
		model, len(cases), 100*float64(correct)/float64(len(cases)), falseApproves,
		latencies[len(latencies)/2], latencies[len(latencies)*95/100], tokens/len(cases))
}

func judgeEvalURL() string {
	if url := os.Getenv(judgeEvalGatewayEnv); url != "" {
		return url
	}
	return judgeEvalDefaultURL
}

func loadJudgeEvalCases(t *testing.T) []judgeEvalCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", judgeEvalFile))
	if err != nil {
		t.Fatalf("reading eval cases: %v", err)
	}

	var cases []judgeEvalCase
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var c judgeEvalCase
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			t.Fatalf("decoding eval case: %v", err)
		}
		cases = append(cases, c)
	}
	return cases
}
