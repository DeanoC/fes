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
    "fes.menu": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.catch": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.pong": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.zx81": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.coleco": "ba411cc51b40cf31a606762253d5e121bf16486e293276d06c84ca5702693a7f",
    "fes.sms": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.sg1000": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.apple2": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.c64": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.spectrum": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.riscv": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.ramtest": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
    "fes.atari-st": "6adb0ae08c2c24f117e9bf3eaf0fac0e14287b58fc480cd38950db5cf0c31ca8",
}
# sha256 of each classified nextpnr pin's option defaults. No recipe records a
# nondefault option yet, so each flags table hashes to its own lock's entry.
FLAGS_SHA256 = {
    pnr_recipe.NEXTPNR_9128800F: "6d60ed8e921f893f4fc3db7faf7c71473e493c99fee7f503c6f96cc38fd20c3f",
    pnr_recipe.NEXTPNR_552BA246: "38d02f4b8db3804837214ed9227bbac0b2b92f4687a3bb31a9d88d48d30ba5e6",
}
# The options nextpnr 552ba246 adds over 9128800f, from a diff of the two
# binaries' --help option lists.
ADDED_552BA246 = {
    "arrival_pin_assignment", "isolated_lut_placement", "critical_cohort_budget",
    "critical_cohort_report", "critical_cohort_model_out", "gpu_required",
    "mistral_dsp_signed_lanes", "router2_profile", "set",
}

# Stable digest of each complete recipe, plus the old producer command's
# normalized argv digest (slot, synth, output and GPU are fixed test values).
# The search vectors follow the former search_placer_qor._run_nextpnr order;
# Catch follows build_fes_demo.build_commands plus its former GPU suffix.
GOLDENS = {
    "fes.menu": ("2dab30da3b734c32b1ff52bfd0cdc035cca4bfe1fcc3989461bf3432be25e958", "b840cd3425c9582751f1a7847d988db554f889f0a27f7a234ad25077ccd473df"),
    "fes.catch": ("2ca8900a129a4b954198e400c5939b8ff28ec3043302ce111f5fabfc9b5c5431", "2e741531aae85de2e7a8796342603dd261b4a025ae564f0575ef1b9caa1dd863"),
    "fes.pong": ("aae5ea738224669955f178252e48606f44b6d46dceb0767bd53bab43ce4e208b", "213407fd4fd731c94a022df86b5460388dd691a54d5f7cb390cd2e5c0e80126a"),
    "fes.zx81": ("ede61b2a6cad88cd5d1b4fe75495914d91873d6072f52ddb03acf44e92c3606a", "1c0a67c9024ace96c56d7270c64a43b562745dd12696d5f8614723a4de083c70"),
    "fes.coleco": ("36aea678cbf9d9df26f0b7bfd6dc409c96c36c7c7c740d737c50b49688819c90", "93de0822ff64f833c8e571cf08b0b7d312f005bde176ad43d308c3f7ec2142af"),
    "fes.sms": ("aaa419748321cab16db125a53dd74b54c0240bb5a021b32a3db417b56543bfe5", "9f5b6f7ee3c682d7927ee0ceb03bd307f891431c209d88aa12ac20004cf97126"),
    "fes.sg1000": ("0faae72f9ba02b06832ddf7b537823c87bc61acf31672e43bb9da36a6abd66e1", "abe6012bd8052f8814e813182b95509c08ff3c906e3f926a1c0edb49f4982b85"),
    "fes.apple2": ("57406311d69ef2e121f002c7cf0afbbbfb7b1c26d114acf88e1664a4b482ddd3", "66f49fb8cf973ded993aff3540cba1c3dc01c5d58c2e1c8353eb10003aaee65b"),
    "fes.c64": ("6373cfbc3a68996ad89efbba3af45fcf0d477466e1b4a45eca5b8c38ebd5ff37", "86b9d7ef13b01b2ff5c1f2cc2e979d117e8e1470aeaf88ce85483163048b6134"),
    "fes.spectrum": ("780de931801b08dcc4df525763a573165a6baf3a3241c6161fd8cdc6d312938a", "df1e8572d39b1baa8febcd96bee550765c2d5451b27baea60682e52a5d87f21d"),
    "fes.riscv": ("aae5ea738224669955f178252e48606f44b6d46dceb0767bd53bab43ce4e208b", "213407fd4fd731c94a022df86b5460388dd691a54d5f7cb390cd2e5c0e80126a"),
    "fes.ramtest": ("ede6474bf85ce45e0631d4edb375bc37ab9c1161aae0a265f3c5c34920947781", "ded346bda6c66889e7f08e994eb82964871784b747cbc24159782b25406da42e"),
    "fes.atari-st": ("fb2168507f5b08a1112ad6f5e90fd48b6ea2b69273073f7f7c32ef882133d05a", "31597590be7e868a81521a62ceaa190e7af72dfffc106b9c5898646ea56e040a"),
}

