#!/bin/bash
set -euo pipefail

readonly USER_DATA=/var/lib/cloud/seed/nocloud/user-data
readonly USER_DATA_LOG=/var/log/cloud-init-output.log
readonly DAEMON_USER=infer
readonly DAEMON_HOME=/home/infer
readonly SCREEN=1280x800
readonly LOGIN_MANAGER=org.freedesktop.login1

run_user_data() {
  if [ "$(head -c 2 "$USER_DATA")" != '#!' ]; then
    echo "user-data must be a script starting with #!, cloud-config is not supported" >&2
    exit 1
  fi
  local script
  script=$(mktemp)
  cp "$USER_DATA" "$script"
  chmod 0700 "$script"
  "$script" 2>&1 | tee -a "$USER_DATA_LOG"
  rm -f "$script"
}

answers_on_bus() {
  gdbus call "--$1" --dest org.freedesktop.DBus --object-path /org/freedesktop/DBus \
    --method org.freedesktop.DBus.NameHasOwner "$2" 2>/dev/null | grep -q true
}

start_login_manager() {
  install -d /run/dbus
  dbus-daemon --system --fork
  python3 -m dbusmock --system --template logind >/dev/null 2>&1 &
  until answers_on_bus system "$LOGIN_MANAGER"; do sleep 0.1; done
}

boot_as_root() {
  if [ -r "$USER_DATA" ]; then
    run_user_data
  fi
  start_login_manager
  install -d -m 0700 -o "$DAEMON_USER" -g "$DAEMON_USER" "$XDG_RUNTIME_DIR"
  HOME=$DAEMON_HOME exec setpriv --reuid="$DAEMON_USER" --regid="$DAEMON_USER" --init-groups "$0" "$@"
}

start_desktop() {
  install -d "$HOME/Desktop"
  export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
  dbus-daemon --session --address="$DBUS_SESSION_BUS_ADDRESS" --fork
  pipewire >/dev/null 2>&1 &
  wireplumber >/dev/null 2>&1 &
  gnome-shell --headless --wayland --virtual-monitor "$SCREEN" >/dev/null 2>&1 &
  until answers_on_bus session org.gnome.Shell.Screenshot; do sleep 0.1; done
}

if [ "$(id -u)" = 0 ]; then
  boot_as_root "$@"
fi
start_desktop
exec infer "$@"
