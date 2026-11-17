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
_D25_PRODUCERS = (
    (re.compile(r"(?:^|/)boards/de25nano/.*/menu/|"
                r"(?:^|/)boards/de25nano/.*/build_menu_package\.py$", re.I),
     "build_menu_package.py"),
    (re.compile(r"(?:^|/)boards/de25nano/.*/zx81|"
                r"(?:^|/)boards/de25nano/.*/build_zx81_package\.py$", re.I),
     "build_zx81_package.py"),
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
    """Resolve a single DE25 Quartus producer; ambiguity fails closed."""
    labels = labels or []
    labelled = {"core:de25-menu": "build_menu_package.py",
                "core:de25-zx81": "build_zx81_package.py"}
    selected = {labelled[label] for label in labels if label in labelled}
    if len(selected) == 1:
        return next(iter(selected))
    if selected:
        return None
    hits = {producer for pattern, producer in _D25_PRODUCERS
            if any(pattern.search(path) for path in paths)}
    return next(iter(hits)) if len(hits) == 1 else None


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
