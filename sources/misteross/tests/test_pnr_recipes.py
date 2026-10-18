"""Per-core P&R recipes pin each producer's effective nextpnr invocation."""
import ast
import hashlib
import json
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
LOCK_SHA256 = "dc1617a1f2e33e2a13d20cec630b1f1b1fb1729fb8eeab61ac7c26ffb00265a3"
FLAGS_SHA256 = "6d60ed8e921f893f4fc3db7faf7c71473e493c99fee7f503c6f96cc38fd20c3f"

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
        command.extend(("--router", recipe["router"], "--gpu-device", str(gpu),
                        "--sdc", recipe["sdc"], "--freq", recipe["freq"]))
    else:
        command.extend(("--sdc", recipe["sdc"], "--freq", recipe["freq"],
                        "--seed", str(seed), "--router", recipe["router"],
                        "--rbf", output_files["--rbf"], "--compress-rbf",
                        "--write", output_files["--write"],
                        "--report", output_files["--report"],
                        "--detailed-timing-report", "--gpu-device", str(gpu)))
    return command


class PnrRecipeTests(unittest.TestCase):
    def test_registered_locks_are_identical_to_the_pinned_global_lock(self):
        base = ROOT / "sources/misteross/toolchain.lock"
        base_bytes = base.read_bytes()
        self.assertEqual(hashlib.sha256(base_bytes).hexdigest(), LOCK_SHA256)
        from scripts.lockfile import load_lock
        self.assertEqual(load_lock(base)["nextpnr"].commit, pnr_recipe.PINNED_NEXTPNR)
        registry = tomllib.loads((ROOT / "config/core-recipes.toml").read_text())
        self.assertEqual(set(GOLDENS), {entry["core_id"] for entry in registry["recipes"]})
        for entry in registry["recipes"]:
            with self.subTest(core=entry["core_id"]):
                lock = ROOT / "sources/misteross" / entry["lock_path"]
                self.assertEqual(lock.read_bytes(), base_bytes)

    def test_recipe_and_legacy_argv_goldens_for_every_registered_producer(self):
        for core_id, (recipe_sha, argv_sha) in GOLDENS.items():
            with self.subTest(core=core_id):
                recipe = pnr_recipe.recipe_for(core_id)
                self.assertEqual(pnr_recipe.recipe_digest(core_id), recipe_sha)
                flags_sha = hashlib.sha256(json.dumps(
                    recipe["flags"], sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                self.assertEqual(flags_sha, FLAGS_SHA256)
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

    def test_menu_argv_uses_the_leased_gpu_device(self):
        command = pnr_recipe.argv(
            "fes.menu", SLOT, SYNTH, 5, 1, output=OUTPUT
        )
        device_index = command.index("--gpu-device")
        self.assertEqual(command[device_index + 1], "1")

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
