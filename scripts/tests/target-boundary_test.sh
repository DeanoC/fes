#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)

if [ ! -d "$root/targetclient" ]; then
  echo "targetclient package is missing" >&2
  exit 1
fi

if rg -n 'github.com/DeanoC/FogCast/host' "$root/ui/kitlauncher" --glob '*.go'; then
  echo 'kit launcher still depends on host package' >&2
  exit 1
fi

if rg -n 'github.com/DeanoC/FogCast/(host|ui/|internal/agent)' "$root/targetclient" --glob '*.go'; then
  echo 'targetclient has a forbidden dependency' >&2
  exit 1
fi

if rg -n 'host\.(Client|KitLease|NewClient|NewKitLease|CastStatus|KitOwnership|ApplianceStatus|ResolveAppliance|KitLeaseHeader|ErrKitLeaseLost)' "$root" --glob '*.go' --glob '!vendor/**'; then
  echo 'removed host target-client symbol remains' >&2
  exit 1
fi

