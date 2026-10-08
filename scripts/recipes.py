"""Immutable format-2 recipe descriptors owned by the FES parent."""
from dataclasses import dataclass
import os
import keyword
import re
import tomllib
import subprocess
from pathlib import Path

def shared_cache_root():
    """Share immutable caches across Git worktrees; allow an explicit location."""
    configured = os.environ.get("FES_CACHE_ROOT")
    if configured:
        path = Path(configured)
        if not path.is_absolute():
            raise ValueError("FES_CACHE_ROOT must be absolute")
        return path
    root = Path(__file__).resolve().parents[1]
    try:
        result = subprocess.run(["git", "-C", str(root), "rev-parse", "--path-format=absolute", "--git-common-dir"],
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    except FileNotFoundError:
        return root / "out/cache"  # Image verifier containers need no Git executable.
    if result.returncode == 0:
        common = Path(result.stdout.strip())
        if common.name == ".git":
            root = common.parent
    return root / "out/cache"


CACHE_ROOT = shared_cache_root()
TOOLCHAIN_CACHE_ROOT = CACHE_ROOT / "misteross-toolchains"
ARTIFACT_CACHE_ROOT = Path(os.environ.get("FES_ARTIFACT_CACHE_ROOT", str(CACHE_ROOT / "core-packages")))
if not ARTIFACT_CACHE_ROOT.is_absolute():
    raise ValueError("FES_ARTIFACT_CACHE_ROOT must be absolute")
# FES_SOURCE_CLOSURE_AUDIT=1 makes producers record reads instead of enforcing
# the narrowed source closure. Packages built that way must never reach the
# shared artifact cache, so the bypass is honoured only when the effective
# artifact cache is a private one created by scripts/core_key_nightly.py.
AUDIT_PRIVATE_CACHE_MARKER = ".fes-closure-audit-private-cache"


def require_private_audit_cache(env=None):
    """Refuse closure audit mode unless packages go to a marked private cache."""
    env = os.environ if env is None else env
    if env.get("FES_SOURCE_CLOSURE_AUDIT") != "1":
        return
    root = ARTIFACT_CACHE_ROOT.resolve()
    if root == (CACHE_ROOT / "core-packages").resolve() or not (root / AUDIT_PRIVATE_CACHE_MARKER).is_file():
        raise ValueError(
            "FES_SOURCE_CLOSURE_AUDIT=1 is only allowed with the nightly's private artifact "
            f"cache (FES_ARTIFACT_CACHE_ROOT containing {AUDIT_PRIVATE_CACHE_MARKER}); "
            "unset it for normal builds")


HIP_ROUTER = "HIP"
HIP_ARCHITECTURES = "gfx1100;gfx1201"

_STRIP_NAMES = frozenset({
    "CC", "CXX", "CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS",
    "LD_LIBRARY_PATH", "PKG_CONFIG_PATH", "PYTHON", "PYTHON_CONFIG",
    "MAKEFLAGS", "MFLAGS", "NINJAFLAGS", "NINJA_STATUS",
    "LD_PRELOAD", "SOURCE_DATE_EPOCH",
    "GIT_DIR", "GIT_WORK_TREE", "GIT_SSH_COMMAND",
    "TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD",
    "FES_TOOLCHAIN_CACHE_ROOT", "FES_TOOLCHAIN_ROOT",
    "CUDA_HOME", "CUDACXX", "CUDA_PATH",
})
_STRIP_PREFIXES = ("GIT_CONFIG_", "CCACHE_", "DISTCC_", "CMAKE_")
_HIP_IDENTITY_NAMES = frozenset({"ROCM_PATH", "HIPCC"})


@dataclass(frozen=True)
class Format2Recipe:
    """One FES format-2 production recipe."""

    core_id: str
    producer_script: str
    producer_module: str
    authenticate: str
    lock_path: str
    gpu_router: str
    hip_architectures: str
    selection_filename: str
    package_dir_env: str
    package_selection_env: str
    quartus_role: str
    cache_root: Path = TOOLCHAIN_CACHE_ROOT
    identity_version: int = 2
    producer_arguments: tuple[str, ...] = ()
    producer_options: tuple[tuple[str, object], ...] = ()
    video_profiles: tuple[str, ...] = ()


REGISTRY_PATH = Path(__file__).resolve().parents[1] / "config/core-recipes.toml"
_OPTIONAL_RECIPE_FIELDS = frozenset({"producer_arguments", "producer_options", "video_profiles"})
_VIDEO_ARGUMENT = {
    "--native-video-socket": ("native_video", True),
    "--video-socket": ("video_socket", True),
}
_VIDEO_OPTION = {key: flag for flag, (key, _) in _VIDEO_ARGUMENT.items()}


def align_video_producer_choice(arguments, options):
    """Keep the CLI flag and the module option as one video-socket choice."""
    arguments = list(arguments)
    options = dict(options)
    flags = [item for item in arguments if item in _VIDEO_ARGUMENT]
    option_keys = [key for key in ("native_video", "video_socket") if options.get(key) is True]
    if len(flags) > 1 or len(option_keys) > 1:
        raise ValueError("video socket choice is encoded more than once")
    if flags and option_keys:
        key, value = _VIDEO_ARGUMENT[flags[0]]
        if option_keys != [key] or options.get(key) is not value:
            raise ValueError("producer arguments and options disagree on the video socket")
        return arguments, options
    if flags:
        key, value = _VIDEO_ARGUMENT[flags[0]]
        options[key] = value
        return arguments, options
    if option_keys:
        arguments.append(_VIDEO_OPTION[option_keys[0]])
        return arguments, options
    return arguments, options
_RECIPE_FIELDS = frozenset(Format2Recipe.__dataclass_fields__) - {"cache_root"} - _OPTIONAL_RECIPE_FIELDS
_IDENTIFIER = r"[A-Za-z_][A-Za-z0-9_]*"


def _relative_path(value):
    # Validate the original spelling, before Path can normalize traversal/dots.
    return all(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*", part)
               and part not in (".", "..") for part in value.split("/"))


def load_recipes(path=REGISTRY_PATH):
    """Load the closed version-1 data schema; never import or execute producers."""
    with Path(path).open("rb") as source:
        document = tomllib.load(source)
    if set(document) != {"version", "recipes"}:
        raise ValueError("recipe registry requires exactly version and recipes")
    if type(document["version"]) is not int or document["version"] != 1:
        raise ValueError("unsupported recipe registry version")
    entries = document["recipes"]
    if not isinstance(entries, list) or not entries:
        raise ValueError("recipes must be a nonempty array of tables")
    result, selections, environment_names = {}, set(), set()
    for entry in entries:
        if (not isinstance(entry, dict) or not _RECIPE_FIELDS <= set(entry)
                or set(entry) - _RECIPE_FIELDS - _OPTIONAL_RECIPE_FIELDS):
            raise ValueError("recipe fields must exactly match the version-1 schema")
        if type(entry.get("identity_version")) is not int or entry["identity_version"] != 2:
            raise ValueError("identity_version must be explicitly 2")
        if any(not isinstance(value, str) or not value.strip()
               or any(ord(char) < 32 or ord(char) == 127 for char in value)
               for key, value in entry.items() if key != "identity_version" and key not in _OPTIONAL_RECIPE_FIELDS):
            raise ValueError("recipe fields must be nonempty strings without control characters")
        entry = dict(entry)
        arguments = entry.get("producer_arguments", [])
        options = entry.get("producer_options", {})
        profiles = entry.get("video_profiles", [])
        if (not isinstance(arguments, list) or any(not isinstance(value, str) or not value
                or any(ord(c) < 32 or ord(c) == 127 for c in value) for value in arguments)):
            raise ValueError("invalid producer arguments")
        owned_arguments = {"--root", "--package-output", "--cache-root", "--identity-version"}
        if any(argument.startswith('--') and any(owned.startswith(argument.split('=', 1)[0])
               for owned in owned_arguments) for argument in arguments):
            raise ValueError('producer arguments cannot override assembly-owned inputs')
        reserved = {"root", "repository", "revision", "tools", "execution", "identity_version", "cache_root"}
        if (not isinstance(options, dict) or any(not re.fullmatch(_IDENTIFIER, key)
                or keyword.iskeyword(key) or key in reserved
                or type(value) not in (str, bool, int)
                or (isinstance(value, str) and any(ord(c) < 32 or ord(c) == 127 for c in value))
                for key, value in options.items())):
            raise ValueError("invalid producer options")
        if profiles not in ([], ["direct", "scanlines"]):
            raise ValueError("video profiles must select direct and scanlines in order")
        arguments, options = align_video_producer_choice(arguments, options)
        entry.update(producer_arguments=tuple(arguments), producer_options=tuple(sorted(options.items())),
                     video_profiles=tuple(profiles))
        core_id = entry["core_id"]
        if not re.fullmatch(r"fes\.[a-z0-9]+(?:[._-][a-z0-9]+)*", core_id):
            raise ValueError("invalid recipe core_id")
        for field in ("producer_script", "lock_path"):
            if not _relative_path(entry[field]):
                raise ValueError(f"invalid relative {field}")
        module = entry["producer_module"]
        if (not all(re.fullmatch(_IDENTIFIER, part) and not keyword.iskeyword(part)
                    for part in module.split("."))
                or entry["producer_script"] != module.replace(".", "/") + ".py"):
            raise ValueError("invalid producer_module or producer_script mismatch")
        auth = entry["authenticate"]
        if not re.fullmatch(_IDENTIFIER, auth) or keyword.iskeyword(auth):
            raise ValueError("invalid authenticate identifier")
        if entry["gpu_router"] != HIP_ROUTER:
            raise ValueError("only the HIP production route is supported")
        if not re.fullmatch(r"gfx[0-9a-f]+(?:;gfx[0-9a-f]+)*", entry["hip_architectures"]):
            raise ValueError("invalid HIP architectures")
        selection = entry["selection_filename"]
        if ("/" in selection or not _relative_path(selection)
                or not selection.endswith(".package-selection.toml")):
            raise ValueError("invalid selection_filename")
        names = (entry["package_dir_env"], entry["package_selection_env"])
        if any(not re.fullmatch(r"FES_[A-Z0-9_]+_PACKAGE_(?:DIR|SELECTION)", name)
               for name in names):
            raise ValueError("invalid package environment variable")
        if not names[0].endswith("_DIR") or not names[1].endswith("_SELECTION"):
            raise ValueError("package environment variable role mismatch")
        if core_id in result or selection in selections or any(
                name in environment_names for name in names) or names[0] == names[1]:
            raise ValueError("duplicate core ID, selection filename or environment variable")
        result[core_id] = Format2Recipe(**entry)
        selections.add(selection)
        environment_names.update(names)
    return result


FORMAT2_RECIPES = load_recipes()


def recipe_for(core_id):
    """Return the immutable descriptor for a supported format-2 core ID."""
    try:
        return FORMAT2_RECIPES[core_id]
    except KeyError as error:
        supported = ", ".join(FORMAT2_RECIPES)
        raise ValueError(
            f"unknown format-2 recipe {core_id!r}; supported: {supported}"
        ) from error


def producer_environment(env=None, recipe=None):
    """Scrub caller overrides and apply HIP identity without selecting cache mode."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    mapped = os.environ.copy() if env is None else dict(env)
    require_private_audit_cache(mapped)
    for name in tuple(mapped):
        if name in _HIP_IDENTITY_NAMES:
            continue
        if name in _STRIP_NAMES or name.startswith(_STRIP_PREFIXES):
            del mapped[name]
    mapped.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
    mapped.pop("FES_TOOLCHAIN_ROOT", None)
    mapped["FES_TOOLCHAIN_GPU_ROUTER"] = recipe.gpu_router
    mapped["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = recipe.hip_architectures
    return mapped
