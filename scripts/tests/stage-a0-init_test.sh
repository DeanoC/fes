#!/bin/sh
set -eu

# shellcheck disable=SC1007 # Required POSIX empty-CDPATH command environment.
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/stage-a0-init.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
args="$tmp/args"
tool="$tmp/tool"
destination="$tmp/destination
with-newline"
cat >"$tool" <<'EOF'
#!/bin/sh
set -eu
: "${STAGE_A0_ARGS:?}"
for arg do printf '%s\0' "$arg" >>"$STAGE_A0_ARGS"; done
EOF
chmod +x "$tool"
STAGE_A0_ARGS="$args" STAGE_A0_TOOL="$tool" "$root/scripts/stage-a0-init-main.sh" --bootstrap fixtures/bootstrap.toml --destination "$destination"
printf 'init-main\0--bootstrap\0fixtures/bootstrap.toml\0--destination\0%s\0' "$destination" >"$tmp/expected"
cmp "$tmp/expected" "$args"
if rg -n -i 'push|ssh|curl|wget|credential|token|password|/dev/|target' "$root/scripts/stage-a0-init-main.sh"; then
	echo 'wrapper has a prohibited operation' >&2
	exit 1
fi
if rg -n 'eval|\$\*' "$root/scripts/stage-a0-init-main.sh"; then
	echo 'wrapper reconstructs arguments' >&2
	exit 1
fi
