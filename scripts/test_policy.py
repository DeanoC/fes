"""Shared routine/extended test-mode policy for CI and the local affected runner.

Routine runs execute the fast regression and concurrency-focused checks.
Full-device video producer/publication and real-media suites run only when
their inputs change or on scheduled/manual full runs. The exhaustive race pass
runs weekly/manual or when shared-contract/unknown inputs force it.
"""
from fnmatch import fnmatchcase
from pathlib import Path

TEST_MODE_KEYS = ('video', 'media', 'full_race')

# Video producer, package-admission and host library consumer inputs. Changes
# here re-include the real-device video suites in the parent run.
VIDEO_FILES = frozenset({
    'scripts/factory_video_parts.py', 'scripts/core_catalog.py',
    'scripts/core_dev_accept.py', 'scripts/core_dev.py',
    'scripts/package_acceptance_isolated.py',
    'scripts/recipes.py', 'scripts/bundle.py',
    'scripts/artifact_cache.py', 'scripts/inputs.py', 'scripts/build.py',
    'scripts/native_dev.py', 'scripts/environment.py',
    'config/core-recipes.toml',
    'tests/test_factory_video_parts.py',
    'tests/test_factory_video_publication.py',
})
# misteross is narrowed to the producer software, toolchain pins and the core
# trees the Coleco/ST video-part recipes hash; its tests, docs, Makefile,
# experiments and unrelated cores stay routine. Scheduled runs remain full.
VIDEO_ROOTS = ('sources/misteross/scripts', 'sources/misteross/expansion',
               'sources/misteross/toolchain.lock', 'sources/misteross/toolchains',
               'sources/misteross/cores/fes-common', 'sources/misteross/cores/fes-coleco',
               'sources/misteross/cores/fes-atari-st', 'sources/misteross/cores/fes-c64',
               'sources/misteross/cores/fes-ramtest', 'sources/misteross/cores/fes-zx81',
               'sources/FogCast/corepackage', 'sources/FogCast/corecatalog')
# Host consumers of package/video admission; their *_test.go files stay routine.
VIDEO_CONSUMER_ROOTS = ('sources/FogCast/catalog', 'sources/FogCast/fogcast')

# Image, media, platform and container assembly inputs plus their container
# drivers. Changes here re-include the real-media suites in the parent run.
MEDIA_FILES = frozenset({
    'scripts/platform.py', 'scripts/image_toolchain.py', 'scripts/build.py',
    'scripts/inputs.py', 'scripts/native_dev.py', 'scripts/environment.py',
    'boot-media.lock.toml',
    'tests/test_media_image.py', 'tests/test_appliance.py',
    'tests/test_appliance_media.py',
})
MEDIA_ROOTS = ('image', 'platform', 'containers', 'profiles',
               'sources/FogCast/appliance', 'sources/FogCast/cmd/target-image-lock')
MEDIA_SCRIPT_PATTERNS = ('scripts/media*.py', 'scripts/appliance*.py')

# Dependency changes can alter both consumer and assembly behavior.
DEPENDENCY_FILES = frozenset({'sources/FogCast/go.mod', 'sources/FogCast/go.sum'})

# Everything beneath these roots is recognized even when not relevant, so new
# helper files do not silently classify as unknown. Root-level Make/docs are
# recognized as well; any AGENTS.md is behavioral guidance and stays broad.
RECOGNIZED_ROOTS = ('scripts', 'tests', 'config', '.github',
                    'sources/FogCast', 'sources/misteross',
                    'sources/mister-packages', 'sources/libmister-runtime')
RECOGNIZED_FILES = frozenset({'Makefile', 'README.md'})

# Supplemental concurrency-focused instrumentation for the two host packages
# whose all-tests race pass is expensive. This is deliberately NOT exhaustive:
# every assertion still runs non-race; exhaustive instrumentation is full mode.
HOST_RACE_FOCUS = ('(Concurrent|Concurrency|Race|Cancel|Cancellation|Barrier|'
                   'Lifecycle|Session|Stop|Target|Lock|Queued|Queue|Drain|Lease|'
                   'Discovery|Watch|Input|Mesh)')

FOGCAST_MODULE = 'sources/FogCast'
APPLIANCE_MODULE = 'sources/FogCast/appliance'
EXPANSION_MODULE = 'sources/misteross/expansion'
EXPENSIVE_HOST_PACKAGES = ('github.com/DeanoC/FogCast/fogcast',
                           'github.com/DeanoC/FogCast/corepackage')


def _under(path, root):
    return path == root or path.startswith(root + '/')


