"""Deterministic host tools and profile-controlled child Make settings."""
import os


def build_environment():
    env = os.environ.copy()
    for name in tuple(env):
        if (name.startswith(("TARGET_IMAGE_", "NATIVE_RUNTIME_", "MEGADRIVE_RBF_", "PONG_RBF_", "SNES_RBF_", "NES_RBF_", "FES_PONG_PACKAGE_"))
                or name in ("LIBMISTER_RUNTIME_DIR", "MAKEFLAGS", "MAKEOVERRIDES", "MFLAGS",
                            "GOFLAGS", "GOEXPERIMENT", "GOOS", "GOARCH", "GOARM", "GOAMD64",
                            "GOWORK", "GOTOOLCHAIN", "GOENV", "GOFIPS140",
                            "FES_TOOLCHAIN_CACHE_ROOT")):
            del env[name]
    env.update(GOENV="off", GOWORK="off", GOFLAGS="", GOEXPERIMENT="",
               GOAMD64="v1", GOTOOLCHAIN="auto", GOFIPS140="off")
    return env
