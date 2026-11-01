"""Parent regression groups for reviewed component and parent inputs.

Shared implementation, configuration and unknown inputs remain full-suite. This
is an explicit integration boundary, not an inferred Python dependency graph.
Unassigned new test modules always run until their ownership is reviewed.
"""
import os
from pathlib import Path, PurePosixPath
import subprocess

try:
    from scripts import host_affected
except ImportError:
    import host_affected


def names(text):
    return frozenset('test_' + name for name in text.split())


GROUPS = {
    'policy': names('''affected check_deadcode check_diff check_doc_links ci_gate
        ci_workflow consistency coordination generate hil_plan host_affected
        image_assembly parent_affected test_changed test_policy
        v1_firmware_licensing_doc'''),
    'build': names('''build_diagnostics dev_snapshot import_modules inputs
        image_artifacts kit_software module_sources native_dev receipt source_status staging status'''),
    'packages': names('''artifact_cache bundle core_build core_catalog core_dev
        core_dev_accept core_key_nightly factory_video_parts factory_video_publication
        optional_cores recipes rom_package_pipeline'''),
    'media': names('''appliance appliance_expand_dry_run appliance_media block_probe
        image_passes image_toolchain launcher_provision media media_host media_image
        media_inputs platform'''),
    'acceptance': names('''atari_st_demo_input atari_st_demo_media atari_st_disk_diagnostic
        atari_st_io_diagnostic atari_st_memory_diagnostic atari_st_ram_write_trace
        atari_st_raster_trace atari_st_rendered_capture frame_trace_summary gpu_lease
        input_diagnostic kit_software_acceptance package_acceptance
        package_acceptance_isolated ramtest_notices target_acceptance'''),
}

# These assembly entrypoints and their subprocess helpers are consumed by the
# media suites. Build/policy coverage is always retained (including recipe hashes).
# Do not infer this table from matching filenames: imports are not the only inputs.
PARENT_INPUT_GROUPS = {
    'scripts/' + name + '.py': frozenset({'media'})
    for name in ('appliance', 'appliance_inside', 'appliance_media',
                 'appliance_media_inside', 'appliance_expand_dry_run')
}

# Reviewed lane-planner, gate and diff-check inputs affect CI policy and its
# build/status consumers, not package/media implementation. Parent selection
# and shared mode policy deliberately remain unlisted and select every group.
PARENT_INPUT_GROUPS.update({
    path: frozenset({'policy'}) for path in (
        'scripts/affected.py', 'scripts/ci_gate.py', 'scripts/check_diff.py',
        'tests/test_affected.py', 'tests/test_ci_gate.py',
        'tests/test_check_diff.py', 'tests/test_test_changed.py',
        'tests/test_v1_firmware_licensing_doc.py',
    )
})

# Test modules may supply fixtures to another group. Keep their consumers too.
# Other current cross-test imports stay within a group or in baseline policy.
TEST_CONSUMER_GROUPS = {
    'test_package_acceptance': frozenset({'packages'}),
}

# Modules that assert against repository guides, rather than temporary Markdown
# fixtures. Retain them for mixed code/docs edits without selecting all packages.
DOCUMENTATION_MODULES = names('''check_doc_links image_assembly consistency
    coordination core_build bundle v1_firmware_licensing_doc''')


def documentation(path):
    # Same classification as the outer affected planner.
    return 'docs' in PurePosixPath(path).parts or path.endswith('.md')


def parent_groups(path):
    if path in PARENT_INPUT_GROUPS:
        return PARENT_INPUT_GROUPS[path]
    for group, modules in GROUPS.items():
        # Unreviewed policy tests keep full coverage of the selector itself.
        if group != 'policy':
            for module in modules:
                if path == 'tests/' + module + '.py':
                    return {group} | TEST_CONSUMER_GROUPS.get(module, set())
    return None


def under(path, prefix):
    return path == prefix or path.startswith(prefix + '/')


