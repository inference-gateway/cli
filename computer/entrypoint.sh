#!/bin/bash
set -euo pipefail

readonly USER_DATA=/var/lib/cloud/seed/nocloud/user-data
readonly USER_DATA_LOG=/var/log/cloud-init-output.log
readonly DAEMON_USER=infer
readonly DAEMON_HOME=/home/infer
readonly SCREEN=1280x800x24
readonly WALLPAPER=#2e3440
readonly TASKBAR_CONFIG=/etc/xdg/tint2/computer.tint2rc
readonly APPLICATIONS=/usr/share/applications

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

boot_as_root() {
  if [ -r "$USER_DATA" ]; then
    run_user_data
  fi
  HOME=$DAEMON_HOME exec setpriv --reuid="$DAEMON_USER" --regid="$DAEMON_USER" --init-groups "$0" "$@"
}

launchers_for_installed_apps() {
  grep -L '^NoDisplay=true' "$APPLICATIONS"/*.desktop | sed 's/^/launcher_item_app = /'
}

start_taskbar() {
  local config
  config=$(mktemp --suffix=.tint2rc)
  { cat "$TASKBAR_CONFIG"; launchers_for_installed_apps; } >"$config"
  tint2 -c "$config" >/dev/null 2>&1 &
}

start_desktop() {
  Xvfb "$DISPLAY" -screen 0 "$SCREEN" -nolisten tcp >/dev/null 2>&1 &
  until [ -e "/tmp/.X11-unix/X${DISPLAY#:}" ]; do sleep 0.1; done
  xsetroot -solid "$WALLPAPER"
  openbox-session >/dev/null 2>&1 &
  start_taskbar
}

if [ "$(id -u)" = 0 ]; then
  boot_as_root "$@"
fi
start_desktop
exec infer "$@"
