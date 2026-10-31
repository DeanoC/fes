"""Per-core P&R recipes pin each producer's effective nextpnr invocation."""
import ast
import hashlib
import json
import re
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts import pnr_recipe


ROOT = pnr_recipe.FES_ROOT
SLOT = "/slot/bin/nextpnr-mistral"
SYNTH = "/fixture/synth.json"
OUTPUT = "/out"
ENV = {
    "PATH": "/slot/bin:/usr/bin:/bin",
    "HOME": "/tmp/private-build-home",
    "LANG": "C",
    "LC_ALL": "C",
    "TZ": "UTC",
}
# Each registered core's own toolchain.lock. A per-core bump changes only its
# core's entry here, plus that core's recipe and argv goldens.
LOCK_SHA256 = {
    "fes.menu": "2adfe0457b7d90debb76f52593c5101a88171a9c0b37ca3c2b2146c199ae0df0",
    "fes.catch": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.pong": "e9c28e573a5037cc7a3acef7637097691bf5bfac6f1a0cdca3bace84b9383edc",
    "fes.zx81": "27ceca798d9e7b24c8c770cba7b54794276398095c552217936d84c1a3c5312a",
    "fes.zx80": "27ceca798d9e7b24c8c770cba7b54794276398095c552217936d84c1a3c5312a",
    "fes.ts1000": "27ceca798d9e7b24c8c770cba7b54794276398095c552217936d84c1a3c5312a",
    "fes.ts1500": "27ceca798d9e7b24c8c770cba7b54794276398095c552217936d84c1a3c5312a",
    "fes.coleco": "cb2c2809dd1b9687fbb1f50d3c6ea3a8e01ebf328b1ea29c8d567854c93d0fdb",
    "fes.sms": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.sg1000": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.apple2": "bd19d80345cae5f8cc106a9128b2ff71e2ec3711de968341cedfec721ff7f675",
    "fes.c64": "bd19d80345cae5f8cc106a9128b2ff71e2ec3711de968341cedfec721ff7f675",
    "fes.spectrum": "213604b82009022085ea1bc504207da3c5b56db044d2e836fce8688d246afec3",
    "fes.spectrum128": "213604b82009022085ea1bc504207da3c5b56db044d2e836fce8688d246afec3",
    "fes.spectrum-plus3": "213604b82009022085ea1bc504207da3c5b56db044d2e836fce8688d246afec3",
    "fes.pentagon": "213604b82009022085ea1bc504207da3c5b56db044d2e836fce8688d246afec3",
    "fes.riscv": "2669044d4d481c94cf6ba7127614077480fd65ba730a784ac26f747764e472cf",
    "fes.ramtest": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.atari-st": "e4a11eac287674e473ab33ae356595245d564d32623be5b90472740d02aaecdb",
}
# Source-derived hashes of the complete CLI option-name sets from nextpnr's
# add_options() declarations. Schemas are indexed by the set itself, not pins.
SOURCE_OPTION_SET_SHA256 = {
    96: "e23f3850fe127f9694053f5d0c8700fff655ec1b9e3504cb3f9da779d0446432",
    105: "45926822f5e796342c3b60958db24c586273432fe6f07b9c319b4031745d784d",
    107: "05aa13ab70d4fa57a517b2df56f2aa45ac1d1de6a1a7f3e53dc1ba6080982e77",
}
SOURCE_OPTION_SETS = {
    len(option_names): option_names
    for option_names in pnr_recipe._SOURCE_OPTION_SET_SCHEMAS
}
SCHEMAS_BY_OPTION_COUNT = {
    len(option_names): schema
    for option_names, schema in pnr_recipe._SOURCE_OPTION_SET_SCHEMAS.items()
}
FLAGS_SHA256 = {
    56: "6d60ed8e921f893f4fc3db7faf7c71473e493c99fee7f503c6f96cc38fd20c3f",
    65: "38d02f4b8db3804837214ed9227bbac0b2b92f4687a3bb31a9d88d48d30ba5e6",
    67: "1a6b1280f5a3303b8ac0447b56e9af53d606633ec29ae9c4004ca700bf9d82b2",
}
PCXT_FLAGS_SHA256 = "1df45a51d1ebb46353c90454cf28986b9c565f05185a5a4adfc4fcaac9508b5a"
ZX81_FLAGS_SHA256 = "0d8e2411d19f74c2af22177bb9c5836ab7cb0e7713e5931f0eca7b0b11914d7e"
ZX81_FAMILY = {"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500"}

# The source-declared additions to the 105-option set, independently checked
# against the pinned nextpnr declarations.
ADDED_0A253257 = {
    "arrival_pin_assignment", "isolated_lut_placement", "critical_cohort_budget",
    "critical_cohort_report", "critical_cohort_model_out", "gpu_required",
    "mistral_dsp_signed_lanes", "router2_profile", "set",
}
ADDED_BEC82E36 = {
    "phase_related_harmonic_clocks", "mistral_ff_reset_reference",
}
NO_PYTHON_GUI_OPTIONS = {
    "gui", "gui_no_aa", "on_failure", "post_route", "pre_pack", "pre_place",
    "pre_route", "run",
}

