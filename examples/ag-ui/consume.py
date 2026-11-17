#!/usr/bin/env python3
"""Consume infer's AG-UI output.

`stream` reads one run of `infer headless --format ag-ui` from stdin and prints
its events by type. `serve` spawns `infer headless --serve`, writes
run_agent_input frames to its stdin and prints one AG-UI run per frame; with
--approval it answers one tool approval over stdin and watches the resumed run.
"""

import json
import os
import subprocess
import sys

MAX_RESULT_CHARS = 160


def describe(line):
    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        return None
    kind = event.get("type", "")
    if kind == "RUN_STARTED":
        return "RUN_STARTED threadId={} runId={}".format(event.get("threadId"), event.get("runId"))
    if kind == "TEXT_MESSAGE_CONTENT":
        return "TEXT_MESSAGE_CONTENT {}".format(event.get("delta", ""))
    if kind == "TOOL_CALL_START":
        return "TOOL_CALL_START {} {}".format(event.get("toolCallId"), event.get("toolCallName"))
    if kind == "TOOL_CALL_RESULT":
        content = str(event.get("content", ""))
        if len(content) > MAX_RESULT_CHARS:
            content = content[:MAX_RESULT_CHARS] + " ..."
        return "TOOL_CALL_RESULT {} {}".format(event.get("toolCallId"), content)
    if kind == "RUN_FINISHED":
        outcome = event.get("outcome") or {}
        return "RUN_FINISHED {}".format(outcome.get("type", "") if isinstance(outcome, dict) else outcome)
    if kind == "RUN_ERROR":
        return "RUN_ERROR {}".format(event.get("message", ""))
    return None


def stream():
    for line in sys.stdin:
        text = describe(line.strip())
        if text:
            print(text, flush=True)
    return 0


def run_agent_frame(content, message_id):
    return {"type": "run_agent_input", "input": {"messages": [{"id": message_id, "role": "user", "content": content}]}}


def resume_frame(interrupt_id):
    return {"type": "run_agent_input", "input": {"resume": [{"interruptId": interrupt_id, "status": "resolved"}]}}


def send(worker, frame):
    worker.stdin.write(json.dumps(frame) + "\n")
    worker.stdin.flush()


def serve(approval):
    argv = os.environ.get("INFER", "infer").split() + ["headless", "--serve", "--session-id", "ag-ui-serve-demo"]
    if approval:
        argv.append("--require-approval")
    worker = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=sys.stderr, text=True)

    if approval:
        send(worker, run_agent_frame("write the marker file ag-ui-demo.txt", "approval-demo-1"))
    else:
        send(worker, run_agent_frame("say something", "serve-demo-1"))

    runs = 0
    finished_a_run = False
    resumed = False
    for line in worker.stdout:
        text = describe(line.strip())
        if not text:
            continue
        print(text, flush=True)

        event = json.loads(line)
        kind = event.get("type", "")
        if kind not in ("RUN_FINISHED", "RUN_ERROR"):
            continue
        runs += 1
        finished_a_run = True
        if kind == "RUN_ERROR":
            return die(worker, "the worker errored")

        if approval:
            if not resumed:
                interrupt = event.get("outcome", {}).get("interrupts", [{}])[0]
                if event.get("outcome", {}).get("type") != "interrupt":
                    return die(worker, "expected the run to suspend on the Write approval")
                print("approval requested: {} (interrupt {}), answering resolved".format(
                    interrupt.get("metadata", {}).get("toolName"), interrupt.get("id")), flush=True)
                send(worker, resume_frame(interrupt.get("id")))
                resumed = True
                finished_a_run = False
                continue
            break
        if runs < 2:
            print("-- next frame --", flush=True)
            send(worker, run_agent_frame("another greeting please", "serve-demo-2"))
            finished_a_run = False
            continue
        break

    if not finished_a_run:
        return die(worker, "stdout closed before the run finished")
    worker.stdin.close()
    worker.wait()
    return 0


def die(worker, message):
    worker.kill()
    sys.stderr.write(message + "\n")
    return 1


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "serve":
        return serve("--approval" in sys.argv)
    if len(sys.argv) > 1 and sys.argv[1] == "stream":
        return stream()
    sys.stderr.write("usage: consume.py stream | serve [--approval]\n")
    return 2


if __name__ == "__main__":
    sys.exit(main())
