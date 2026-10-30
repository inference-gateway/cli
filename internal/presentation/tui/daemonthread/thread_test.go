package daemonthread

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func TestThreadFollowsAndPlacesTheSessionOnTheLocalDaemon(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)

	follows := d.frames("resume_conversation")
	if len(follows) != 1 || follows[0]["id"] != "conv-1" || follows[0]["project_dir"] != "/proj" {
		t.Fatalf("follow frames = %v, want one resume_conversation for conv-1 in /proj", follows)
	}
	placement := thread.Placement()
	if !placement.Daemon || placement.Local != "laptop" || placement.Where() != "laptop" || placement.Remote() {
		t.Fatalf("placement = %+v, want the session on the local laptop daemon", placement)
	}
	if !thread.Follows("conv-1") || thread.Follows("other") {
		t.Error("the thread follows the wrong conversation")
	}
}

func TestThreadNeverBlocksTheSocketOnUnreadRunFrames(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	conn := d.conn()
	for i := range 200 {
		send(conn, fmt.Sprintf(`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":"%d"}`, i))
	}
	d.answer("list_models", `{"type":"models","models":["m1"]}`)
	if _, err := thread.Request(t.Context(), map[string]string{"type": "list_models"}, "models"); err != nil {
		t.Fatalf("a request after 200 unread run frames: %v", err)
	}
	for i := range 200 {
		frame, err := thread.Next(t.Context())
		if err != nil || !strings.Contains(string(frame), fmt.Sprintf(`"delta":"%d"`, i)) {
			t.Fatalf("frame %d = %s, %v, want the frames in order", i, frame, err)
		}
	}
}

func TestThreadFailsTheRequestARunErrorRefuses(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	d.answer("list_conversations", `{"type":"RUN_ERROR","message":"no worker in that dir"}`)

	_, err := thread.Request(t.Context(), map[string]string{"type": "list_conversations"}, "conversations")
	if err == nil || err.Error() != "no worker in that dir" {
		t.Fatalf("err = %v, want the RUN_ERROR's message", err)
	}
}

func TestThreadAsksTheTUIToFollowARunItDidNotStart(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	send(d.conn(), `{"type":"RUN_STARTED","threadId":"conv-1","runId":"foreign"}`)
	eventually(t, "the follow request", func() bool { return len(notified[tui.FollowRunEvent](notifier)) == 1 })

	release := thread.Consume()
	defer release()
	send(d.conn(), `{"type":"RUN_FINISHED","threadId":"conv-1","runId":"foreign","outcome":{"type":"interrupt","interrupts":[]}}`)
	eventually(t, "the frame to queue", func() bool { return thread.Pending() })
	if got := len(notified[tui.FollowRunEvent](notifier)); got != 1 {
		t.Fatalf("follow requests = %d while a request reads the frames, want 1", got)
	}
}

func TestThreadReconnectsAndFollowsAgain(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	defer thread.Consume()()
	d.binding.Close()
	eventually(t, "the offline placement", func() bool { return thread.Placement().Offline })

	again := startDaemon(t, d.port)
	eventually(t, "the reconnect", func() bool { return !thread.Placement().Offline })
	eventually(t, "the follow on the new daemon", func() bool { return len(again.frames("resume_conversation")) == 1 })

	var markers []string
	for thread.Pending() {
		frame, _ := thread.Next(t.Context())
		markers = append(markers, Marker(frame))
	}
	if strings.Join(markers, ",") != markerOffline+","+markerReconnected {
		t.Errorf("markers = %v, want offline then reconnected", markers)
	}
	placements := notified[tui.SessionPlacementChangedEvent](notifier)
	offline := slices.IndexFunc(placements, func(p tui.SessionPlacementChangedEvent) bool { return p.Placement.Offline })
	if offline < 0 || placements[len(placements)-1].Placement.Offline {
		t.Errorf("placements = %+v, want an offline one and an online one last", placements)
	}
}

func TestThreadLeavesNoMarkersForTheNextTurnAfterAnIdleReconnect(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	d.binding.Close()
	eventually(t, "the offline placement", func() bool { return thread.Placement().Offline })

	again := startDaemon(t, d.port)
	eventually(t, "the follow on the new daemon", func() bool { return len(again.frames("resume_conversation")) == 1 })

	if thread.Pending() {
		frame, _ := thread.Next(t.Context())
		t.Fatalf("an idle reconnect left %s for the next turn to read", frame)
	}
}

