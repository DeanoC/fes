"""Deterministic host tools and profile-controlled child Make settings."""
import os

from recipes import FORMAT2_RECIPES


def owned_package_environment_names():
    """Parent-owned package inputs. Ambient values must not reach the child."""
    names = {"FES_PACKAGE_IDS", "FES_VIDEO_PARTS_DIR", "FES_UNAVAILABLE_CORES"}
    for recipe in FORMAT2_RECIPES.values():
        names.add(recipe.package_dir_env)
        names.add(recipe.package_selection_env)
    return frozenset(names)


def build_environment():
    env = os.environ.copy()
    owned = owned_package_environment_names()
    for name in tuple(env):
        if (name.startswith(("TARGET_IMAGE_", "NATIVE_RUNTIME_")) or name in owned
                or name in ("FES_PREBUILT_RUNTIME_SHA256", "LIBMISTER_RUNTIME_DIR", "MAKEFLAGS", "MAKEOVERRIDES", "MFLAGS",
                            "GOFLAGS", "GOEXPERIMENT", "GOOS", "GOARCH", "GOARM", "GOAMD64",
                            "GOWORK", "GOTOOLCHAIN", "GOENV", "GOFIPS140",
                            "FES_TOOLCHAIN_CACHE_ROOT")):
            del env[name]
    env.update(GOENV="off", GOWORK="off", GOFLAGS="", GOEXPERIMENT="",
               GOAMD64="v1", GOTOOLCHAIN="auto", GOFIPS140="off")
    return env
