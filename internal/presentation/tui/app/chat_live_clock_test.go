package app

import (
	"testing"
	"time"
)

func TestLiveClockKeepsOneChainAtTheNeededCadence(t *testing.T) {
	var c liveClock

	if c.sync(0) != nil {
		t.Fatal("nothing live must not arm the clock")
	}
	if c.sync(time.Second) == nil {
		t.Fatal("a live counter must arm the clock")
	}
	if c.sync(time.Second) != nil {
		t.Fatal("an unchanged cadence must keep the chain in flight, not arm a second one")
	}
	slow := liveClockTickMsg{epoch: c.epoch}
	if c.sync(100*time.Millisecond) == nil {
		t.Fatal("a spinner appearing must retune the clock")
	}
	if c.fire(slow) {
		t.Fatal("the superseded slow tick must be dropped")
	}
	if !c.fire(liveClockTickMsg{epoch: c.epoch}) {
		t.Fatal("the tick in flight must fire")
	}
	if c.sync(100*time.Millisecond) == nil {
		t.Fatal("a fired tick must re-arm while the spinner stays")
	}
	if c.sync(0) != nil || c.fire(liveClockTickMsg{epoch: c.epoch - 1}) {
		t.Fatal("the clock must stop once nothing is live and drop the tick it left in flight")
	}
}
