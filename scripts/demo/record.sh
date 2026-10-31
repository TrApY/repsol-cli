#!/bin/sh
# Records the README demo GIF with vhs against the in-process fake backend.
# Every value on screen is synthetic; nothing reaches Repsol.
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
pid=
cleanup() {
	if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; fi
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

cd "$root"
mkdir -p "$work/bin" "$work/home" "$work/config" images/demos
go build -trimpath -ldflags "-X github.com/felipe9962/repsol-cli/internal/cli.Version=demo" \
	-o "$work/bin/repsol" ./cmd/repsol
go build -o "$work/fakebackend" ./scripts/demo/fakebackend

"$work/fakebackend" >"$work/fake.env" &
pid=$!
tries=0
until grep -q REPSOL_GMID "$work/fake.env" 2>/dev/null; do
	tries=$((tries + 1))
	if [ "$tries" -gt 50 ]; then
		echo "record.sh: the fake backend did not start" >&2
		exit 1
	fi
	sleep 0.1
done

# The demo shell gets its own HOME and config directory, so the recording
# never reads or writes the real ones. vhs itself keeps the real HOME.
{
	cat "$work/fake.env"
	echo "export PATH=\"$work/bin:\$PATH\""
	echo "export HOME=\"$work/home\" XDG_CONFIG_HOME=\"$work/config\""
	echo "export PS1='\$ '"
} >"$work/demo.env"

DEMO_ENV="$work/demo.env" vhs scripts/demo/order.tape
