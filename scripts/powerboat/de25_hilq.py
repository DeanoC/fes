#!/usr/bin/env python3
"""Shared routing and safety rules for Powerboat's DE25 HIL automation."""

from __future__ import annotations

import re


_NON_DEPLOYED = re.compile(r"(^docs/|\.md$|^tests?/|/_test\.|/test_)", re.I)
_D25_DEPLOYED = re.compile(
    r"(^image/de25-nano/|^platform/.*de25|^sources/misteross/boards/de25nano/|"
    r"^sources/mister-packages/.*de25|^sources/libmister-runtime/.*de25|"
    r"^sources/FogCast/.*de25)", re.I
)


def kit_target(paths: list[str], labels: list[str] | None = None) -> str | None:
    """Return D25 for deployed DE25 work, otherwise preserve A/B routing."""
    labels = labels or []
    if any(label.startswith("core:de25-") for label in labels):
        return "D25"
    deployed = [p for p in paths if not _NON_DEPLOYED.search(p)]
    if any(_D25_DEPLOYED.search(p) for p in deployed):
        return "D25"
    return None


def de25_producer(paths: list[str], labels: list[str] | None = None) -> str | None:
    """Return the seal selection for D25 work; the exact head's recipes decide.

    `core:de25-<name>` labels name registered recipes (`fes.<name>`) and
    `core:de25-all` names every one. Otherwise D25 work seals `de25:auto`:
    scripts/de25_seal_plan.py at the exact head builds each registered DE25
    recipe and model whose producer identity moved since the merge base. The
    sealer refuses anything that is not a registered DE25 Quartus Pro recipe.
    """
    labels = labels or []
    named = sorted({label.removeprefix("core:de25-").lower() for label in labels
                    if label.startswith("core:de25-")})
    if "all" in named:
        return "de25:all"
    if named:
        return "de25:" + ",".join("fes." + name for name in named)
    return "de25:auto" if kit_target(paths, labels) == "D25" else None


def selected_kits(target: str | None) -> tuple[str, ...]:
    """D25 work is D25-only; untargeted existing HIL remains B/A."""
    return ("D25",) if target == "D25" else ("B", "A")


def recovery_write_allowed(expected_serial: str, observed_serial: str,
                           expected_target: str, observed_target: str,
                          device: str, expected_media_identity: str,
                          device_serial: str) -> bool:
    """Gate a card write on independent board and removable-device identity."""
    return (expected_serial == "TRWU8J8G" and observed_serial == expected_serial
            and expected_target == "de25-j8g" and observed_target == expected_target
            and bool(device) and device.startswith("/dev/disk/by-id/")
            and bool(expected_media_identity)
            and expected_media_identity in device
            and expected_media_identity in device_serial
            and "TRWU8J8G" in device_serial and "TRWU8CBL" not in device_serial)