# Hashes of the frozen legacy producer argv matrix for every registered
# seed/weight pair. These are independent of the recipe hash and protect the
# migrated argv layout and each production combination.
ARGV_MATRIX_GOLDENS = {
    "fes.menu": "d356ffdf406928e2d588be6e41f92ef4a51dd6b02fce5e660f123c97d2cd7bcb",
    "fes.catch": "79b8ac93741f11b180132307a0b3261e6e41839e89240c6363f0087e03e75bc4",
    "fes.pong": "177f0476e55e89727fd172de68e2a08b4c684e2a0c99184e4330acfaa4efa85c",
    "fes.zx81": "3e247618b00da0b097ab09f7851f721127e3598df58d7c3a7ca006c0cc032749",
    "fes.coleco": "7bde18f0254f40a680638a4476839141eaf65ffbf2a0bf7069cec059332f9439",
    "fes.sms": "f1c5c9118f716306b8493bccb3d24c1bec6ce5d20ea4c942b618c7aa728499bb",
    "fes.sg1000": "e67b5e8e63cf0c3ba560d58c04167361d95e2cf3d653d1d1ff42524fdea9d7fb",
    "fes.apple2": "9810f5f4a5870d854ad0e9c5d364d587e95f75ae9d7316f2772683ee1f8395fb",
    "fes.c64": "ed1c7847679e3d9d18654e7e32892c2e585cc1317dd058a865668bb8860223f1",
    "fes.spectrum": "e3836e041f0ebefcf4eeff1c8969849321ce5f9ada1ed30e499cc2064febe060",
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
    return command


def _toml(value):
    if type(value) is bool:
        return "true" if value else "false"
    if isinstance(value, list):
        return "[" + ", ".join(json.dumps(item) for item in value) + "]"
    return json.dumps(value)


def _pong_on_nextpnr(directory, pin, *, schema=None, omit=(), values=None):
    """Copy the registry and every lock to a temporary FES tree and pin only
    Pong's lock to nextpnr *pin*. Pong's flags table becomes the defaults of
    *schema* (default *pin*) less *omit*, with *values* applied, so the result
    does not depend on Pong's current lock or recipe."""
    root = Path(directory)
    registry = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())
    for entry in registry["recipes"]:
        lock = root / "sources/misteross" / entry["lock_path"]
        lock.parent.mkdir(parents=True, exist_ok=True)
        lock.write_bytes((ROOT / "sources/misteross" / entry["lock_path"]).read_bytes())
    lock = root / "sources/misteross/cores/fes-pong/toolchain.lock"
    text, count = re.subn(r'(\[tool\.nextpnr\][^\[]*?commit = ")[0-9a-f]{40}"',
                          rf'\g<1>{pin}"', lock.read_text())
    assert count == 1
    lock.write_text(text)
    flags = dict(pnr_recipe.NEXTPNR_PINS[schema or pin], **(values or {}))
    table = "".join(f"{name} = {_toml(value)}\n" for name, value in flags.items() if name not in omit)
    source = (ROOT / "config/core-recipes.toml").read_text(encoding="utf-8")
    start = source.index('core_id = "fes.pong"')
    end = source.index("[[recipes]]", start + 1)
    header = "[recipes.pnr.flags]\n"
    first = source.index(header, start, end) + len(header)
    assert "\n[" not in source[first:end]
    (root / "config").mkdir()
    registry_path = root / "config/core-recipes.toml"
    registry_path.write_text(source[:first] + table + "\n" + source[end:], encoding="utf-8")
    return patch.multiple(pnr_recipe, FES_ROOT=root, REGISTRY=registry_path)


