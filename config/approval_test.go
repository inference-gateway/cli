package config

import "testing"

func TestResolveApprovalDelivery(t *testing.T) {
	tests := []struct {
		name      string
		behaviour string
		broker    bool
		isChat    bool
		want      string
	}{
		// prompt (default): adapts to the reachable channel, else blocks.
		{"prompt chat", ApprovalBehaviourPrompt, false, true, ApprovalBehaviourPrompt},
		{"prompt chat with broker still prompts", ApprovalBehaviourPrompt, true, true, ApprovalBehaviourPrompt},
		{"prompt headless+broker -> ipc (telegram)", ApprovalBehaviourPrompt, true, false, ApprovalBehaviourIPC},
		{"prompt headless no broker -> block (ci)", ApprovalBehaviourPrompt, false, false, ApprovalBehaviourBlock},

		// ipc: only delivers over a broker, blocks otherwise (incl. chat).
		{"ipc headless+broker", ApprovalBehaviourIPC, true, false, ApprovalBehaviourIPC},
		{"ipc chat+broker", ApprovalBehaviourIPC, true, true, ApprovalBehaviourIPC},
		{"ipc no broker -> block", ApprovalBehaviourIPC, false, false, ApprovalBehaviourBlock},
		{"ipc chat no broker -> block", ApprovalBehaviourIPC, false, true, ApprovalBehaviourBlock},

		// block: always blocks.
		{"block chat", ApprovalBehaviourBlock, false, true, ApprovalBehaviourBlock},
		{"block headless+broker", ApprovalBehaviourBlock, true, false, ApprovalBehaviourBlock},

		{"judge chat", ApprovalBehaviourJudge, false, true, ApprovalBehaviourJudge},
		{"judge headless+broker", ApprovalBehaviourJudge, true, false, ApprovalBehaviourJudge},
		{"judge headless no broker -> still judge", ApprovalBehaviourJudge, false, false, ApprovalBehaviourJudge},

		{"unknown chat -> prompt", "bogus", false, true, ApprovalBehaviourPrompt},
		{"unknown headless no broker -> block", "bogus", false, false, ApprovalBehaviourBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveApprovalDelivery(tt.behaviour, tt.broker, tt.isChat); got != tt.want {
				t.Errorf("ResolveApprovalDelivery(%q, broker=%v, chat=%v) = %q, want %q",
					tt.behaviour, tt.broker, tt.isChat, got, tt.want)
			}
		})
	}
}

func TestApprovalBehaviourFor(t *testing.T) {
	if got := DefaultConfig().Tools.Safety.ApprovalBehaviour; got != ApprovalBehaviourPrompt {
		t.Errorf("default approval_behaviour = %q, want %q", got, ApprovalBehaviourPrompt)
	}

	tests := []struct {
		set  string
		want string
	}{
		{ApprovalBehaviourPrompt, ApprovalBehaviourPrompt},
		{ApprovalBehaviourIPC, ApprovalBehaviourIPC},
		{ApprovalBehaviourBlock, ApprovalBehaviourBlock},
		{ApprovalBehaviourJudge, ApprovalBehaviourJudge},
		{"", ApprovalBehaviourPrompt},
		{"bogus", ApprovalBehaviourPrompt}, // unknown -> safe default
	}
	for _, tt := range tests {
		cfg := DefaultConfig()
		cfg.Tools.Safety.ApprovalBehaviour = tt.set
		if got := cfg.ApprovalBehaviourFor("Bash"); got != tt.want {
			t.Errorf("ApprovalBehaviourFor with %q = %q, want %q", tt.set, got, tt.want)
		}
	}
}

func TestConfigValidate_ApprovalBehaviour(t *testing.T) {
	valid := []string{"", ApprovalBehaviourPrompt, ApprovalBehaviourIPC, ApprovalBehaviourBlock}
	for _, v := range valid {
		cfg := DefaultConfig()
		cfg.Tools.Safety.ApprovalBehaviour = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with approval_behaviour %q returned error: %v", v, err)
		}
	}

	cfg := DefaultConfig()
	cfg.Tools.Safety.ApprovalBehaviour = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with approval_behaviour \"bogus\" should return an error")
	}
}

func TestConfigValidate_JudgeFailFast(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tools.Safety.ApprovalBehaviour = ApprovalBehaviourJudge
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with judge behaviour and no resolvable model should fail fast")
	}

	cfg.Judge.Model = "openai/gpt-5"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with judge.model set returned error: %v", err)
	}

	cfg.Judge.Model = ""
	cfg.Agent.Model = "anthropic/claude"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with agent.model fallback returned error: %v", err)
	}
}

func TestJudgeRequired(t *testing.T) {
	if (DefaultConfig()).JudgeRequired() {
		t.Error("JudgeRequired() on defaults should be false")
	}

	cfg := DefaultConfig()
	cfg.Tools.Safety.ApprovalBehaviour = ApprovalBehaviourJudge
	if !cfg.JudgeRequired() {
		t.Error("JudgeRequired() with approval_behaviour judge should be true")
	}
}

func TestConfigValidate_ComputerUseApproval(t *testing.T) {
	valid := []string{"", ComputerUseApprovalNever, ComputerUseApprovalDestructive, ComputerUseApprovalAlways}
	for _, v := range valid {
		cfg := DefaultConfig()
		cfg.ComputerUse.Approval = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with computer_use.approval %q returned error: %v", v, err)
		}
	}

	cfg := DefaultConfig()
	cfg.ComputerUse.Approval = "alway"
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with computer_use.approval \"alway\" should return an error")
	}
}