# Stable digest of each complete recipe, plus the old producer command's
# normalized argv digest (slot, synth, output and GPU are fixed test values).
# The search vectors follow the former search_placer_qor._run_nextpnr order;
# Catch follows build_fes_demo.build_commands plus its former GPU suffix.
GOLDENS = {
    "fes.menu": ("2dab30da3b734c32b1ff52bfd0cdc035cca4bfe1fcc3989461bf3432be25e958", "b840cd3425c9582751f1a7847d988db554f889f0a27f7a234ad25077ccd473df"),
    "fes.catch": ("2ca8900a129a4b954198e400c5939b8ff28ec3043302ce111f5fabfc9b5c5431", "2e741531aae85de2e7a8796342603dd261b4a025ae564f0575ef1b9caa1dd863"),
    "fes.pong": ("c7be1fd18e5fea4deb93dc7247e759409c802db3073e11659ac591ddb68b589b", "213407fd4fd731c94a022df86b5460388dd691a54d5f7cb390cd2e5c0e80126a"),
    "fes.zx81": ("9bc04386391dad1f1991315d44866f283758dfe5fe1d421af5eab848832c8340", "1a99f0e9f4026971913ec926e4836f3ed84f5cd1de3bc55e233beb3c677f0d95"),
    "fes.zx80": ("9bc04386391dad1f1991315d44866f283758dfe5fe1d421af5eab848832c8340", "1a99f0e9f4026971913ec926e4836f3ed84f5cd1de3bc55e233beb3c677f0d95"),
    "fes.ts1000": ("9bc04386391dad1f1991315d44866f283758dfe5fe1d421af5eab848832c8340", "1a99f0e9f4026971913ec926e4836f3ed84f5cd1de3bc55e233beb3c677f0d95"),
    "fes.ts1500": ("9bc04386391dad1f1991315d44866f283758dfe5fe1d421af5eab848832c8340", "1a99f0e9f4026971913ec926e4836f3ed84f5cd1de3bc55e233beb3c677f0d95"),
    "fes.coleco": ("5a6fccb367857e0282be870b60298989f1d845023f9198e79d979e06755c927d", "93de0822ff64f833c8e571cf08b0b7d312f005bde176ad43d308c3f7ec2142af"),
    "fes.sms": ("aaa419748321cab16db125a53dd74b54c0240bb5a021b32a3db417b56543bfe5", "9f5b6f7ee3c682d7927ee0ceb03bd307f891431c209d88aa12ac20004cf97126"),
    "fes.sg1000": ("0faae72f9ba02b06832ddf7b537823c87bc61acf31672e43bb9da36a6abd66e1", "abe6012bd8052f8814e813182b95509c08ff3c906e3f926a1c0edb49f4982b85"),
    "fes.apple2": ("57406311d69ef2e121f002c7cf0afbbbfb7b1c26d114acf88e1664a4b482ddd3", "66f49fb8cf973ded993aff3540cba1c3dc01c5d58c2e1c8353eb10003aaee65b"),
    "fes.c64": ("6373cfbc3a68996ad89efbba3af45fcf0d477466e1b4a45eca5b8c38ebd5ff37", "86b9d7ef13b01b2ff5c1f2cc2e979d117e8e1470aeaf88ce85483163048b6134"),
    "fes.spectrum": ("09c8a0e75e87113ac9cfd5a05071d62431ce8427b4d7b95c38897d5e95305c29", "df1e8572d39b1baa8febcd96bee550765c2d5451b27baea60682e52a5d87f21d"),
    "fes.spectrum128": ("09c8a0e75e87113ac9cfd5a05071d62431ce8427b4d7b95c38897d5e95305c29", "df1e8572d39b1baa8febcd96bee550765c2d5451b27baea60682e52a5d87f21d"),
    "fes.spectrum-plus3": ("09c8a0e75e87113ac9cfd5a05071d62431ce8427b4d7b95c38897d5e95305c29", "df1e8572d39b1baa8febcd96bee550765c2d5451b27baea60682e52a5d87f21d"),
    "fes.pentagon": ("09c8a0e75e87113ac9cfd5a05071d62431ce8427b4d7b95c38897d5e95305c29", "df1e8572d39b1baa8febcd96bee550765c2d5451b27baea60682e52a5d87f21d"),
    "fes.riscv": ("c7be1fd18e5fea4deb93dc7247e759409c802db3073e11659ac591ddb68b589b", "213407fd4fd731c94a022df86b5460388dd691a54d5f7cb390cd2e5c0e80126a"),
    "fes.ramtest": ("ede6474bf85ce45e0631d4edb375bc37ab9c1161aae0a265f3c5c34920947781", "ded346bda6c66889e7f08e994eb82964871784b747cbc24159782b25406da42e"),
    "fes.atari-st": ("024942708300607968eb887324d48912d50c96634fe7ea0cd1a74d16776dbfd0", "31597590be7e868a81521a62ceaa190e7af72dfffc106b9c5898646ea56e040a"),
}

# Hashes of the frozen legacy producer argv matrix for every registered
# seed/weight pair. These are independent of the recipe hash and protect the
# migrated argv layout and each production combination.
ARGV_MATRIX_GOLDENS = {
    "fes.menu": "d356ffdf406928e2d588be6e41f92ef4a51dd6b02fce5e660f123c97d2cd7bcb",
    "fes.catch": "79b8ac93741f11b180132307a0b3261e6e41839e89240c6363f0087e03e75bc4",
    "fes.pong": "177f0476e55e89727fd172de68e2a08b4c684e2a0c99184e4330acfaa4efa85c",
    "fes.zx81": "949fc3d65ea57966453b12b924c229f30974afb83311d95e0d1cdd544d529a16",
    "fes.zx80": "949fc3d65ea57966453b12b924c229f30974afb83311d95e0d1cdd544d529a16",
    "fes.ts1000": "949fc3d65ea57966453b12b924c229f30974afb83311d95e0d1cdd544d529a16",
    "fes.ts1500": "949fc3d65ea57966453b12b924c229f30974afb83311d95e0d1cdd544d529a16",
    "fes.coleco": "7bde18f0254f40a680638a4476839141eaf65ffbf2a0bf7069cec059332f9439",
    "fes.sms": "f1c5c9118f716306b8493bccb3d24c1bec6ce5d20ea4c942b618c7aa728499bb",
    "fes.sg1000": "e67b5e8e63cf0c3ba560d58c04167361d95e2cf3d653d1d1ff42524fdea9d7fb",
    "fes.apple2": "9810f5f4a5870d854ad0e9c5d364d587e95f75ae9d7316f2772683ee1f8395fb",
    "fes.c64": "ed1c7847679e3d9d18654e7e32892c2e585cc1317dd058a865668bb8860223f1",
    "fes.spectrum": "e3836e041f0ebefcf4eeff1c8969849321ce5f9ada1ed30e499cc2064febe060",
    "fes.spectrum128": "e3836e041f0ebefcf4eeff1c8969849321ce5f9ada1ed30e499cc2064febe060",
    "fes.spectrum-plus3": "e3836e041f0ebefcf4eeff1c8969849321ce5f9ada1ed30e499cc2064febe060",
    "fes.pentagon": "e3836e041f0ebefcf4eeff1c8969849321ce5f9ada1ed30e499cc2064febe060",
    "fes.riscv": "177f0476e55e89727fd172de68e2a08b4c684e2a0c99184e4330acfaa4efa85c",
    "fes.ramtest": "159f755b9a023c72346c869160555cb102132b77c7ca714bd0b52980be3d4079",
    "fes.atari-st": "36c86eaedd189ac95d79f18c08cd140d6def21d83613e863d6dae20567ab6d84",
}


