#!/usr/bin/env python3
"""Choose the DE25 package builds for one exact head from the registered recipes.

Powerboat's HILQ seal runs this script from the exact head it is about to
build. It never compiles anything. It only reads `config/core-recipes.toml`
and asks each registered DE25 producer for its selection-only build record,
the same record `tools/identity.py --source` compares, so the producers
themselves decide which source changes move their packages.

Selections:

* `auto`: every registered DE25 recipe and model whose design identity or
  toolchain selection differs from the merge base (`--base-root`). A recipe
  or model the base does not have, or cannot describe, is selected. When no
  package input moved (for example a DE25 runtime or image change), MENU is
  built so the kit still runs an exact-head package.
* `all`: every registered DE25 recipe and model.
* `<core_id>[@<model>][,...]`: the named recipes. Without `@model`, every
  model the producer offers is built.

A producer with models offers them as `MODELS`, or as its machine's
descriptions (`MACHINE.descriptions`, the producer's own `--model` choices).
Models are separate packages: each has its own build record and BUILD_ID, and
the kit has to run each exact package. Every model of a recipe shares one
source closure, so a source change cannot be attributed to one model; the
identities decide, and in practice they move together.

Only registered DE25 Quartus Pro producers are accepted. Anything else is
refused, never mapped to a fallback.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from scripts.recipes import load_recipes  # noqa: E402

BOARD = 'de25-nano'
PRODUCER_DIR = 'boards/de25nano/producers/'
# Built when a DE25 change moves no package input. MENU is the board's boot
# core, present on every DE25 image, so it is always a valid exact-head run.
FALLBACK_CORE = 'fes.menu'
CORE_ID = re.compile(r'fes\.[a-z0-9][a-z0-9.-]*')
MODEL = re.compile(r'[A-Za-z0-9][A-Za-z0-9_.-]*')

# Runs in a fresh interpreter with only one checkout's `sources/misteross` on
# sys.path: the repository's `scripts` package and misteross's `scripts`
# package share a name, and a base checkout's producers must not mix with the
# head's modules.
PROBE = r'''
import importlib, json, sys
from pathlib import Path
source = Path(sys.argv[1]).resolve()
sys.path.insert(0, str(source))
EXECUTION = {"selection_only": True, "gpu_device": "lease", "toolchain": "quartus"}
def models(module):
    named = getattr(module, "MODELS", None)
    if named is None and getattr(module, "MACHINE", None) is not None:
        named = tuple(module.MACHINE.descriptions)
    named = list(named or ())
    if any(type(name) is not str or not name for name in named) or len(named) != len(set(named)):
        raise ValueError("producer models must be distinct nonempty names")
    return named or [None]
result = {}
for recipe in json.loads(sys.argv[2]):
    entry = {"models": None, "identities": {}, "error": None}
    try:
        if not (source / recipe["producer_script"]).is_file():
            raise LookupError("producer script is not in this checkout")
        module = importlib.import_module(recipe["producer_module"])
        names = models(module)
        repository, revision = module._require_clean_source(source, identity_version=2)
        tools = (module.selection_identities() if hasattr(module, "selection_identities")
                 else {name: "selection-only" for name in (
                     module.MACHINE.tool_set(module.MACHINE.spec(source))
                     if getattr(module, "MACHINE", None) is not None and hasattr(module.MACHINE, "tool_set")
                     else ("quartus", "qsys", "ipgenerate", "pfg"))})
        for name in names:
            extra = {} if name is None else {"model": name}
            record = json.loads(module.create_build_record(
                source, repository, revision, tools, execution=EXECUTION, identity_version=2, **extra))
            ids = record["identities"]
            entry["identities"][name or ""] = {"design": ids["design"], "toolchain": ids["toolchain"]}
        entry["models"] = names
    except Exception as error:  # reported per recipe; the caller decides
        entry["error"] = type(error).__name__ + ": " + str(error)
    result[recipe["core_id"]] = entry
print(json.dumps(result, sort_keys=True))
'''


class PlanError(ValueError):
    pass


def registered(path=None, *, allow_empty=False):
    """Return the registered DE25 Quartus Pro recipes, refusing malformed rows."""
    recipes = load_recipes(path, board=BOARD) if path else load_recipes(board=BOARD)
    result = {}
    for core_id, recipe in sorted(recipes.items()):
        quartus = recipe.quartus or {}
        script = recipe.producer_script
        if (recipe.board != BOARD or recipe.toolchain != 'quartus' or recipe.front_end not in ('quartus-pro', 'yosys')
                or quartus.get('edition') != 'pro' or quartus.get('device_family') != 'agilex5'
                or not script.startswith(PRODUCER_DIR) or not script.endswith('.py')
                or '..' in Path(script).parts or not CORE_ID.fullmatch(core_id)):
            raise PlanError(f'{core_id}: not a DE25 Quartus Pro producer recipe')
        result[core_id] = recipe
    if not result and not allow_empty:
        raise PlanError('no registered DE25 recipes')
    return result


def probe(root, recipes, *, python=sys.executable, timeout=900):
    """Describe each recipe's models and identities in one checkout."""
    source = Path(root).resolve() / 'sources/misteross'
    rows = [{'core_id': r.core_id, 'producer_module': r.producer_module, 'producer_script': r.producer_script}
            for r in recipes.values()]
    env = {key: value for key, value in os.environ.items() if not key.startswith('PYTHON')}
    done = subprocess.run([python, '-I', '-c', PROBE, str(source), json.dumps(rows)], cwd=source, env=env,
                          capture_output=True, text=True, timeout=timeout)
    if done.returncode:
        raise PlanError('producer probe failed in ' + str(root) + ': ' + done.stderr.strip()[-600:])
    return json.loads(done.stdout)


