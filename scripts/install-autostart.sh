#!/bin/sh
set -eu

target=${1:?Usage: sh scripts/install-autostart.sh root@station-ip [ssh-port]}
port=${2:-2222}
case "$port" in ''|*[!0-9]*) echo 'Invalid SSH port' >&2; exit 1 ;; esac
cd "$(dirname "$0")/.."

# Upload over SSH: the station does not need scp or an SFTP server.
ssh -p "$port" "$target" 'set -eu; test "$(id -u)" = 0; test -x /data/opt/polar/polar; mkdir -p /data/opt/polar; cat > /data/opt/polar/polar.init.new' < scripts/polar.init

expected=$(sha256sum scripts/polar.init | cut -d ' ' -f 1)
actual=$(ssh -p "$port" "$target" 'sha256sum /data/opt/polar/polar.init.new' | cut -d ' ' -f 1)
[ "$expected" = "$actual" ] || { echo 'Upload checksum mismatch' >&2; exit 1; }

ssh -p "$port" "$target" 'sh -s' <<'REMOTE'
set -eu
base=/data/opt/polar
service=/etc/init.d/S99zzpolar
temporary=/etc/init.d/.polar-autostart.$$
restore_ro=0
locked=0

cleanup() {
  result=$?
  trap - EXIT HUP INT TERM
  if [ "$locked" = 1 ]; then
    rm -f "$temporary"
    if [ "$restore_ro" = 1 ]; then
      sync
      if ! mount -o remount,ro /; then
        echo 'Could not restore read-only root filesystem' >&2
        result=1
      fi
    fi
    rmdir "$base/.autostart-install.lock" || result=1
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

[ "$(id -u)" = 0 ] || { echo 'Root access is required' >&2; exit 1; }
[ -x "$base/polar" ] || { echo 'Install Polar first' >&2; exit 1; }
[ -f /etc/init.d/rcS ] && [ -f /etc/init.d/rcK ] || {
  echo 'Unsupported init system: expected BusyBox rcS/rcK' >&2
  exit 1
}
mkdir "$base/.autostart-install.lock" || { echo 'Autostart installation is already running' >&2; exit 1; }
locked=1

backup=$(mktemp -d "$base/backup-autostart-$(date +%Y%m%d-%H%M%S)-XXXXXX")
if [ -e "$service" ] || [ -L "$service" ]; then
  cp -pP "$service" "$backup/S99zzpolar"
else
  echo 'No previous Polar init script' > "$backup/not-installed"
fi
echo "Autostart backup: $backup"

readonly=$(awk '$2 == "/" && $4 ~ /(^|,)ro(,|$)/ { print "yes" }' /proc/mounts)
if [ "$readonly" = yes ]; then
  mount -o remount,rw /
  restore_ro=1
fi
cp "$base/polar.init.new" "$temporary"
chmod 755 "$temporary"
mv -f "$temporary" "$service"
sync
if [ "$restore_ro" = 1 ]; then
  mount -o remount,ro /
  restore_ro=0
fi
rm -f "$base/polar.init.new"

# Adopt an existing instance, or start Polar if it is not running.
"$service" start
echo "Polar autostart installed: $service"
REMOTE
