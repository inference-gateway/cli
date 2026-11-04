#!/bin/sh
set -e

Xvfb "$DISPLAY" -screen 0 1280x800x24 -nolisten tcp >/dev/null 2>&1 &
until [ -e "/tmp/.X11-unix/X${DISPLAY#:}" ]; do sleep 0.1; done

chromium --no-sandbox --disable-dev-shm-usage --disable-gpu --no-first-run --no-default-browser-check \
  --password-store=basic --window-position=0,0 --window-size=1280,800 about:blank >/dev/null 2>&1 &

mkdir -p /home/infer/.infer/tls
if [ ! -f /home/infer/.infer/tls/server.crt ]; then
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 365 \
    -subj /CN=infer-desktop -addext "subjectAltName=IP:127.0.0.1,DNS:localhost,DNS:desktop" \
    -keyout /home/infer/.infer/tls/server.key -out /home/infer/.infer/tls/server.crt 2>/dev/null
fi

exec infer daemon "$@"