def _legacy_argv(core_id, recipe, *, slot=SLOT, synth=SYNTH, output=OUTPUT,
                 seed=None, weight=None, gpu=0):
    seed = recipe["seeds"][0] if seed is None else seed
    weight = recipe["timing_weights"][0] if weight is None else weight
    command = [slot, "--json", synth, "--device", "5CSEBA6U23I7", "--qsf", recipe["qsf"]]
    output_files = {
        "--report": f"{output}/timing.json",
        "--write": f"{output}/routed.json",
        "--rbf": f"{output}/core.rbf",
    }
    if recipe["argv_style"] == "search":
        command.extend(("--seed", str(seed),
                        "--placer-heap-timingweight", str(weight),
                        "--placer-heap-critexp", str(recipe["critexp"])))
        for flag in ("--report", "--write", "--rbf"):
            command.extend((flag, output_files[flag]))
        command.extend(("--compress-rbf", "--detailed-timing-report"))
        if recipe["timing_allow_fail"]:
            command.append("--timing-allow-fail")
        command.extend(("--router", recipe["router"]))
        if recipe["gpu_device"] == "lease":
            command.extend(("--gpu-device", "0"))
        command.extend(("--sdc", recipe["sdc"], "--freq", recipe["freq"]))
    else:
        command.extend(("--sdc", recipe["sdc"], "--freq", recipe["freq"],
                        "--seed", str(seed), "--router", recipe["router"],
                        "--rbf", output_files["--rbf"], "--compress-rbf",
                        "--write", output_files["--write"],
                        "--report", output_files["--report"],
                        "--detailed-timing-report"))
        if recipe["gpu_device"] == "lease":
            command.extend(("--gpu-device", "0"))
    if core_id == "fes.pcxt":
        command.extend(("--timing-report-paths", str(recipe["flags"]["timing_report_paths"])))
    if core_id in ZX81_FAMILY:
        command.append("--arrival-pin-assignment")
    return command


def _toml(value):
    if type(value) is bool:
        return "true" if value else "false"
    if isinstance(value, list):
        return "[" + ", ".join(json.dumps(item) for item in value) + "]"
    return json.dumps(value)


def _core_on_nextpnr(directory, core_id, pin, *, omit=(), values=None):
    """Copy the registry and locks, then give one core the supplied nextpnr pin."""
    root = Path(directory)
    registry = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())
    for entry in registry["recipes"]:
        lock = root / "sources/misteross" / entry["lock_path"]
        lock.parent.mkdir(parents=True, exist_ok=True)
        lock.write_bytes((ROOT / "sources/misteross" / entry["lock_path"]).read_bytes())
    lock = root / "sources/misteross/cores" / f"{core_id.replace('.', '-')}/toolchain.lock"
    text, count = re.subn(r'(\[tool\.nextpnr\][^\[]*?commit = ")[0-9a-f]{40}"',
                          rf'\g<1>{pin}"', lock.read_text())
    assert count == 1
    lock.write_text(text)
    source = (ROOT / "config/core-recipes.toml").read_text(encoding="utf-8")
    document = tomllib.loads(source)
    flags = dict(next(entry["pnr"]["flags"] for entry in document["recipes"]
                      if entry["core_id"] == core_id))
    flags.update(values or {})
    flags = {name: value for name, value in flags.items() if name not in omit}
    table = "".join(f"{name} = {_toml(value)}\n" for name, value in flags.items())
    start = source.index(f'core_id = "{core_id}"')
    end = source.index("[[recipes]]", start + 1)
    header = "[recipes.pnr.flags]\n"
    first = source.index(header, start, end) + len(header)
    assert "\n[" not in source[first:end]
    (root / "config").mkdir()
    registry_path = root / "config/core-recipes.toml"
    registry_path.write_text(source[:first] + table + "\n" + source[end:], encoding="utf-8")
    return patch.multiple(pnr_recipe, FES_ROOT=root, REGISTRY=registry_path)


def _pong_on_nextpnr(directory, pin, *, omit=(), values=None):
    return _core_on_nextpnr(directory, "fes.pong", pin, omit=omit, values=values)


def _fake_nextpnr_help(path, option_names):
    lines = ["  -h [ --help ]" if name == "help"
             else f"  --{name.replace('_', '-')} arg"
             for name in sorted(option_names)]
    text = "nextpnr option listing\n" + "\n".join(lines) + "\n"
    path.write_text(
        "#!/usr/bin/env python3\nimport sys\n"
        f"print({json.dumps(text)}, end='')\n"
        "if sys.argv[1:] != ['--help']:\n    raise SystemExit(2)\n",
        encoding="utf-8")
    path.chmod(0o755)


