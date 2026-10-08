"""Buildroot toolchain identity used by the parent image fingerprint."""
from pathlib import Path
import sys

IMAGE = Path(__file__).resolve().parents[1] / "image"
sys.path.insert(0, str(IMAGE / "scripts"))
from toolchain_cache import key as toolchain_key, shared_root  # noqa: E402
