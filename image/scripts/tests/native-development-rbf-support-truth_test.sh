#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)

fail() {
	printf 'native-development-rbf-support-truth: %s\n' "$1" >&2
	exit 1
}

for path in \
	buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/mister-runtime/development.rbf \
	buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/mister-runtime/cores/development.rbf \
	buildroot/board/fogcast-target/native-rootfs-overlay/tmp/fogcast-development/core.rbf; do
	[ ! -e "$root/$path" ] || fail "packaged development artifact: $path"
done

if grep -R -n -E --exclude=native-development-rbf-support-truth_test.sh \
	'/usr/share/mister-runtime/(cores/)?development\.rbf' \
	"$root/buildroot" "$root/scripts" 2>/dev/null | grep -vE 'reject|absence|forbid|must not|test' >/dev/null; then
	fail 'production packaging references a development RBF'
fi

printf '%s\n' 'native development RBF support truth passed'