class PnrRecipeTests(unittest.TestCase):
    def test_every_registered_lock_pins_a_recipe_with_its_option_schema(self):
        from scripts.lockfile import load_lock
        registry = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())
        self.assertEqual(set(GOLDENS), {entry["core_id"] for entry in registry["recipes"] if entry.get("toolchain", "oss") == "oss"})
        self.assertEqual(set(LOCK_SHA256), set(GOLDENS))
        for entry in registry["recipes"]:
            if entry.get("toolchain", "oss") != "oss":
                continue
            core_id = entry["core_id"]
            with self.subTest(core=core_id):
                lock = ROOT / "sources/misteross" / entry["lock_path"]
                self.assertEqual(lock, pnr_recipe.lock_path(ROOT / "sources/misteross", core_id))
                self.assertEqual(hashlib.sha256(lock.read_bytes()).hexdigest(), LOCK_SHA256[core_id])
                pin = load_lock(lock)["nextpnr"].commit
                self.assertEqual(pnr_recipe.nextpnr_pin(core_id), pin)
                if core_id in {"fes.coleco", "fes.pcxt"}:
                    option_count = 107
                elif core_id in {"fes.pong", "fes.riscv", "fes.atari-st"} | ZX81_FAMILY:
                    option_count = 105
                else:
                    option_count = 96
                pnr_recipe.validate_option_names(core_id, SOURCE_OPTION_SETS[option_count])
                self.assertEqual(set(pnr_recipe.recipe_for(core_id)["flags"]),
                                 set(SCHEMAS_BY_OPTION_COUNT[option_count]))

    def test_source_option_sets_select_exact_schemas_and_shared_defaults(self):
        self.assertEqual(set(SOURCE_OPTION_SETS), {96, 105, 107})
        self.assertEqual(set(SCHEMAS_BY_OPTION_COUNT), {96, 105, 107})
        for count, option_names in SOURCE_OPTION_SETS.items():
            digest = hashlib.sha256(json.dumps(
                sorted(option_names), separators=(",", ":")).encode()).hexdigest()
            self.assertEqual(digest, SOURCE_OPTION_SET_SHA256[count])
        old = SCHEMAS_BY_OPTION_COUNT[96]
        new = SCHEMAS_BY_OPTION_COUNT[105]
        newest = SCHEMAS_BY_OPTION_COUNT[107]
        self.assertEqual(set(new) - set(old), ADDED_0A253257)
        self.assertEqual(set(newest) - set(new), ADDED_BEC82E36)
        self.assertEqual({name: new[name] for name in old}, dict(old))
        self.assertEqual({name: newest[name] for name in new}, dict(new))
        self.assertEqual(len(old), 56)
        self.assertEqual(len(new), 65)
        self.assertEqual(len(newest), 67)
        for schema in (old, new, newest):
            self.assertEqual(hashlib.sha256(json.dumps(
                dict(schema), sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
                FLAGS_SHA256[len(schema)])

    def test_each_schema_accepts_only_full_or_exact_no_python_gui_option_set(self):
        self.assertEqual(pnr_recipe._ABSENT_BY_RECIPE_OPTIONS, NO_PYTHON_GUI_OPTIONS)
        pins = {
            96: "9128800fa6e77c4138088a7107a95376ba47ff15",
            105: "f485b5857affc377199af1b5bfcd63d31330b50b",
            107: "bec82e36d436acec7cbeb248725acfde72b8f220",
        }
        for count, classified_options in SOURCE_OPTION_SETS.items():
            with self.subTest(option_count=count), tempfile.TemporaryDirectory() as directory, \
                    _core_on_nextpnr(directory, "fes.menu", pins[count]):
                for variant, option_names in (
                        ("full", classified_options),
                        ("no-python-no-gui", classified_options - NO_PYTHON_GUI_OPTIONS)):
                    with self.subTest(variant=variant):
                        binary = Path(directory) / f"nextpnr-{variant}"
                        _fake_nextpnr_help(binary, option_names)
                        self.assertEqual(
                            dict(pnr_recipe.validate_nextpnr_tool("fes.menu", binary)),
                            dict(SCHEMAS_BY_OPTION_COUNT[count]))

    def test_no_python_gui_option_set_rejects_partial_and_unrelated_mismatches(self):
        pins = {
            96: "9128800fa6e77c4138088a7107a95376ba47ff15",
            105: "f485b5857affc377199af1b5bfcd63d31330b50b",
            107: "bec82e36d436acec7cbeb248725acfde72b8f220",
        }
        for count, classified_options in SOURCE_OPTION_SETS.items():
            mismatches = {
                "partial-no-python-no-gui": classified_options - {"gui"},
                "extra-option": classified_options | {"unexpected_option"},
                "missing-non-gui-option": classified_options - {"randomize_seed"},
            }
            for case, option_names in mismatches.items():
                with self.subTest(option_count=count, case=case), \
                        tempfile.TemporaryDirectory() as directory, \
                        _core_on_nextpnr(directory, "fes.menu", pins[count]):
                    binary = Path(directory) / "nextpnr-mismatch"
                    _fake_nextpnr_help(binary, option_names)
                    with self.assertRaisesRegex(
                            pnr_recipe.RecipeError,
                            "nextpnr CLI option set differs from every classified schema"):
                        pnr_recipe.validate_nextpnr_tool("fes.menu", binary)

    def test_menu_pnr_step_accepts_0a253257_and_moving_f485_head(self):
        pins = (
            "0a253257eadd8a7877b881ab1f940cbd9739c1be",
            "f485b5857affc377199af1b5bfcd63d31330b50b",
        )
        source_options = SOURCE_OPTION_SETS[105]
        for pin in pins:
            with self.subTest(pin=pin), tempfile.TemporaryDirectory() as directory, \
                    _core_on_nextpnr(directory, "fes.menu", pin):
                binary = Path(directory) / "nextpnr-mistral"
                _fake_nextpnr_help(binary, source_options)
                schema = pnr_recipe.validate_nextpnr_tool("fes.menu", binary)
                recipe = pnr_recipe.recipe_for("fes.menu")
                self.assertEqual(pnr_recipe.nextpnr_pin("fes.menu"), pin)
                self.assertEqual(dict(recipe["flags"]), dict(schema))
                self.assertEqual({name: recipe["flags"][name] for name in ADDED_0A253257},
                                 {name: pnr_recipe._ADDED_105_OPTIONS[name]
                                  for name in ADDED_0A253257})
                command = pnr_recipe.argv("fes.menu", binary, SYNTH,
                                          recipe["seeds"][0], 0, output=OUTPUT)
                expected = _legacy_argv("fes.menu", recipe, slot=str(binary),
                                        seed=recipe["seeds"][0])
                self.assertEqual(list(command), expected)

    def test_coleco_bec82e36_schema_keeps_default_argv_unchanged(self):
        pin = "bec82e36d436acec7cbeb248725acfde72b8f220"
        with tempfile.TemporaryDirectory() as directory, \
                _core_on_nextpnr(directory, "fes.coleco", pin):
            binary = Path(directory) / "nextpnr-mistral"
            _fake_nextpnr_help(binary, SOURCE_OPTION_SETS[107])
            schema = pnr_recipe.validate_nextpnr_tool("fes.coleco", binary)
            recipe = pnr_recipe.recipe_for("fes.coleco", binary)
            self.assertEqual(pnr_recipe.nextpnr_pin("fes.coleco"), pin)
            self.assertEqual(dict(recipe["flags"]), dict(schema))
            self.assertEqual({name: recipe["flags"][name] for name in ADDED_BEC82E36},
                             {name: False for name in ADDED_BEC82E36})
            command = pnr_recipe.argv(
                "fes.coleco", binary, SYNTH, recipe["seeds"][0], 0, output=OUTPUT)
            self.assertEqual(list(command), _legacy_argv(
                "fes.coleco", recipe, slot=str(binary), seed=recipe["seeds"][0]))

    def test_runtime_recipe_schema_is_selected_by_binary_identity(self):
        pin = pnr_recipe.nextpnr_pin("fes.menu")
        with tempfile.TemporaryDirectory() as directory, _core_on_nextpnr(
                directory, "fes.menu", pin):
            first = Path(directory) / "first-nextpnr"
            second = Path(directory) / "second-nextpnr"
            _fake_nextpnr_help(first, SOURCE_OPTION_SETS[96])
            _fake_nextpnr_help(second, SOURCE_OPTION_SETS[105])

            pnr_recipe.validate_nextpnr_tool("fes.menu", first)
            pnr_recipe.validate_nextpnr_tool("fes.menu", second)

            self.assertEqual(len(pnr_recipe.recipe_for("fes.menu", first)["flags"]), 56)
            self.assertEqual(len(pnr_recipe.recipe_for("fes.menu", second)["flags"]), 65)
            first_digest = pnr_recipe.recipe_digest("fes.menu", first)
            second_digest = pnr_recipe.recipe_digest("fes.menu", second)
            self.assertNotEqual(first_digest, second_digest)
            self.assertEqual(pnr_recipe.recipe_digest("fes.menu", first), first_digest)

    def test_argv_revalidates_a_second_binary_and_reports_option_set_difference(self):
        pin = pnr_recipe.nextpnr_pin("fes.menu")
        with tempfile.TemporaryDirectory() as directory, _core_on_nextpnr(
                directory, "fes.menu", pin):
            first = Path(directory) / "first-nextpnr"
            second = Path(directory) / "second-nextpnr"
            options = SOURCE_OPTION_SETS[96]
            mismatched = (set(options) - {"randomize_seed"}) | {"unclassified_option"}
            _fake_nextpnr_help(first, options)
            _fake_nextpnr_help(second, mismatched)
            seed = pnr_recipe.recipe_for("fes.menu")["seeds"][0]

            with patch.object(pnr_recipe.subprocess, "run",
                              wraps=pnr_recipe.subprocess.run) as inspect_help:
                pnr_recipe.argv("fes.menu", first, SYNTH, seed, 0, output=OUTPUT)
                with self.assertRaises(pnr_recipe.RecipeError) as raised:
                    pnr_recipe.argv("fes.menu", second, SYNTH, seed, 0, output=OUTPUT)

            message = str(raised.exception)
            self.assertIn("missing ['randomize_seed']", message)
            self.assertIn("extra ['unclassified_option']", message)
            inspected = [Path(call.args[0][0]) for call in inspect_help.call_args_list]
            self.assertEqual(inspected, [first.resolve(), second.resolve()])

    def test_authenticated_core_lock_validates_its_compiler_option_set(self):
        from scripts import fes_build_common

        with tempfile.TemporaryDirectory() as directory, _core_on_nextpnr(
                directory, "fes.menu", "f485b5857affc377199af1b5bfcd63d31330b50b"):
            binary = Path(directory) / "nextpnr-mistral"
            _fake_nextpnr_help(binary, SOURCE_OPTION_SETS[105])
            root = Path(directory) / "sources/misteross"
            lock = root / "cores/fes-menu/toolchain.lock"
            tools = {"nextpnr-mistral": fes_build_common.AuthenticatedTool(
                path=binary, identity="commit=f485b585; sha256=fixture")}
            fes_build_common._validate_registered_pnr_options(root, lock, tools)
            self.assertEqual(len(pnr_recipe.recipe_for("fes.menu")["flags"]), 65)

    def test_option_set_differences_report_missing_and_extra_names(self):
        expected = SOURCE_OPTION_SETS[105]
        cases = (
            (set(expected) | {"unexpected_option"}, "extra ['unexpected_option']"),
            (set(expected) - {"router2_profile"}, "missing ['router2_profile']"),
        )
        for options, message in cases:
            with self.subTest(message=message), tempfile.TemporaryDirectory() as directory, \
                    _core_on_nextpnr(directory, "fes.menu",
                                     "f485b5857affc377199af1b5bfcd63d31330b50b"):
                with self.assertRaises(pnr_recipe.RecipeError) as raised:
                    pnr_recipe.validate_option_names("fes.menu", options)
                self.assertIn(message, str(raised.exception))

    def test_old_compiler_rejects_recipe_flags_it_cannot_accept(self):
        pin = "9128800fa6e77c4138088a7107a95376ba47ff15"
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, pin, values={"arrival_pin_assignment": False}):
            with self.assertRaisesRegex(pnr_recipe.RecipeError,
                                        "extra .*arrival_pin_assignment"):
                pnr_recipe.validate_option_names("fes.pong", SOURCE_OPTION_SETS[96])

    def test_new_schema_fails_when_a_per_core_option_is_missing(self):
        pin = "f485b5857affc377199af1b5bfcd63d31330b50b"
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, pin, omit=("placer",)):
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "missing .*placer"):
                pnr_recipe.validate_option_names("fes.pong", SOURCE_OPTION_SETS[105])

    def test_552ba246_options_reach_argv_when_set(self):
        values = {
            "arrival_pin_assignment": True, "isolated_lut_placement": True,
            "critical_cohort_budget": 4, "critical_cohort_report": "prior/timing.json",
            "gpu_required": True, "mistral_dsp_signed_lanes": True,
            "router2_profile": "profile.json",
            "set": ["gpurouter/repairBbExpand=4", "placerHeap/netShareWeight=0.5"],
        }
        pin = "0a253257eadd8a7877b881ab1f940cbd9739c1be"
        with tempfile.TemporaryDirectory() as directory, \
                _pong_on_nextpnr(directory, pin, values=values):
            pnr_recipe.validate_option_names("fes.pong", SOURCE_OPTION_SETS[105])
            recipe = pnr_recipe.recipe_for("fes.pong")
            command = list(pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT))
        legacy = _legacy_argv("fes.pong", recipe, seed=1)
        self.assertEqual(command[:len(legacy)], legacy)
        self.assertEqual(command[len(legacy):], [
            "--arrival-pin-assignment", "--critical-cohort-budget", "4",
            "--critical-cohort-report", "prior/timing.json", "--gpu-required",
            "--isolated-lut-placement", "--mistral-dsp-signed-lanes",
            "--router2-profile", "profile.json",
            "--set", "gpurouter/repairBbExpand=4", "--set", "placerHeap/netShareWeight=0.5",
        ])

    def test_552ba246_settings_cannot_replace_another_recipe_input(self):
        rejected = (
            {"set": ["seed=1"]}, {"set": ["target_freq=10"]}, {"set": ["sdc/clock/clk_sys=net"]},
            {"set": ["sdc/period/clk_sys=30"]}, {"set": ["timing/allowFail=1"]},
            {"set": ["placerHeap/alpha=0.5"]}, {"set": ["router2/profilePath=x.json"]},
            {"set": ["gpurouter/device=1"]}, {"set": ["mistral/ff4=1"]}, {"set": ["fes/slot_clock=x"]},
            {"set": ["gpurouter/maxIter"]}, {"set": ["gpurouter/maxIter="]}, {"set": ["=1"]},
            {"set": ["gpurouter/maxIter=1", "gpurouter/maxIter=2"]},
            {"set": ["gpurouter/maxIter=1"], "gpu_opt": ["maxIter=2"]},
            {"critical_cohort_budget": 65},
            {"critical_cohort_report": "prior/timing.json"},
            {"critical_cohort_model_out": "model.json", "critical_cohort_budget": 4},
        )
        for values in rejected:
            with self.subTest(values=values), tempfile.TemporaryDirectory() as directory, \
                    _pong_on_nextpnr(directory, "0a253257eadd8a7877b881ab1f940cbd9739c1be",
                                    values=values):
                with self.assertRaisesRegex(pnr_recipe.RecipeError, "invalid nextpnr option value"):
                    pnr_recipe.recipe_for("fes.pong")
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, "0a253257eadd8a7877b881ab1f940cbd9739c1be",
                values={"critical_cohort_model_out": "model.json"}):
            pnr_recipe.validate_option_names("fes.pong", SOURCE_OPTION_SETS[105])
            command = pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT)
        self.assertEqual(command[-2:], ("--critical-cohort-model-out", "model.json"))

    def test_recipe_and_legacy_argv_goldens_for_every_registered_producer(self):
        for core_id, (recipe_sha, argv_sha) in GOLDENS.items():
            with self.subTest(core=core_id):
                recipe = pnr_recipe.recipe_for(core_id)
                self.assertEqual(pnr_recipe.recipe_digest(core_id), recipe_sha)
                flags_sha = hashlib.sha256(json.dumps(
                    recipe["flags"], sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                expected_flags_sha = (PCXT_FLAGS_SHA256 if core_id == "fes.pcxt" else
                                      ZX81_FLAGS_SHA256 if core_id in ZX81_FAMILY else
                                      FLAGS_SHA256[len(recipe["flags"])])
                self.assertEqual(flags_sha, expected_flags_sha)
                weights = tuple(dict.fromkeys((*recipe["timing_weights"], *recipe["search_weights"])))
                optional_env = {**ENV, **recipe["optional_env"]}
                self.assertEqual(pnr_recipe.environment(core_id, optional_env), optional_env)
                matrix = []
                for seed in recipe["seeds"]:
                    for weight in weights:
                        command, environment = pnr_recipe.build(
                            core_id, SLOT, SYNTH, seed, 0, output=OUTPUT, env=ENV,
                            weight=weight, critexp=recipe["critexp"])
                        expected = _legacy_argv(core_id, recipe, seed=seed, weight=weight)
                        self.assertEqual(list(command), expected)
                        self.assertEqual(environment, ENV)
                        matrix.append([seed, weight, expected])
                        if seed == recipe["seeds"][0] and weight == recipe["timing_weights"][0]:
                            self.assertEqual(hashlib.sha256(json.dumps(
                                list(command), separators=(",", ":")).encode()).hexdigest(), argv_sha)
                matrix_sha = hashlib.sha256(json.dumps(
                    matrix, separators=(",", ":")).encode()).hexdigest()
                self.assertEqual(matrix_sha, ARGV_MATRIX_GOLDENS[core_id])

    def test_registry_edits_change_the_live_recipe_digest(self):
        source = (ROOT / "config/core-recipes.toml").read_text(encoding="utf-8")
        start = source.index('core_id = "fes.pong"')
        end = source.index("[[recipes]]", start + 1)
        pong = source[start:end]
        old_seeds = "seeds = [1, 2, 3, 4, 5, 6, 7, 8]"
        self.assertIn(old_seeds, pong)
        changed = source[:start] + pong.replace(
            old_seeds, "seeds = [1, 2, 3, 4, 5, 6, 7, 8, 9]", 1
        ) + source[end:]
        with tempfile.TemporaryDirectory() as directory:
            registry = Path(directory) / "core-recipes.toml"
            registry.write_text(source, encoding="utf-8")
            with patch.object(pnr_recipe, "REGISTRY", registry):
                initial = pnr_recipe.recipe_digest("fes.pong")
                registry.write_text(changed, encoding="utf-8")
                self.assertNotEqual(pnr_recipe.recipe_digest("fes.pong"), initial)

    def test_nondefault_flags_and_recipe_extras_are_serialized(self):
        import copy

        for core_id in ("fes.pong", "fes.catch"):
            with self.subTest(core=core_id):
                recipe = copy.deepcopy(pnr_recipe.recipe_for(core_id))
                recipe["extra"] = ["--router2-alt-weights"]
                if core_id == "fes.pong":
                    recipe["flags"]["parallel_refine"] = True
                    recipe["flags"]["placer_heap_alpha"] = 0.2
                    recipe["flags"]["remap_optimize_pins"] = True
                with patch.object(pnr_recipe, "recipe_for", return_value=recipe):
                    command = pnr_recipe.argv(
                        core_id, SLOT, SYNTH, recipe["seeds"][0], 0, output=OUTPUT
                    )
                self.assertIn("--router2-alt-weights", command)
                if core_id == "fes.pong":
                    self.assertIn("--parallel-refine", command)
                    self.assertIn("--threads", command)
                    self.assertIn("--placer-heap-alpha", command)
                    self.assertIn("0.2", command)
                    self.assertIn("--remap-optimize-pins", command)

    def test_recipe_keeps_the_requested_physical_device_for_its_lease(self):
        command = pnr_recipe.argv(
            "fes.menu", SLOT, SYNTH, 5, 1, output=OUTPUT
        )
        self.assertEqual(command[command.index("--gpu-device") + 1], "1")

    def test_recipe_build_maps_gpu_run_device_and_preserves_hip_mask(self):
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "amd:1",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value=None):
            command, environment = pnr_recipe.build(
                "fes.pong", SLOT, SYNTH, 1, 1, output=OUTPUT,
                env=leased_env,
            )
        device_index = command.index("--gpu-device")
        self.assertEqual(command[device_index + 1], "0")
        self.assertEqual(environment["GPU_RUN_DEVICE"], "0")
        self.assertEqual(environment["ROCR_VISIBLE_DEVICES"], "1,2")

    def test_unrecorded_option_and_environment_are_rejected(self):
        recipe = pnr_recipe.recipe_for("fes.pong")
        with self.assertRaises(pnr_recipe.RecipeError):
            pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT,
                            extra=("--unlisted-nextpnr-option",))
        with self.assertRaises(pnr_recipe.RecipeError):
            pnr_recipe.build("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT,
                             env={**ENV, "NEXTPNR_MISTRAL_PLACED_TIMEOUT": "30"})
        self.assertIn("CUDA_DEVICE_ORDER", pnr_recipe.recipe_for("fes.sms")["optional_env"])
        pnr_recipe.environment("fes.sms", {**ENV, "CUDA_DEVICE_ORDER": "PCI_BUS_ID"})

    def test_registered_build_accepts_the_exact_active_gpu_run_lease(self):
        recipe = pnr_recipe.recipe_for("fes.menu")
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "amd:1",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value=None) as resolve:
            command, environment = pnr_recipe.build(
                "fes.menu", SLOT, SYNTH, recipe["seeds"][0], 1,
                output=OUTPUT, env=leased_env)
        resolve.assert_called_once_with("amd", "1")
        self.assertEqual(environment, leased_env)
        device = command.index("--gpu-device")
        self.assertEqual(command[device + 1], "0")

    def test_amd_gpu_run_accepts_the_render_node_mapped_to_the_first_lease_id(self):
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "/dev/dri/renderD129",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value="/dev/dri/renderD129") as resolve:
            pnr_recipe.environment("fes.menu", leased_env, gpu=0)
        resolve.assert_called_once_with("amd", "1")

    def test_amd_gpu_run_rejects_a_render_node_for_another_leased_gpu(self):
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "/dev/dri/renderD128",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value="/dev/dri/renderD129"):
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "expected device"):
                pnr_recipe.environment("fes.menu", leased_env, gpu=0)

    def test_amd_gpu_run_rejects_the_fallback_marker_when_kfd_mapping_exists(self):
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "amd:1",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value="/dev/dri/renderD129") as resolve:
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "expected device"):
                pnr_recipe.environment("fes.menu", leased_env, gpu=0)
        resolve.assert_called_once_with("amd", "1")

    def test_amd_gpu_run_accepts_the_fallback_marker_without_kfd_topology(self):
        render_node_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "/dev/dri/renderD129",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        gpu_lease = pnr_recipe._gpu_lease_module()
        with patch.object(gpu_lease, "gpu_device_path", return_value=None) as resolve:
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "expected device"):
                pnr_recipe.environment("fes.menu", render_node_env, gpu=0)

            # gpu-run deliberately emits this physical-id marker when KFD has
            # no render-node mapping. It remains valid because it names the
            # first leased id exactly and contains no ambiguous device path.
            fallback_env = {
                **render_node_env,
                "GPU_RUN_EXPECTED_DEVICE": "amd:1",
            }
            pnr_recipe.environment("fes.menu", fallback_env, gpu=0)
        self.assertEqual(resolve.call_count, 2)
        resolve.assert_called_with("amd", "1")

    def test_ambient_gpu_overrides_without_a_lease_are_rejected(self):
        recipe = pnr_recipe.recipe_for("fes.menu")
        for name in (
            "FES_GPU_IDS", "GPU_RUN_DEVICE", "GPU_RUN_EXPECTED_DEVICE",
            "NEXTPNR_REQUIRE_GPU", "HIP_VISIBLE_DEVICES",
            "ROCR_VISIBLE_DEVICES", "CUDA_VISIBLE_DEVICES",
        ):
            with self.subTest(name=name), self.assertRaises(pnr_recipe.RecipeError):
                pnr_recipe.build(
                    "fes.menu", SLOT, SYNTH, recipe["seeds"][0], 0,
                    output=OUTPUT, env={**ENV, name: "0"})

    def test_gpu_run_lease_rejects_masks_or_markers_that_do_not_match(self):
        recipe = pnr_recipe.recipe_for("fes.menu")
        leased_env = {
            **ENV,
            "FES_GPU_IDS": "1,2",
            "GPU_RUN_DEVICE": "0",
            "GPU_RUN_EXPECTED_DEVICE": "amd:1",
            "NEXTPNR_REQUIRE_GPU": "1",
            "ROCR_VISIBLE_DEVICES": "1,2",
        }
        for changes in (
            {"ROCR_VISIBLE_DEVICES": "0"},
            {"GPU_RUN_EXPECTED_DEVICE": "amd:9"},
            {"GPU_RUN_DEVICE": "2"},
            {"HIP_VISIBLE_DEVICES": "1,2"},
            {"CUDA_VISIBLE_DEVICES": "1,2"},
        ):
            with self.subTest(changes=changes), self.assertRaises(pnr_recipe.RecipeError):
                pnr_recipe.build(
                    "fes.menu", SLOT, SYNTH, recipe["seeds"][0], 0,
                    output=OUTPUT, env={**leased_env, **changes})

    def test_search_producers_bind_to_the_registered_recipe(self):
        entries = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())["recipes"]
        for entry in entries:
            if entry.get("toolchain", "oss") != "oss" or entry["pnr"]["argv_style"] != "search":
                continue
            source = ROOT / "sources/misteross" / entry["producer_script"]
            tree = ast.parse(source.read_text())
            if entry["core_id"] == "fes.pcxt":
                calls = [node for node in ast.walk(tree)
                         if isinstance(node, ast.Call)
                         and isinstance(node.func, ast.Attribute)
                         and node.func.attr == "build"
                         and isinstance(node.func.value, ast.Name)
                         and node.func.value.id == "pnr_recipe"]
                self.assertEqual(len(calls), 1, entry["producer_script"])
                self.assertEqual(calls[0].args[0].value, entry["core_id"])
                self.assertTrue(any(keyword.arg == "env" for keyword in calls[0].keywords))
                continue
            calls = [node for node in ast.walk(tree)
                     if isinstance(node, ast.Call)
                     and isinstance(node.func, ast.Name)
                     and node.func.id == "route_after_synth"]
            values = [keyword.value for call in calls
                      for keyword in call.keywords if keyword.arg == "core_id"]
            self.assertTrue(calls, entry["producer_script"])
            self.assertEqual(len(values), len(calls), entry["producer_script"])
            if all(isinstance(value, ast.Constant) for value in values):
                self.assertEqual([value.value for value in values], [entry["core_id"]] * len(calls),
                                 entry["producer_script"])
            else:
                # A producer shared by several cores selects the core with a
                # recipe option and names each core it builds.
                self.assertIn("producer_options", entry, entry["producer_script"])
                self.assertIn(entry["core_id"], {node.value for node in ast.walk(tree)
                                                 if isinstance(node, ast.Constant)}, entry["producer_script"])
            for call in calls:
                env_kw = next((keyword for keyword in call.keywords if keyword.arg == "env"), None)
                env_dict = any(keyword.arg is None and isinstance(keyword.value, ast.Dict)
                               and any(isinstance(key, ast.Constant) and key.value == "env"
                                       for key in keyword.value.keys)
                               for keyword in call.keywords)
                self.assertTrue(env_kw is not None or env_dict, entry["producer_script"])

    def test_catch_direct_producer_uses_the_shared_builder(self):
        from scripts import build_fes_demo as demo
        commands = demo.build_commands(
            ROOT, "0" * 32,
            {"yosys": Path("/slot/bin/yosys"), "nextpnr-mistral": Path(SLOT)},
            audio=True, catch=True, gpu_device=0, env=ENV)
        recipe = pnr_recipe.recipe_for("fes.catch")
        self.assertEqual(list(commands[1]), _legacy_argv(
            "fes.catch", recipe, synth="build/fes-catch/synth.json", output="build/fes-catch"))


if __name__ == "__main__":
    unittest.main()
