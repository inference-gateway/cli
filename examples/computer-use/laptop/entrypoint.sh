#!/bin/sh
set -e

mkdir -p /home/infer/.infer/tls /home/infer/project
if [ ! -f /home/infer/.infer/tls/server.crt ]; then
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 365 \
    -subj /CN=infer-laptop -addext "subjectAltName=IP:127.0.0.1,DNS:localhost,DNS:laptop" \
    -keyout /home/infer/.infer/tls/server.key -out /home/infer/.infer/tls/server.crt 2>/dev/null
fi
cp /opt/infer-laptop/daemon.yaml /home/infer/.infer/daemon.yaml

cd /home/infer/project
if [ ! -d .git ]; then
  git init --quiet -b main
  git config user.email demo@example.com
  git config user.name demo
  echo "# teleport me to the desktop" > README.md
  git add README.md
  git commit --quiet -m "init"
fi

until [ -f /desktop/.infer/tls/server.crt ]; do
  echo "waiting for the desktop daemon's certificate..."
  sleep 2
done

exec infer daemon "$@"
