package computer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	computermocks "github.com/inference-gateway/cli/tests/mocks/computer"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

func TestScreenControlTakeOverClaimsAndRefusesASecondClaim(t *testing.T) {
	notifier := &agentdomainmocks.FakeUINotifier{}
	control := NewScreenControl(config.DefaultConfig(), notifier)

	if err := control.TakeOver(); err != nil {
		t.Fatalf("TakeOver: %v", err)
	}
	if !control.Held() {
		t.Fatal("the person claimed the screen, Held is false")
	}
	if err := control.TakeOver(); err == nil {
		t.Fatal("a second claim while the person holds the screen must be refused")
	}
	notifies := notifier.Invocations()["Notify"]
	if len(notifies) != 1 {
		t.Fatalf("the control published %d events, want only the taken-over one", len(notifies))
	}
	taken, ok := notifies[0][0].(agentdomain.ScreenTakenOverEvent)
	if !ok || taken.GetTimestamp().IsZero() {
		t.Fatalf("the taken-over publish was %#v, want a stamped ScreenTakenOverEvent", notifies[0][0])
	}
}

func TestScreenControlHandBackReleasesOnce(t *testing.T) {
	notifier := &agentdomainmocks.FakeUINotifier{}
	control := NewScreenControl(config.DefaultConfig(), notifier)
	if err := control.TakeOver(); err != nil {
		t.Fatalf("TakeOver: %v", err)
	}

	if !control.HandBack() || control.Held() {
		t.Fatal("the hand-back did not release the held screen")
	}
	if control.HandBack() {
		t.Fatal("handing back an unheld screen reported a release")
	}

	notifies := notifier.Invocations()["Notify"]
	if len(notifies) != 2 {
		t.Fatalf("the control published %d events, want the taken-over and one handed-back", len(notifies))
	}
	handed, ok := notifies[1][0].(agentdomain.ScreenHandedBackEvent)
	if !ok || handed.GetTimestamp().IsZero() {
		t.Fatalf("the handed-back publish was %#v, want a stamped ScreenHandedBackEvent", notifies[1][0])
	}
}

func TestScreenControlWaitReleaseParksUntilHandBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		control := NewScreenControl(config.DefaultConfig(), &agentdomainmocks.FakeUINotifier{})
		if err := control.TakeOver(); err != nil {
			t.Fatalf("TakeOver: %v", err)
		}
		parked := make(chan error, 2)
		var waiting sync.WaitGroup
		for range 2 {
			waiting.Go(func() { parked <- control.WaitRelease(context.Background()) })
		}
		synctest.Wait()
		if len(parked) != 0 {
			t.Fatal("an agent action ran while the person held the screen")
		}
		control.HandBack()
		waiting.Wait()
		for range 2 {
			if err := <-parked; err != nil {
				t.Fatalf("a parked action ended with %v, want it released", err)
			}
		}
	})
}

func TestScreenControlWaitReleaseReparksAfterANewClaim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		control := NewScreenControl(config.DefaultConfig(), &agentdomainmocks.FakeUINotifier{})
		if err := control.TakeOver(); err != nil {
			t.Fatalf("TakeOver: %v", err)
		}
		first := make(chan error, 1)
		var waiting sync.WaitGroup
		waiting.Go(func() { first <- control.WaitRelease(context.Background()) })
		synctest.Wait()
		control.HandBack()
		if err := <-first; err != nil {
			t.Fatalf("the released action ended with %v", err)
		}

		if err := control.TakeOver(); err != nil {
			t.Fatalf("the second TakeOver: %v", err)
		}
		second := make(chan error, 1)
		waiting.Go(func() { second <- control.WaitRelease(context.Background()) })
		synctest.Wait()
		if len(second) != 0 {
			t.Fatal("an action ran into the new person's claim")
		}
		control.HandBack()
		waiting.Wait()
		if err := <-second; err != nil {
			t.Fatalf("the reparked action ended with %v", err)
		}
	})
}

func TestScreenControlWaitReleaseHonoursTheContext(t *testing.T) {
	control := NewScreenControl(config.DefaultConfig(), &agentdomainmocks.FakeUINotifier{})
	if err := control.TakeOver(); err != nil {
		t.Fatalf("TakeOver: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := control.WaitRelease(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitRelease = %v, want the turn's cancellation", err)
	}
}

func TestScreenControlDriveRefusesAnUnheldScreen(t *testing.T) {
	control := NewScreenControl(config.DefaultConfig(), &agentdomainmocks.FakeUINotifier{})
	err := control.Drive(t.Context(), computerdomain.ScreenInput{Kind: computerdomain.ActionClick})
	if !errors.Is(err, computerdomain.ErrScreenNotHeld) {
		t.Fatalf("Drive on an unheld screen = %v, want ErrScreenNotHeld", err)
	}
}

func TestScreenControlTellsItsFollowerAboutTheHold(t *testing.T) {
	control := NewScreenControl(&config.Config{}, nil)
	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}

	follower := &computermocks.FakeScreenFollower{}
	control.Follow(follower)
	if follower.HoldChangedCallCount() != 1 || !follower.HoldChangedArgsForCall(0) {
		t.Fatal("a follower that joins during a hold must be told the screen is held")
	}

	control.HandBack()
	if follower.HoldChangedCallCount() != 2 || follower.HoldChangedArgsForCall(1) {
		t.Fatal("handing back must tell the follower the hold ended")
	}

	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}
	if follower.HoldChangedCallCount() != 3 || !follower.HoldChangedArgsForCall(2) {
		t.Fatal("taking over must tell the follower the hold started")
	}
	if follower.InputLandedCallCount() != 0 {
		t.Fatal("no input landed yet")
	}
}
