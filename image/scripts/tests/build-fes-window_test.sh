#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-window-build.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

# A stand-in kernel tree. Like Linux, an O= build runs make again in the output
# directory, and kernelrelease appends the localversion file found there.
kernel=$fixture/kernel
mkdir "$kernel"
cat >"$kernel/Makefile" <<'EOF'
ifneq ($(sub_make_done),1)
export sub_make_done := 1
$(MAKECMDGOALS):
	@$(MAKE) -C $(O) -f $(CURDIR)/Makefile $@
else
MiSTer_defconfig modules_prepare:
	@:
kernelrelease:
	@echo 5.15.1$$(cat localversion.mister)
modules:
	@echo $(FES_WINDOW_VERSION) >$(M)/fes_window.ko
endif
EOF
printf '%s\n' '#!/bin/sh' >"$fixture/arm-strip"
chmod 0755 "$fixture/arm-strip"

# Buildroot runs its post-build scripts with its own MAKEFLAGS, which carry w.
build_window() {
  MAKEFLAGS=w MAKELEVEL=1 sh "$repo/scripts/build-fes-window.sh" \
    "$kernel" "$fixture/build" "$fixture/output" "$fixture/arm-" "$fixture/host" "$1"
}

build_window 5.15.1-MiSTer
grep -Eqx '[0-9a-f]{16}' "$fixture/output/version"
cmp "$fixture/output/version" "$fixture/output/fes_window.ko"

if message=$(build_window 5.15.1 2>&1); then
  echo 'build-fes-window accepted the wrong kernel release' >&2
  exit 1
fi
[ "$message" = 'build-fes-window: kernel release is 5.15.1-MiSTer, expected 5.15.1' ] || {
  printf '%s\n' 'build-fes-window failed for the wrong reason:' "$message" >&2
  exit 1
}
