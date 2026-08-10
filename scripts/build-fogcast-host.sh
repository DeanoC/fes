#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
output=${1:-$repo/bin/FogCastHost.app}
signing_identity=${FOGCAST_SIGNING_IDENTITY:-}
version=${VERSION:-0.1.0}
revision=${REVISION:-$(git -C "$repo" rev-parse --verify HEAD 2>/dev/null || printf unknown)}
deployment_target=${MACOSX_DEPLOYMENT_TARGET:-11.0}
ldflags_allow=${CGO_LDFLAGS_ALLOW:--Wl,-weak_framework,.*}
cgo_cflags=${CGO_CFLAGS:-}
cgo_cxxflags=${CGO_CXXFLAGS:-}
cgo_ldflags=${CGO_LDFLAGS:-}

# Keep every native object at the same floor as the final arm64 helper. The
# deployment target environment controls the linker, while these explicit
# compiler/linker flags also cover cgo's generated objects.
cgo_cflags="$cgo_cflags -mmacosx-version-min=$deployment_target"
cgo_cxxflags="$cgo_cxxflags -mmacosx-version-min=$deployment_target"
cgo_ldflags="$cgo_ldflags -mmacosx-version-min=$deployment_target"

if [ -z "$signing_identity" ] || [ "$signing_identity" = "-" ]; then
	printf '%s\n' 'fogcast-host-build: FOGCAST_SIGNING_IDENTITY must name a stable local macOS signing identity' >&2
	exit 2
fi
if [ "$(uname -s)" != Darwin ]; then
	printf '%s\n' 'fogcast-host-build: a Darwin host is required for the AVFoundation cgo build' >&2
	exit 2
fi
if ! command -v codesign >/dev/null 2>&1; then
	printf '%s\n' 'fogcast-host-build: codesign is required' >&2
	exit 2
fi
case "$output" in
	*.app) ;;
	*) printf '%s\n' 'fogcast-host-build: output must be a .app bundle path' >&2; exit 2 ;;
esac

fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-host-build.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM
app=$fixture/FogCastHost.app
mkdir -p "$app/Contents/MacOS"
cp "$repo/resources/fogcast-host/Info.plist" "$app/Contents/Info.plist"

MACOSX_DEPLOYMENT_TARGET="$deployment_target" CGO_CFLAGS="$cgo_cflags" CGO_CXXFLAGS="$cgo_cxxflags" CGO_LDFLAGS="$cgo_ldflags" CGO_LDFLAGS_ALLOW="$ldflags_allow" CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go build \
	-buildvcs=false -trimpath \
	-ldflags "-s -w -X github.com/DeanoC/FogCast-POC/internal/version.Version=$version -X github.com/DeanoC/FogCast-POC/internal/version.Revision=$revision" \
	-o "$app/Contents/MacOS/fogcast-api" \
	"$repo/cmd/fogcast-api"

codesign --force --deep --options runtime \
	--entitlements "$repo/resources/fogcast-host/Entitlements.plist" \
	--sign "$signing_identity" "$app"

mkdir -p "$(dirname "$output")"
if [ -e "$output" ]; then
	if [ "${FOGCAST_HOST_REPLACE:-0}" != 1 ]; then
		printf '%s\n' "fogcast-host-build: output already exists; set FOGCAST_HOST_REPLACE=1 to replace $output" >&2
		exit 2
	fi
	rm -rf "$output"
fi
mv "$app" "$output"
printf '%s\n' "fogcast-host-build: wrote signed helper $output"
