"""Deterministic host tools and profile-controlled child Make settings."""
import os


def build_environment():
    env = os.environ.copy()
    for name in tuple(env):
        if (name.startswith(("TARGET_IMAGE_", "NATIVE_RUNTIME_",
                            "FES_PONG_PACKAGE_", "FES_ZX81_PACKAGE_", "FES_COLECO_PACKAGE_"))
                or name in ("LIBMISTER_RUNTIME_DIR", "MAKEFLAGS", "MAKEOVERRIDES", "MFLAGS",
                            "GOFLAGS", "GOEXPERIMENT", "GOOS", "GOARCH", "GOARM", "GOAMD64",
                            "GOWORK", "GOTOOLCHAIN", "GOENV", "GOFIPS140",
                            "FES_TOOLCHAIN_CACHE_ROOT", "FES_PACKAGE_IDS")):
            del env[name]
    env.update(GOENV="off", GOWORK="off", GOFLAGS="", GOEXPERIMENT="",
               GOAMD64="v1", GOTOOLCHAIN="auto", GOFIPS140="off")
    return env