class PnrRecipeTests(unittest.TestCase):
    def test_every_registered_lock_pins_its_recipes_classified_nextpnr(self):
        from scripts.lockfile import load_lock
        registry = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())
        self.assertEqual(set(GOLDENS), {entry["core_id"] for entry in registry["recipes"]})
        self.assertEqual(set(LOCK_SHA256), set(GOLDENS))
        for entry in registry["recipes"]:
            core_id = entry["core_id"]
            with self.subTest(core=core_id):
                lock = ROOT / "sources/misteross" / entry["lock_path"]
                self.assertEqual(lock, pnr_recipe.lock_path(ROOT / "sources/misteross", core_id))
                self.assertEqual(hashlib.sha256(lock.read_bytes()).hexdigest(), LOCK_SHA256[core_id])
                pin = load_lock(lock)["nextpnr"].commit
                self.assertEqual(pnr_recipe.nextpnr_pin(core_id), pin)
                self.assertEqual(set(pnr_recipe.recipe_for(core_id)["flags"]),
                                 set(pnr_recipe.NEXTPNR_PINS[pin]))

    def test_552ba246_classifies_exactly_its_added_options(self):
        old = pnr_recipe.NEXTPNR_PINS[pnr_recipe.NEXTPNR_9128800F]
        new = pnr_recipe.NEXTPNR_PINS[pnr_recipe.NEXTPNR_552BA246]
        self.assertEqual(set(new) - set(old), ADDED_552BA246)
        self.assertEqual({name: new[name] for name in old}, dict(old))
        self.assertEqual(set(pnr_recipe.NEXTPNR_PINS), set(FLAGS_SHA256))
        for pin, defaults in pnr_recipe.NEXTPNR_PINS.items():
            self.assertEqual(hashlib.sha256(json.dumps(
                dict(defaults), sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
                FLAGS_SHA256[pin])

    def test_a_552ba246_recipe_must_set_every_added_option(self):
        pin = pnr_recipe.NEXTPNR_552BA246
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, pin, schema=pnr_recipe.NEXTPNR_9128800F):
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "missing .*arrival_pin_assignment"):
                pnr_recipe.recipe_for("fes.pong")
        for name in sorted(ADDED_552BA246):
            with self.subTest(omitted=name), tempfile.TemporaryDirectory() as directory, \
                    _pong_on_nextpnr(directory, pin, omit=(name,)):
                with self.assertRaisesRegex(pnr_recipe.RecipeError, f"missing \\['{name}'\\]"):
                    pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT)
        menu = pnr_recipe.nextpnr_pin("fes.menu")
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(directory, pin):
            self.assertEqual(pnr_recipe.nextpnr_pin("fes.pong"), pin)
            self.assertEqual(pnr_recipe.nextpnr_pin("fes.menu"), menu)
            recipe = pnr_recipe.recipe_for("fes.pong")
            # Defaults reach no argv, so the added options leave it unchanged.
            self.assertEqual(list(pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT)),
                             _legacy_argv("fes.pong", recipe, seed=1))

    def test_9128800f_recipes_reject_options_their_nextpnr_lacks(self):
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, pnr_recipe.NEXTPNR_9128800F, values={"arrival_pin_assignment": False}):
            with self.assertRaisesRegex(pnr_recipe.RecipeError, r"unknown \['arrival_pin_assignment'\]"):
                pnr_recipe.recipe_for("fes.pong")

    def test_a_lock_on_an_unclassified_nextpnr_is_rejected(self):
        unclassified = "e2f1e02943f9292f68a8a551a6a934d5fb0f52c7"
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, unclassified, schema=pnr_recipe.NEXTPNR_552BA246):
            with self.assertRaisesRegex(pnr_recipe.RecipeError, "no classified option schema"):
                pnr_recipe.recipe_digest("fes.menu")

    def test_552ba246_options_reach_argv_when_set(self):
        values = {
            "arrival_pin_assignment": True, "isolated_lut_placement": True,
            "critical_cohort_budget": 4, "critical_cohort_report": "prior/timing.json",
            "gpu_required": True, "mistral_dsp_signed_lanes": True,
            "router2_profile": "profile.json",
            "set": ["gpurouter/repairBbExpand=4", "placerHeap/netShareWeight=0.5"],
        }
        with tempfile.TemporaryDirectory() as directory, \
                _pong_on_nextpnr(directory, pnr_recipe.NEXTPNR_552BA246, values=values):
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
                    _pong_on_nextpnr(directory, pnr_recipe.NEXTPNR_552BA246, values=values):
                with self.assertRaisesRegex(pnr_recipe.RecipeError, "invalid nextpnr option value"):
                    pnr_recipe.recipe_for("fes.pong")
        # The unrepaired baseline may export its calibration model.
        with tempfile.TemporaryDirectory() as directory, _pong_on_nextpnr(
                directory, pnr_recipe.NEXTPNR_552BA246, values={"critical_cohort_model_out": "model.json"}):
            command = pnr_recipe.argv("fes.pong", SLOT, SYNTH, 1, 0, output=OUTPUT)
        self.assertEqual(command[-2:], ("--critical-cohort-model-out", "model.json"))

    def test_recipe_and_legacy_argv_goldens_for_every_registered_producer(self):
        for core_id, (recipe_sha, argv_sha) in GOLDENS.items():
            with self.subTest(core=core_id):
                recipe = pnr_recipe.recipe_for(core_id)
                self.assertEqual(pnr_recipe.recipe_digest(core_id), recipe_sha)
                flags_sha = hashlib.sha256(json.dumps(
                    recipe["flags"], sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                self.assertEqual(flags_sha, FLAGS_SHA256[pnr_recipe.nextpnr_pin(core_id)])
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
            if entry["pnr"]["argv_style"] != "search":
                continue
            source = ROOT / "sources/misteross" / entry["producer_script"]
            tree = ast.parse(source.read_text())
            calls = [node for node in ast.walk(tree)
                     if isinstance(node, ast.Call)
                     and isinstance(node.func, ast.Name)
                     and node.func.id == "route_after_synth"]
            ids = [ast.literal_eval(keyword.value) for call in calls
                   for keyword in call.keywords if keyword.arg == "core_id"]
            self.assertTrue(calls, entry["producer_script"])
            self.assertEqual(ids, [entry["core_id"]] * len(calls), entry["producer_script"])
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
