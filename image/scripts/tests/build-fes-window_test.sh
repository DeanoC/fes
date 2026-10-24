#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-window-build.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

# A stand-in kernel tree. Like Linux, an O= build runs make again in the output
# directory, and kernelrelease appends the localversion file found there.
# Preparation builds the GCC plugins, whose headers include gmp.h. Like the
# debug information its build ID hashes, the module records the kernel, build
# and SDK paths unless KCFLAGS maps them.
kernel=$fixture/kernel
mkdir "$kernel"
cat >"$kernel/Makefile" <<'EOF'
ifneq ($(sub_make_done),1)
export sub_make_done := 1
$(MAKECMDGOALS):
	@$(MAKE) -C $(O) -f $(CURDIR)/Makefile $@
else
MiSTer_defconfig:
	@:
kernelrelease:
	@echo 5.15.1$$(cat localversion.mister)
modules_prepare:
	@test -f $(patsubst -I%,%,$(HOSTCXXFLAGS))/gmp.h || { echo 'gmp.h: No such file or directory' >&2; exit 1; }
modules:
	@{ echo $(FES_WINDOW_VERSION); \
	  for path in $(patsubst %/Makefile,%,$(firstword $(MAKEFILE_LIST))) $(CURDIR) $(M) \
	      $(patsubst -I%/include,%,$(HOSTCXXFLAGS)); do \
	    for map in $(patsubst -fdebug-prefix-map=%,%,$(filter -fdebug-prefix-map=%,$(KCFLAGS))); do \
	      case $$path in "$${map%%=*}"*) path=$${map#*=}$${path#"$${map%%=*}"} ;; esac; \
	    done; \
	    echo $$path; \
	  done; } >$(M)/fes_window.ko
endif
EOF
printf '%s\n' '#!/bin/sh' >"$fixture/arm-strip"
chmod 0755 "$fixture/arm-strip"
mkdir -p "$fixture/host/include"
: >"$fixture/host/include/gmp.h"

# Buildroot runs its post-build scripts with its own MAKEFLAGS, which carry w.
build_window() {
  MAKEFLAGS=w MAKELEVEL=1 sh "$repo/scripts/build-fes-window.sh" \
    "$kernel" "$fixture/build" "$fixture/output" "$fixture/arm-" "$fixture/host" "$1"
}

build_window 5.15.1-MiSTer
grep -Eqx '[0-9a-f]{16}' "$fixture/output/version"
[ "$(head -n 1 "$fixture/output/fes_window.ko")" = "$(cat "$fixture/output/version")" ]

if message=$(build_window 5.15.1 2>&1); then
  echo 'build-fes-window accepted the wrong kernel release' >&2
  exit 1
fi
[ "$message" = 'build-fes-window: kernel release is 5.15.1-MiSTer, expected 5.15.1' ] || {
  printf '%s\n' 'build-fes-window failed for the wrong reason:' "$message" >&2
  exit 1
}

# The factory post-build builds the driver with an unrelocated copy of the
# cached SDK. Buildroot's HOST_DIR has no gmp.h. Stand-ins replace the package
# installer, the bundle assembler and the SDK cache, which writes an SDK host.
image=$fixture/image
board=$image/buildroot/board/fogcast-target
mkdir -p "$board" "$image/scripts" "$image/kernel" "$image/build/cache/target-image"
cp "$repo/buildroot/board/fogcast-target/native-post-build.sh" "$board/"
cp "$repo/scripts/build-fes-window.sh" "$image/scripts/"
cp -R "$repo/kernel/fes-window" "$image/kernel/"
cat >"$image/scripts/native-extra-cores.sh" <<'EOF'
#!/bin/sh
if [ "$1" = count ]; then echo 1; fi
EOF
chmod 0755 "$image/scripts/native-extra-cores.sh"
cat >"$image/scripts/toolchain_cache.py" <<'EOF'
import pathlib
import sys
assert sys.argv[1] == 'extract'
host = pathlib.Path(sys.argv[2], 'host')
(host / 'include').mkdir(parents=True)
(host / 'include/gmp.h').touch()
(host / 'bin').mkdir()
(host / 'bin/arm-buildroot-linux-gnueabihf-strip').write_text('#!/bin/sh\n')
(host / 'bin/arm-buildroot-linux-gnueabihf-strip').chmod(0o755)
EOF
cat >"$image/scripts/kit-software.py" <<'EOF'
import shutil
import sys
shutil.copytree(sys.argv[sys.argv.index('--window-driver') + 1],
                sys.argv[sys.argv.index('--target') + 1] + '/window-driver')
EOF
git -C "$kernel" init -q
git -C "$kernel" add Makefile
git -C "$kernel" -c user.name=fixture -c user.email=fixture@example.invalid \
  -c commit.gpgsign=false commit -q --no-verify -m kernel
git clone -q --bare "$kernel" "$image/build/cache/target-image/linux-kernel.git"
printf '[kernel]\ncommit = "%s"\nrelease = "5.15.1-MiSTer"\n' \
  "$(git -C "$kernel" rev-parse HEAD)" >"$image/build/target-image.sources.lock.toml"

fogcast=$fixture/fogcast
mkdir -p "$fogcast/bin"
for tool in fogcast-kit fogcast-tenfoot fes-kit-data fes-kit-software fes-bluetooth; do
  printf '%s\n' '#!/bin/sh' >"$fogcast/bin/$tool-linux-armv7"
  chmod 0755 "$fogcast/bin/$tool-linux-armv7"
done
mkdir "$fixture/idle"
printf '%s\n' idle >"$fixture/idle/idle.rbf"
chmod 0444 "$fixture/idle/idle.rbf"
cat >"$fixture/runtime.lock.toml" <<EOF
[mister_runtime]
commit = '1111111111111111111111111111111111111111'
[splash_rbf]
fat_destination = '/idle.rbf'
[idle_rbf]
sha256 = '$(sha256sum "$fixture/idle/idle.rbf" | cut -d ' ' -f 1)'
size = $(wc -c <"$fixture/idle/idle.rbf" | tr -d ' ')
install_path = '/usr/share/mister-runtime/idle.rbf'
EOF
target=$fixture/target
mkdir -p "$target/usr/sbin" "$target/etc/init.d" "$fixture/buildroot-build" "$fixture/buildroot-host/bin"
for tool in mister-runtime mister-agent; do
  printf '%s\n' '#!/bin/sh' >"$target/usr/sbin/$tool"
  chmod 0755 "$target/usr/sbin/$tool"
done
: >"$target/etc/init.d/S40fes-kit-software"

unset FES_WINDOW_DRIVER_DIR
MAKEFLAGS=w MAKELEVEL=1 BUILD_DIR=$fixture/buildroot-build HOST_DIR=$fixture/buildroot-host \
  FOGCAST_DIR=$fogcast NATIVE_RUNTIME_INPUT_LOCK=$fixture/runtime.lock.toml \
  NATIVE_RUNTIME_IDLE_FILE=$fixture/idle/idle.rbf \
  sh "$board/native-post-build.sh" "$target"
# The post-build's kernel, build and SDK paths differ from the first build's,
# and the module is the same.
cmp "$fixture/output/version" "$target/window-driver/version"
cmp "$fixture/output/fes_window.ko" "$target/window-driver/fes_window.ko"
