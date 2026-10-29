package daemonthread

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
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

func TestThreadTransfersAndFollowsTheMovedSession(t *testing.T) {
	d := startDaemon(t, 0)
	thread, notifier := connected(t, d)

	if err := thread.Transfer("k8s-dev", []string{"build/out.bin"}); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	eventually(t, "the transfer request", func() bool { return len(d.frames("transfer_session")) == 1 })
	request := d.frames("transfer_session")[0]
	if request["target"] != "k8s-dev" || request["id"] != "conv-1" || request["project_dir"] != "/proj" {
		t.Fatalf("transfer request = %v", request)
	}
	if err := thread.Transfer("k8s-dev", nil); err == nil {
		t.Fatal("a second transfer started while one runs")
	}
	id := request["transfer_id"].(string)
	send(d.conn(), `{"type":"transfer_progress","transfer_id":"`+id+`","stage":"checkpointing"}`)
	send(d.conn(), `{"type":"transfer_progress","transfer_id":"other","stage":"failed"}`)
	moved := `{"type":"session_moved","session":{"conversation_id":"conv-1"},"environment":"k8s-dev","workspace":"/workspace/proj","cancelled_interrupts":[{"id":"tc1"}]}`
	send(d.conn(), moved)
	send(d.conn(), moved)

	eventually(t, "the move", func() bool {
		changes := notified[tui.SessionPlacementChangedEvent](notifier)
		return len(changes) > 0 && changes[len(changes)-1].Moved
	})
	changes := notified[tui.SessionPlacementChangedEvent](notifier)
	last := changes[len(changes)-1]
	if last.Placement.Environment != "k8s-dev" || last.Placement.Workspace != "/workspace/proj" || !last.Placement.Remote() || last.CancelledInterrupts != 1 {
		t.Fatalf("moved placement = %+v", last)
	}
	progress := notified[tui.TransferProgressEvent](notifier)
	if len(progress) != 1 || progress[0].Progress.Stage != agentdomain.TransferCheckpointing {
		t.Fatalf("progress = %+v, want this transfer's one stage", progress)
	}
	eventually(t, "the follow after the move", func() bool { return len(d.frames("resume_conversation")) == 2 })
	refollow := d.frames("resume_conversation")[1]
	if refollow["environment"] != "k8s-dev" || refollow["workspace"] != "/workspace/proj" {
		t.Errorf("refollow = %v, want it placed on the environment it moved to", refollow)
	}
	if moves := len(d.frames("resume_conversation")); moves != 2 {
		t.Errorf("follows = %d, the duplicate session_moved followed again", moves)
	}
}

func TestThreadMovesARemoteSessionOnlyBackHome(t *testing.T) {
	d := startDaemon(t, 0)
	thread, _ := connected(t, d)
	send(d.conn(), `{"type":"session_moved","session":{"conversation_id":"conv-1"},"environment":"k8s-dev","workspace":"/w"}`)
	eventually(t, "the move", func() bool { return thread.Placement().Remote() })

	err := thread.Transfer("workstation", nil)
	if err == nil || !strings.Contains(err.Error(), "only back to laptop") {
		t.Fatalf("err = %v, want the refusal naming the way back", err)
	}
	if len(d.frames("transfer_session")) != 0 {
		t.Fatal("the refused transfer reached the daemon")
	}
	if err := thread.Transfer("laptop", nil); err != nil {
		t.Fatalf("moving back home: %v", err)
	}
	eventually(t, "the move back to reach the daemon", func() bool { return len(d.frames("transfer_session")) == 1 })
	if target := d.frames("transfer_session")[0]["target"]; target != "laptop" {
		t.Errorf("target = %v, want laptop", target)
	}
}
