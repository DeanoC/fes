"""Parent regression groups for component-only changes.

Parent implementation, configuration and unknown inputs remain full-suite. This
is an explicit integration boundary, not an inferred Python dependency graph.
Unassigned new test modules always run until their ownership is reviewed.
"""
from pathlib import PurePosixPath


def names(text):
    return frozenset('test_' + name for name in text.split())


GROUPS = {
    'policy': names('''affected check_deadcode check_diff check_doc_links ci_gate
        ci_workflow consistency coordination generate hil_plan host_affected
        image_assembly parent_affected test_changed test_policy'''),
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


def under(path, prefix):
    return path == prefix or path.startswith(prefix + '/')


def select_groups(paths, video=False, media=False):
    """Return groups, or None for complete coverage. Union mixed owners."""
    if paths is None or not paths:
        return None
    groups = {'policy', 'build'}
    for path in paths:
        # Restrict pruning to known first-party component trees. Even docs in
        # a mixed change are conservative: tests may read embedded Markdown.
        parts = PurePosixPath(path).parts
        if '..' in parts or path.startswith('/'):
            return None
        if under(path, 'sources/FogCast'):
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
            # scripts/tests, image/platform and newly introduced modules.
            return None
    if video:
        groups.add('packages')
    if media:
        groups.add('media')
    return groups


def selected_modules(groups, available):
    if groups is None:
        return set(available)
    assigned = set().union(*GROUPS.values())
    # A new/unclassified test is never silently omitted.
    return set().union(*(GROUPS[group] for group in groups)) | (set(available) - assigned)
