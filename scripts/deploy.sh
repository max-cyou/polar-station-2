#!/bin/sh
set -eu

target=${1:?Usage: ./scripts/deploy.sh root@station-ip [ssh-port]}
port=${2:-2222}
cd "$(dirname "$0")/.."

npm run build:station
ssh -p "$port" "$target" 'mkdir -p /data/opt/polar && cat > /data/opt/polar/polar.new && chmod 755 /data/opt/polar/polar.new' < bin/polar

expected=$(sha256sum bin/polar | cut -d ' ' -f 1)
actual=$(ssh -p "$port" "$target" 'sha256sum /data/opt/polar/polar.new' | cut -d ' ' -f 1)
[ "$expected" = "$actual" ] || { echo 'Upload checksum mismatch' >&2; exit 1; }

ssh -p "$port" "$target" 'sh -s' <<'REMOTE'
set -eu
base=/data/opt/polar
if [ -f "$base/polar.pid" ]; then
  pid=$(cat "$base/polar.pid")
  case "$pid" in ''|*[!0-9]*) echo 'Invalid Polar PID' >&2; exit 1;; esac
  if kill -0 "$pid" 2>/dev/null; then
    executable=$(readlink -f "$base/polar")
    case "$(readlink "/proc/$pid/exe")" in
      "$executable"|"$executable (deleted)") kill "$pid" ;;
      *) echo 'PID belongs to another process; refusing to stop it' >&2; exit 1 ;;
    esac
    tries=0
    while kill -0 "$pid" 2>/dev/null; do
      tries=$((tries + 1))
      [ "$tries" -lt 30 ] || { echo 'Polar did not stop' >&2; exit 1; }
      sleep 0.1
    done
  fi
fi
mv "$base/polar.new" "$base/polar"
nohup "$base/polar" -listen :8080 </dev/null >"$base/polar.log" 2>&1 &
pid=$!
echo "$pid" > "$base/polar.pid"
sleep 1
if ! kill -0 "$pid" 2>/dev/null; then
  cat "$base/polar.log" >&2
  exit 1
fi
echo "Polar started on port 8080 (PID $pid)"
REMOTE