def verified_go_test_only(root, paths):
    """Prove existing Go test ownership before omitting parent consumers."""
    changed = host_affected.go_inputs(paths)
    if not changed or any(not path.endswith('_test.go') or
                          not under(path, 'sources/FogCast') for path in changed):
        return False
    root = Path(root).resolve()
    files = {root / path for path in changed}
    if any(not path.is_file() or path.resolve() != path for path in files):
        return False
    environment = dict(os.environ)
    for key in ('GOOS', 'GOARCH', 'GOARM'):
        environment.pop(key, None)
    try:
        records = host_affected.listing(root, environment)
        if host_affected.select(paths, records, root) is None:
            return False
        listed = set()
        for record in records:
            directory = Path(record['Dir']).resolve()
            # Directory embeds may cover files absent from the current listing.
            # Keep all existing consumers whenever any embed could include one.
            if (any(record.get(field) for field in host_affected.EMBED_FIELDS) and
                    any(path.is_relative_to(directory) for path in files)):
                return False
            listed.update(directory / name for key in ('TestGoFiles', 'XTestGoFiles')
                          for name in record.get(key, []))
        return files <= listed
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        return False


def select_groups(paths, video=False, media=False, root=None):
    """Return groups, or None for complete coverage. Union mixed owners."""
    if paths is None or not paths:
        return None
    groups = {'policy', 'build'}
    for path in paths:
        parts = PurePosixPath(path).parts
        if '..' in parts or path.startswith('/'):
            return None
        # Documentation adds its readers at module selection, not every group.
        if documentation(path):
            continue
        parent = parent_groups(path)
        if parent is not None:
            groups.update(parent)
        elif under(path, 'sources/FogCast'):
            known = ('ui', 'resources', 'host', 'hostclient', 'kitlease', 'librarymedia',
                     'libraryuser', 'protocol', 'remoteinput', 'romsource', 'targetclient',
                     'appliance', 'corepackage', 'corecatalog', 'catalog', 'fogcast',
                     'internal', 'cmd')
            if not any(under(path, 'sources/FogCast/' + area) for area in known):
                return None
            # Build definitions/dependencies can affect any integration group.
            if (PurePosixPath(path).name in ('Makefile', 'go.mod', 'go.sum') or
                    under(path, 'sources/FogCast/scripts')):
                return None
            # Parent media tools consume the appliance module and image selector.
            if (under(path, 'sources/FogCast/appliance') or
                    under(path, 'sources/FogCast/cmd/target-image-lock') or
                    under(path, 'sources/FogCast/internal/targetimage')):
                groups.update(('media', 'packages', 'acceptance'))
            # Package admission and launch consumers are exercised by the
            # publication and acceptance suites as well as the Go tests.
            if any(under(path, 'sources/FogCast/' + area) for area in
                   ('corepackage', 'corecatalog', 'catalog', 'fogcast', 'internal', 'cmd',
                    'host', 'hostclient', 'kitlease', 'librarymedia', 'libraryuser', 'protocol',
                    'remoteinput', 'romsource', 'targetclient')):
                groups.update(('packages', 'acceptance'))
        elif under(path, 'sources/libmister-runtime'):
            groups.update(('packages', 'acceptance'))
        elif under(path, 'sources/misteross'):
            groups.update(('packages', 'acceptance'))
        else:
            # Includes shared contracts, root build/config inputs, parent
            # unreviewed scripts/tests, image/platform and newly introduced modules.
            return None
    if (root is not None and not video and not media and 'media' not in groups
            and verified_go_test_only(root, paths)):
        return {'policy', 'build'}
    if video:
        groups.add('packages')
    if media:
        groups.add('media')
    return groups


def selected_modules(groups, available, paths=None):
    if groups is None:
        return set(available)
    assigned = set().union(*GROUPS.values())
    # A new/unclassified test is never silently omitted.
    readers = DOCUMENTATION_MODULES if any(documentation(path) for path in paths or ()) else set()
    return set().union(*(GROUPS[group] for group in groups)) | readers | (set(available) - assigned)
