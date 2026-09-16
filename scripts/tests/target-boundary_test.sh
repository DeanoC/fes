#!/bin/sh
set -eu

root=${FOGCAST_BOUNDARY_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)}

if ! command -v rg >/dev/null 2>&1; then
  echo 'rg is required' >&2
  exit 1
fi

# Run rg outside a boolean test so exit >1 fails this script. Prints matches
# and returns 0 on matches, 1 on no matches.
rg_check() {
  set +e
  hits=$(rg -n --no-heading "$@")
  status=$?
  set -e
  case $status in
    0)
      printf '%s\n' "$hits" >&2
      return 0
      ;;
    1)
      return 1
      ;;
    *)
      printf 'rg failed with exit %s\n' "$status" >&2
      exit 1
      ;;
  esac
}

if [ ! -d "$root/targetclient" ]; then
  echo "targetclient package is missing" >&2
  exit 1
fi

if rg_check --glob '*.go' 'github.com/DeanoC/FogCast/host' "$root/ui/kitlauncher"; then
  echo 'kit launcher still depends on host package' >&2
  exit 1
fi

if rg_check --glob '*.go' 'github.com/DeanoC/FogCast/(host|ui/|internal/agent)' "$root/targetclient"; then
  echo 'targetclient has a forbidden dependency' >&2
  exit 1
fi

if rg_check --glob '*.go' --glob '!vendor/**' 'host\.(Client|KitLease|NewClient|NewKitLease|CastStatus|KitOwnership|ApplianceStatus|ResolveAppliance|KitLeaseHeader|ErrKitLeaseLost)' "$root"; then
  echo 'removed host target-client symbol remains' >&2
  exit 1
fi

# Public contract packages are required and must not import internal/ or host/UI.
for dir in protocol corepackage kitlease; do
  if [ ! -d "$root/$dir" ]; then
    echo "required public contract directory is missing: $dir" >&2
    exit 1
  fi
  if rg_check --glob '*.go' --glob '!*_test.go' 'github.com/DeanoC/FogCast/(internal/|host|ui/)' "$root/$dir"; then
    echo "public $dir/ has a forbidden host, UI, or internal dependency" >&2
    exit 1
  fi
done

# Target implementation must not import host, UI, or targetclient.
for dir in internal/agent internal/httpapi internal/mister internal/misterruntime internal/input internal/targetcache internal/applianceupdate internal/flightdiag; do
  if [ ! -d "$root/$dir" ]; then
    continue
  fi
  if rg_check --glob '*.go' --glob '!*_test.go' 'github.com/DeanoC/FogCast/(host|ui/|targetclient)' "$root/$dir"; then
    echo "$dir imports host, UI, or targetclient" >&2
    exit 1
  fi
done
