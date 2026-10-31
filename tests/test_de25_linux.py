"""Exercise the DE25 image path in the parent software lane."""
import importlib.util
from pathlib import Path

LANE = Path(__file__).resolve().parents[1] / "image/de25-nano"


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, LANE / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


VendorBootTests = load("de25_linux_boot_tests", "test_boot.py").VendorBootTests
BuildTests = load("de25_linux_build_tests", "test_build.py")
HeadlessBuildTests = BuildTests.HeadlessBuildTests
RootfsIdentityTests = BuildTests.RootfsIdentityTests
RootfsTests = load("de25_linux_rootfs_tests", "rootfs/test_rootfs.py").RootfsTests