func TestThreadTeleportsAndFollowsTheTeleportedSession(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)

	if err := thread.Teleport("k8s-dev", []string{"build/out.bin"}); err != nil {
		t.Fatalf("Teleport: %v", err)
	}
	eventually(t, "the teleport request", func() bool { return len(d.frames("teleport_session")) == 1 })
	request := d.frames("teleport_session")[0]
	if request["target"] != "k8s-dev" || request["id"] != "conv-1" || request["project_dir"] != "/proj" {
		t.Fatalf("teleport request = %v", request)
	}
	if err := thread.Teleport("k8s-dev", nil); err == nil {
		t.Fatal("a second teleport started while one runs")
	}
	id := request["teleport_id"].(string)
	send(d.conn(), `{"type":"teleport_progress","teleport_id":"`+id+`","stage":"checkpointing"}`)
	send(d.conn(), `{"type":"teleport_progress","teleport_id":"other","stage":"failed"}`)
	teleported := `{"type":"session_teleported","session":{"conversation_id":"conv-1"},"environment":"k8s-dev","workspace":"/workspace/proj","cancelled_interrupts":[{"id":"tc1"}]}`
	send(d.conn(), teleported)
	send(d.conn(), teleported)

	eventually(t, "the teleport", func() bool {
		changes := notified[tui.SessionPlacementChangedEvent](notifier)
		return len(changes) > 0 && changes[len(changes)-1].Teleported
	})
	changes := notified[tui.SessionPlacementChangedEvent](notifier)
	last := changes[len(changes)-1]
	if last.Placement.Environment != "k8s-dev" || last.Placement.Workspace != "/workspace/proj" || !last.Placement.Remote() || last.CancelledInterrupts != 1 {
		t.Fatalf("teleported placement = %+v", last)
	}
	progress := notified[tui.TeleportProgressEvent](notifier)
	if len(progress) != 1 || progress[0].Progress.Stage != agentdomain.TeleportCheckpointing {
		t.Fatalf("progress = %+v, want this teleport's one stage", progress)
	}
	eventually(t, "the follow after the teleport", func() bool { return len(d.frames("resume_conversation")) == 2 })
	refollow := d.frames("resume_conversation")[1]
	if refollow["environment"] != "k8s-dev" || refollow["workspace"] != "/workspace/proj" {
		t.Errorf("refollow = %v, want it placed on the environment it teleported to", refollow)
	}
	if teleports := len(d.frames("resume_conversation")); teleports != 2 {
		t.Errorf("follows = %d, the duplicate session_teleported followed again", teleports)
	}
}

func TestThreadTeleportsARemoteSessionOnlyBackHome(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	send(d.conn(), `{"type":"session_teleported","session":{"conversation_id":"conv-1"},"environment":"k8s-dev","workspace":"/w"}`)
	eventually(t, "the teleport", func() bool { return thread.Placement().Remote() })

	err := thread.Teleport("workstation", nil)
	if err == nil || !strings.Contains(err.Error(), "only back to laptop") {
		t.Fatalf("err = %v, want the refusal naming the way back", err)
	}
	if len(d.frames("teleport_session")) != 0 {
		t.Fatal("the refused teleport reached the daemon")
	}
	if err := thread.Teleport("laptop", nil); err != nil {
		t.Fatalf("teleporting back home: %v", err)
	}
	eventually(t, "the teleport back to reach the daemon", func() bool { return len(d.frames("teleport_session")) == 1 })
	if target := d.frames("teleport_session")[0]["target"]; target != "laptop" {
		t.Errorf("target = %v, want laptop", target)
	}
}

func TestThreadFindsTheProjectOfASessionItsDaemonRuns(t *testing.T) {
	d := startDaemon(t, 0)
	leases := storage.NewMemoryStorage()
	_, _, _ = leases.ClaimLease(t.Context(), agentdomain.Lease{ConversationID: "here", Environment: "laptop", Workspace: "/proj/other", Until: time.Now().Add(time.Hour)})
	_, _, _ = leases.ClaimLease(t.Context(), agentdomain.Lease{ConversationID: "away", Environment: "k8s-dev", Workspace: "/workspace/x", Until: time.Now().Add(time.Hour)})
	thread := New(d.config(), Options{ProjectDir: "/proj"}, leases)
	t.Cleanup(thread.Close)
	if err := thread.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	if dir := thread.SessionProject(t.Context(), "here"); dir != "/proj/other" {
		t.Errorf("SessionProject(here) = %q, want its own project", dir)
	}
	for _, id := range []string{"away", "unknown"} {
		if dir := thread.SessionProject(t.Context(), id); dir != "" {
			t.Errorf("SessionProject(%s) = %q, want none", id, dir)
		}
	}

	thread.MoveTo("/proj/other")
	if err := thread.Follow(t.Context(), "here", sessionsdomain.ThreadOptions{}); err != nil {
		t.Fatal(err)
	}
	if follow := d.frames("resume_conversation"); follow[0]["project_dir"] != "/proj/other" {
		t.Errorf("followed in %v, want the session's own project", follow[0]["project_dir"])
	}
}

func TestThreadOwesAFollowForAPromptReplayedBeforeAnyTurnReads(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	if thread.Unfollowed() {
		t.Fatal("a thread at rest owes a follow")
	}

	send(d.conn(), `{"type":"RUN_FINISHED","threadId":"conv-1","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"plan-1","reason":"plan_approval"}]}}`)
	eventually(t, "the replayed prompt", func() bool { return thread.Unfollowed() })

	release := thread.Consume()
	defer release()
	if thread.Unfollowed() {
		t.Error("the follow is still owed once a turn reads the thread")
	}
}

func TestThreadAsksToFollowARunThatStartedWhileATurnFinished(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	release := thread.Consume()
	send(d.conn(), `{"type":"RUN_STARTED","threadId":"conv-2","runId":"r2"}`)
	eventually(t, "the next run's start", func() bool { return thread.Pending() })
	if len(notified[tui.FollowRunEvent](notifier)) != 0 {
		t.Fatal("asked to follow while a turn read the thread")
	}

	release()

	eventually(t, "the request to follow the next run", func() bool { return len(notified[tui.FollowRunEvent](notifier)) == 1 })
}

func TestThreadKeepsATurnReadingWhenTheTurnBeforeReleasesLate(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)
	late := thread.Consume()
	release := thread.Consume()
	defer release()

	late()
	send(d.conn(), `{"type":"RUN_STARTED","threadId":"conv-2","runId":"r2"}`)

	eventually(t, "the run's start", func() bool { return thread.Pending() })
	if len(notified[tui.FollowRunEvent](notifier)) != 0 {
		t.Error("asked to follow the run a turn reads")
	}
}
