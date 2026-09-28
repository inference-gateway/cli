#!/usr/bin/env python3
import json
import sys

args = json.load(sys.stdin)
try:
    with open(args["path"], encoding="utf-8") as f:
        text = f.read()
except OSError as err:
    sys.exit(f"cannot read {args['path']}: {err.strerror}")

print(f"{args['path']}: {text.count(chr(10))} lines, {len(text.split())} words, {len(text)} characters")
