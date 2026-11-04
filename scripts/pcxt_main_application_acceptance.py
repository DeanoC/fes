#!/usr/bin/env python3
"""Run an owned COM or game diagnostic bound to merged main and local tools.

The caller must already own and launch the session, and remains responsible
for stopping it, restoring the kit and releasing its claim. No retry is made.
"""
import hashlib
import json
import sys
from pathlib import Path

if __package__:
    from . import pcxt_dos_application_acceptance as app
    from . import pcxt_floppy_persistence_acceptance as floppy
else:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import pcxt_dos_application_acceptance as app
    import pcxt_floppy_persistence_acceptance as floppy


HEALTH_UNSAFE = False


def main(argv=None):
    global HEALTH_UNSAFE
    HEALTH_UNSAFE = False
    original_parser, original_source = app.parser, app.source_provenance
    original_validate_once = app._validate_active_once
    original_runner, original_validate = app.pa.Runner, app.validate_active
    original_com, original_game = app.run_com, app.run_game

    def parser():
        p = original_parser()
        p.add_argument('--source-repository', required=True)
        p.add_argument('--source-tool-commit', required=True)
        p.add_argument('--source-historical-main', action='store_true', help='test a pinned historical main commit only')
        p.add_argument('--source-observed-tip', help='reviewed live main tip in historical mode')
        p.add_argument('--source-diff-sha256', help='reviewed raw producer-to-tip diff SHA-256')
        p.add_argument('--source-minimum-tip', help='last accepted main tip in historical mode')
        p.add_argument('--host-pid', type=int, required=True)
        return p

    def prepare(args):
        args.source_main = not getattr(args, 'source_historical_main', False)
        args.source_pr = None
        args.source_base = 'main'
        return args

    def source(args, font):
        result, host_raw, software_raw = original_source(args, font)
        floppy.check_matching_sources(result, args.source_commit)
        result['main_source'] = floppy.main_source_evidence(args)
        for module in (floppy, floppy.health_evidence, sys.modules[__name__]):
            path = Path(module.__file__).resolve()
            result['diagnostic_file_sha256'][str(path.relative_to(Path(__file__).resolve().parents[1]))] = hashlib.sha256(path.read_bytes()).hexdigest()
        return result, host_raw, software_raw

    def guarded(runner, owned, args, disk_sha, **kwargs):
        previous = observer.tracing
        try:
            observer.tracing = True
            observer.run_stage('host_guard', lambda: floppy.running_private_host(args.host_pid, args.host_api, args.host_binary))
            return floppy.health_evidence.traced_app_validation(
                app, observer, lambda: original_validate_once(runner, owned, args, disk_sha, **kwargs))
        finally:
            observer.tracing = previous

    def scenario(original, *, game=False):
        def run(runner, owned, args, rec, input_session_id, font, **kwargs):
            try:
                floppy.require_source(args)
                if game:
                    if 'observer_factory' in kwargs:
                        raise app.pa.AcceptanceError('game observer factory is owned by the main wrapper')
                    def observer_factory(observe):
                        global HEALTH_UNSAFE
                        try:
                            observer.prepare()
                            def traced(deadline):
                                observer.begin_trace()
                                try:
                                    return observe(deadline)
                                finally:
                                    observer.end_trace()
                            delegate = app.PrestartedObserver(traced)
                        except BaseException:
                            HEALTH_UNSAFE = True
                            raise
                        class GuardedObserver:
                            def check(self):
                                global HEALTH_UNSAFE
                                try:
                                    return delegate.check()
                                except BaseException:
                                    HEALTH_UNSAFE = True
                                    raise
                            def close(self):
                                global HEALTH_UNSAFE
                                try:
                                    return delegate.close()
                                except BaseException:
                                    HEALTH_UNSAFE = True
                                    raise
                        return GuardedObserver()
                    kwargs['observer_factory'] = observer_factory
                original(runner, owned, args, rec, input_session_id, font, **kwargs)
                floppy.require_source(args)
            finally:
                if getattr(args, 'source_historical_main', False):
                    rec['source_minimum_tip'] = args.source_minimum_tip
                    rec['source_tip_observations'] = list(getattr(args, 'source_tip_observations', []))
        return run

    # Fail before the application diagnostic's first interaction with a kit.
    parsed = prepare(parser().parse_args(argv))
    floppy.require_source(parsed)
    observer = floppy.health_evidence.HealthEvidence(parsed.evidence, parsed.target_id, parsed.expected_boot,
                                                      parsed.bundle_id, parsed.image_sha256)
    class ObservedRunner(original_runner):
        def __init__(self, options):
            super().__init__(options)
            observer.install(self.api)
    def observed_validate(*args, **kwargs):
        global HEALTH_UNSAFE
        try:
            observer.begin_trace()
            try:
                return original_validate(*args, **kwargs)
            finally:
                active_error = sys.exception()
                try:
                    observer.end_trace()
                except BaseException:
                    HEALTH_UNSAFE = True
                    if active_error is None:
                        raise
                    active_error.add_note('stage evidence collection failed; claim must be retained')
        except BaseException:
            HEALTH_UNSAFE = True
            raise
    app.parser, app.source_provenance = parser, source
    app._validate_active_once = guarded
    app.run_com, app.run_game = scenario(original_com), scenario(original_game, game=True)
    app.pa.Runner, app.validate_active = ObservedRunner, observed_validate
    # app.main parses its own args; the main-only defaults must be applied to
    # that namespace too, including every nested active-session observation.
    def prepared_parser():
        p = parser()
        p.set_defaults(source_main=not getattr(parsed, 'source_historical_main', False), source_pr=None, source_base='main')
        if getattr(parsed, 'source_historical_main', False):
            p.set_defaults(source_last_observed_tip=parsed.source_last_observed_tip,
                           source_tip_observations=list(parsed.source_tip_observations))
        return p
    app.parser = prepared_parser
    try:
        app.main(argv)
    finally:
        app.parser, app.source_provenance = original_parser, original_source
        app._validate_active_once = original_validate_once
        app.run_com, app.run_game = original_com, original_game
        app.pa.Runner, app.validate_active = original_runner, original_validate
        active_error = sys.exception()
        try:
            observer.close()
        except BaseException:
            HEALTH_UNSAFE = True
            if active_error is None:
                raise
            active_error.add_note('health evidence close failed; claim must be retained')
        if observer.compromised:
            HEALTH_UNSAFE = True


def cli():
    try:
        app.install_signal_handler()
        main()
    except BaseException as error:
        if HEALTH_UNSAFE:
            raise SystemExit(74) from error
        if isinstance(error, (app.pa.AcceptanceError, ValueError, OSError, json.JSONDecodeError)):
            raise SystemExit(str(error)) from error
        raise


if __name__ == '__main__':
    cli()
