#!/bin/sh
set -e

if [ ! -e "/tmp/.X11-unix/X${DISPLAY#:}" ]; then
  Xvfb "$DISPLAY" -screen 0 1280x800x24 -nolisten tcp >/dev/null 2>&1 &
  until [ -e "/tmp/.X11-unix/X${DISPLAY#:}" ]; do sleep 0.1; done

  chromium --no-sandbox --disable-dev-shm-usage --disable-gpu --no-first-run --no-default-browser-check \
    --password-store=basic --window-position=0,0 --window-size=1280,800 about:blank >/dev/null 2>&1 &
fi

exec /usr/local/bin/infer "$@"