def _documentation(path):
    # Same semantics as scripts.affected.documentation; AGENTS.md is behavioral.
    parts = Path(path).parts
    return ('docs' in parts or path.endswith('.md')) and Path(path).name != 'AGENTS.md'


def _classify(path):
    if path == '<new-branch>' or Path(path).name == 'AGENTS.md':
        return dict.fromkeys(TEST_MODE_KEYS, True)
    if _under(path, 'sources/mister-packages'):
        # Shared contracts touch every consumer, including full race coverage.
        return dict.fromkeys(TEST_MODE_KEYS, True)
    video = (path in VIDEO_FILES or
             any(_under(path, root) for root in VIDEO_ROOTS) or
             (any(_under(path, root) for root in VIDEO_CONSUMER_ROOTS)
              and path.endswith('.go') and not path.endswith('_test.go')) or
             path in DEPENDENCY_FILES)
    media = (path in MEDIA_FILES or
             any(_under(path, root) for root in MEDIA_ROOTS) or
             any(fnmatchcase(path, pattern) for pattern in MEDIA_SCRIPT_PATTERNS) or
             path in DEPENDENCY_FILES)
    if video or media:
        return {'video': video, 'media': media, 'full_race': False}
    if (any(_under(path, root) for root in RECOGNIZED_ROOTS)
            or path in RECOGNIZED_FILES):
        return dict.fromkeys(TEST_MODE_KEYS, False)
    return dict.fromkeys(TEST_MODE_KEYS, True)


def select_test_modes(paths):
    """Union of per-path modes; renames contribute both old and new ownership."""
    modes = dict.fromkeys(TEST_MODE_KEYS, False)
    for path in sorted(set(paths)):
        if _documentation(path):
            continue
        for key, value in _classify(path).items():
            modes[key] = modes[key] or value
    return modes


def validate_test_modes(modes, lanes=None):
    """Modes require exact boolean keys; lane-masked modes must be consistent."""
    if not isinstance(modes, dict) or set(modes) != set(TEST_MODE_KEYS):
        raise ValueError('test_modes must contain exactly the known mode keys')
    if any(type(value) is not bool for value in modes.values()):
        raise ValueError('test_modes values must be booleans')
    if lanes is not None:
        if not isinstance(lanes, dict):
            raise ValueError('lanes must be a mapping when supplied')
        if (modes['video'] or modes['media']) and lanes.get('parent') is not True:
            raise ValueError('video/media test modes require the parent lane')
        if modes['full_race'] and lanes.get('host') is not True:
            raise ValueError('full_race test mode requires the host lane')


def host_test_commands(full=False, packages=None):
    """Ordered host Go commands; routine splits cost, --full races everything."""
    if type(full) is not bool:
        raise ValueError('full must be a boolean')
    race = ['go', 'test', '-race', '-timeout', '30m', './...']
    if full:
        return [
            {'cwd': FOGCAST_MODULE, 'label': 'host exhaustive race suite', 'argv': list(race)},
            {'cwd': APPLIANCE_MODULE, 'label': 'appliance exhaustive race suite', 'argv': list(race)},
            {'cwd': EXPANSION_MODULE, 'label': 'expansion exhaustive race suite', 'argv': list(race)},
        ]
    if not packages:
        raise ValueError('routine host tests require the go-list package set')
    ordinary = [pkg for pkg in packages if pkg not in EXPENSIVE_HOST_PACKAGES]
    if not ordinary:
        raise ValueError('routine host tests require ordinary packages beyond the '
                         'race-focused set')
    short_race = ['go', 'test', '-race', '-short', '-timeout', '30m']
    return [
        # Every functional case runs exactly once: the two race-expensive
        # packages non-race here, all ordinary packages under race next.
        {'cwd': FOGCAST_MODULE, 'label': 'host artifact functional suites',
         'argv': ['go', 'test', '-short', '-timeout', '30m', './fogcast', './corepackage']},
        {'cwd': FOGCAST_MODULE, 'label': 'host race suite excluding race-expensive packages',
         'argv': [*short_race, *ordinary]},
        {'cwd': FOGCAST_MODULE, 'label': 'host concurrency-focused race checks',
         'argv': [*short_race, '-run', HOST_RACE_FOCUS, './fogcast', './corepackage']},
        {'cwd': APPLIANCE_MODULE, 'label': 'appliance race suite',
         'argv': [*short_race, './...']},
        {'cwd': EXPANSION_MODULE, 'label': 'expansion functional suite',
         'argv': ['go', 'test', '-short', '-timeout', '30m', './...']},
        {'cwd': EXPANSION_MODULE, 'label': 'expansion concurrency-focused race checks',
         'argv': [*short_race, '-run', HOST_RACE_FOCUS, './...']},
    ]
