package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	eventRunStarted         = "RUN_STARTED"
	eventTextMessageContent = "TEXT_MESSAGE_CONTENT"
	eventToolCallStart      = "TOOL_CALL_START"
	eventToolCallResult     = "TOOL_CALL_RESULT"
	eventRunFinished        = "RUN_FINISHED"
	eventRunError           = "RUN_ERROR"

	interrupt      = "interrupt"
	maxResultChars = 160
	sessionID      = "ag-ui-serve-demo"
)

var errUsage = errors.New("usage: go run . stream | serve [--approval]")

var errWorker = errors.New("the worker errored")

var errSuspend = errors.New("expected the run to suspend on the Write approval")

var errNoInterrupt = errors.New("expected an interrupt in the run outcome")

var errClosedEarly = errors.New("stdout closed before the run finished")

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	if errors.Is(err, errUsage) {
		os.Exit(2)
	}
	os.Exit(1)
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "stream":
		return stream(os.Stdin)
	case len(args) == 1 && args[0] == "serve":
		return serve(false)
	case len(args) == 2 && args[0] == "serve" && args[1] == "--approval":
		return serve(true)
	}
	return errUsage
}

func parseEvent(line string) (map[string]any, bool) {
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil || event == nil {
		return nil, false
	}
	if _, ok := event["type"].(string); !ok {
		return nil, false
	}
	return event, true
}

func describe(line string) (string, bool) {
	event, ok := parseEvent(line)
	if !ok {
		return "", false
	}
	kind, _ := event["type"].(string)
	switch kind {
	case eventRunStarted:
		return fmt.Sprintf("%v threadId=%v runId=%v", kind, event["threadId"], event["runId"]), true
	case eventTextMessageContent:
		return fmt.Sprintf("%v %v", kind, event["delta"]), true
	case eventToolCallStart:
		return fmt.Sprintf("%v %v %v", kind, event["toolCallId"], event["toolCallName"]), true
	case eventToolCallResult:
		return fmt.Sprintf("%v %v %v", kind, event["toolCallId"], truncateResult(event["content"])), true
	case eventRunFinished:
		return fmt.Sprintf("%v %v", kind, outcomeType(event)), true
	case eventRunError:
		return fmt.Sprintf("%v %v", kind, event["message"]), true
	}
	return "", false
}

func truncateResult(content any) string {
	runes := []rune(fmt.Sprintf("%v", content))
	if len(runes) > maxResultChars {
		return string(runes[:maxResultChars]) + " ..."
	}
	return string(runes)
}

func outcomeType(event map[string]any) any {
	outcome, ok := event["outcome"].(map[string]any)
	if !ok {
		return event["outcome"]
	}
	return outcome["type"]
}

func stream(in io.Reader) error {
	scanner := newEventScanner(in)
	for scanner.Scan() {
		if text, ok := describe(scanner.Text()); ok {
			fmt.Println(text)
		}
	}
	return scanner.Err()
}

func newEventScanner(in io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return scanner
}

func startWorker(approval bool) (*exec.Cmd, io.WriteCloser, *bufio.Scanner, error) {
	argv := []string{"infer"}
	if given := strings.Fields(os.Getenv("INFER")); len(given) > 0 {
		argv = given
	}
	argv = append(argv, "headless", "--serve", "--session-id", sessionID)
	if approval {
		argv = append(argv, "--require-approval")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdin, newEventScanner(stdout), nil
}

func runAgentFrame(content, messageID string) map[string]any {
	return map[string]any{
		"type": "run_agent_input",
		"input": map[string]any{
			"messages": []any{map[string]any{"id": messageID, "role": "user", "content": content}},
		},
	}
}

func resumeFrame(interruptID string) map[string]any {
	return map[string]any{
		"type": "run_agent_input",
		"input": map[string]any{
			"resume": []any{map[string]any{"interruptId": interruptID, "status": "resolved"}},
		},
	}
}

func send(stdin io.WriteCloser, frame map[string]any) error {
	encoded, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	_, err = stdin.Write(append(encoded, '\n'))
	return err
}

func serve(approval bool) error {
	cmd, stdin, stdout, err := startWorker(approval)
	if err != nil {
		return err
	}
	first := runAgentFrame("say something", "serve-demo-1")
	if approval {
		first = runAgentFrame("write the marker file ag-ui-demo.txt", "approval-demo-1")
	}
	if err := send(stdin, first); err != nil {
		return kill(cmd, err)
	}

	runs, resumed := 0, false
	for stdout.Scan() {
		line := stdout.Text()
		if text, ok := describe(line); ok {
			fmt.Println(text)
		}
		event, ok := parseEvent(line)
		if !ok {
			continue
		}
		switch event["type"] {
		case eventRunError:
			return kill(cmd, errWorker)
		case eventRunFinished:
			runs++
			again, err := continueAfterRun(stdin, event, approval, &resumed, runs)
			if err != nil {
				return kill(cmd, err)
			}
			if !again {
				return stopWorker(cmd, stdin)
			}
		}
	}

	if err := stdout.Err(); err != nil {
		return kill(cmd, err)
	}
	return kill(cmd, errClosedEarly)
}

func continueAfterRun(stdin io.WriteCloser, event map[string]any, approval bool, resumed *bool, runs int) (bool, error) {
	if approval {
		if *resumed {
			return false, nil
		}
		if err := answerInterrupt(stdin, event); err != nil {
			return false, err
		}
		*resumed = true
		return true, nil
	}
	if runs < 2 {
		fmt.Println("-- next frame --")
		return true, send(stdin, runAgentFrame("another greeting please", "serve-demo-2"))
	}
	return false, nil
}

func answerInterrupt(stdin io.WriteCloser, event map[string]any) error {
	outcome, ok := event["outcome"].(map[string]any)
	if !ok || outcome["type"] != interrupt {
		return errSuspend
	}
	entry, ok := firstInterrupt(outcome)
	if !ok {
		return errNoInterrupt
	}
	metadata, _ := entry["metadata"].(map[string]any)
	fmt.Printf("approval requested: %v (interrupt %v), answering resolved\n", metadata["toolName"], entry["id"])
	return send(stdin, resumeFrame(fmt.Sprintf("%v", entry["id"])))
}

func firstInterrupt(outcome map[string]any) (map[string]any, bool) {
	interrupts, ok := outcome["interrupts"].([]any)
	if !ok || len(interrupts) == 0 {
		return nil, false
	}
	entry, _ := interrupts[0].(map[string]any)
	return entry, entry != nil
}

func stopWorker(cmd *exec.Cmd, stdin io.WriteCloser) error {
	if err := stdin.Close(); err != nil {
		return kill(cmd, err)
	}
	return cmd.Wait()
}

func kill(cmd *exec.Cmd, err error) error {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return err
}