def parse_selection(text, recipes):
    """Return None for auto, else a list of (core_id, model or None)."""
    text = (text or '').strip()
    if text == 'auto':
        return None
    if text == 'all':
        return [(core_id, None) for core_id in recipes]
    chosen = []
    for item in text.split(','):
        core_id, _, model = item.strip().partition('@')
        if not CORE_ID.fullmatch(core_id) or (model and not MODEL.fullmatch(model)) or (_ and not model):
            raise PlanError(f'invalid DE25 selection {item!r}')
        if core_id not in recipes:
            raise PlanError(f'{core_id} is not a registered DE25 Quartus Pro recipe')
        if (core_id, model or None) in chosen:
            raise PlanError(f'{item} is selected twice')
        chosen.append((core_id, model or None))
    if not chosen:
        raise PlanError('empty DE25 selection')
    return chosen


def slug(core_id, model):
    return core_id if model is None else core_id + '-' + model


def plan(recipes, head, selection, base=None):
    """Turn probe results into ordered jobs; head errors always fail closed."""
    broken = {core_id: entry['error'] for core_id, entry in head.items() if entry.get('error')}
    jobs = []

    def add(core_id, model, reason):
        recipe = recipes[core_id]
        jobs.append({'core_id': core_id, 'model': model, 'slug': slug(core_id, model),
                     'producer_script': recipe.producer_script, 'reason': reason})

    def models_of(core_id):
        if core_id in broken:
            raise PlanError(f'{core_id}: producer cannot describe this head: {broken[core_id]}')
        return head[core_id]['models']

    if selection is None:
        if base is None:
            raise PlanError('auto selection needs the merge-base checkout')
        for core_id in recipes:
            for model in models_of(core_id):
                key = model or ''
                old = base.get(core_id) or {}
                before = (old.get('identities') or {}).get(key)
                after = head[core_id]['identities'][key]
                if old.get('error') or before is None:
                    add(core_id, model, 'new or undescribed at the merge base')
                elif before['design'] != after['design']:
                    add(core_id, model, 'design identity changed')
                elif before['toolchain'] != after['toolchain']:
                    add(core_id, model, 'toolchain selection changed')
        if not jobs:
            for model in models_of(FALLBACK_CORE):
                add(FALLBACK_CORE, model, 'no DE25 package input changed; exact-head MENU run')
        return jobs
    for core_id, model in selection:
        offered = models_of(core_id)
        if model is not None and model not in offered:
            raise PlanError(f'{core_id} has no model {model}; it offers {", ".join(map(str, offered))}')
        for name in ([model] if model is not None else offered):
            add(core_id, name, 'selected explicitly')
    return jobs


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--select', required=True, help='auto, all, or core_id[@model][,...]')
    parser.add_argument('--base-root', type=Path, help='clean checkout of the merge base (auto only)')
    parser.add_argument('--output-dir', type=Path, required=True,
                        help='writes de25-plan.json and de25-jobs.tsv (slug, producer, model or -, core) here')
    args = parser.parse_args(argv)
    try:
        recipes = registered()
        selection = parse_selection(args.select, recipes)
        if selection is None and args.base_root is None:
            raise PlanError('auto selection needs --base-root')
        if selection is not None and args.base_root is not None:
            raise PlanError('--base-root is only for auto selection')
        head = probe(ROOT, recipes)
        if selection is None:
            # Producer code can predate its DE25 recipe registration. Compare
            # only recipes the base actually registered, not every head recipe.
            base_recipes = registered(args.base_root / 'config/core-recipes.toml', allow_empty=True)
            base = probe(args.base_root, base_recipes)
        else:
            base = None
        jobs = plan(recipes, head, selection, base)
    except (PlanError, ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, 'de25-seal-plan: ' + str(error) + '\n')
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / 'de25-plan.json').write_text(json.dumps(
        {'format': 1, 'select': args.select, 'jobs': jobs,
         'head': head, 'base': base}, indent=2, sort_keys=True) + '\n')
    (args.output_dir / 'de25-jobs.tsv').write_text(''.join(
        '\t'.join((job['slug'], job['producer_script'], job['model'] or '-', job['core_id'])) + '\n'
        for job in jobs))
    for job in jobs:
        print(f"{job['slug']}: {job['producer_script']}" + (f" --model {job['model']}" if job['model'] else '')
              + f" ({job['reason']})")


if __name__ == '__main__':
    main()
