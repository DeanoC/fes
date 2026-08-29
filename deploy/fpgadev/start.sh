#!/bin/sh
set -eu

# The package copy is a safe fallback template. install-profile publishes a
# smaller instance with the exact persistent-stage path and helper digest
# embedded; this template still refuses to execute an unverified fixed-path
# binary if it is invoked before that publication.
stage_root=/var/lib/fogcast/fpgadev-staging
for stage in "$stage_root"/*; do
	[ -d "$stage" ] || continue
	helper="$stage/bin/mister-fpga-dev"
	manifest="$stage/manifest.sha256"
	[ -f "$helper" ] && [ -f "$manifest" ] || continue
	expected=$(awk '$2 == "bin/mister-fpga-dev" { print $1; exit }' "$manifest")
	got=$(sha256sum "$helper" | awk '{ print $1 }')
	[ -n "$expected" ] && [ "$got" = "$expected" ] || continue
	exec "$helper" recover-install
done
exit 1
