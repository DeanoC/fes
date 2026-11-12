# SPDX-License-Identifier: MIT
"""pc386 Windows corpus checks (scripts/pc386_wfw.py, sim_fes_pc386.py's wfw
suite and pc386_wfw_kit_acceptance.py's goal logic) on synthetic frames:
no Windows bytes or pictures."""
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import build_pc386_game_drive as drives
from scripts import pc386_wfw as wfw
from scripts import pc386_wfw_kit_acceptance as kit
from scripts import sim_fes_pc386 as sim

GREY, WHITE, NAVY, BLACK = (195, 199, 203), (255, 255, 255), (0, 0, 170), (0, 0, 0)


class Canvas:
    def __init__(self, width=640, height=480, fill=GREY):
        self.width, self.height = width, height
        self.pixels = bytearray(bytes(fill) * (width * height))

    def box(self, x0, y0, x1, y1, colour):
        for y in range(y0, y1 + 1):
            self.pixels[(y * self.width + x0) * 3:(y * self.width + x1 + 1) * 3] = bytes(colour) * (x1 - x0 + 1)

    def caption(self, x0, y0, x1, y1, text_from, text_to):
        """A title bar with white caption 'letters' and the grey system-menu box left of it."""
        self.box(x0 - 20, y0, x0 - 1, y1, GREY)
        self.box(x0, y0, x1, y1, NAVY)
        for x in range(text_from, text_to, 9):
            self.box(x, y0 + 4, x + 5, y1 - 4, WHITE)

    def frame(self):
        return wfw.Frame(self.width, self.height, bytes(self.pixels))

    def ppm(self):
        return f'P6\n{self.width} {self.height}\n255\n'.encode() + bytes(self.pixels)


def program_manager(maximized=False):
    canvas = Canvas()
    left, right = (0, 639) if maximized else (69, 578)
    bottom = 479 if maximized else 383
    canvas.box(left, 48 if not maximized else 0, right, bottom, GREY)
    top = 0 if maximized else 52
    canvas.caption(left + 22, top, right - 41, top + 17, left + 180, left + 300)
    canvas.box(left + 1, top + 18, right - 1, top + 36, WHITE)  # menu bar
    for x in range(left + 20, left + 200, 50):
        canvas.box(x, top + 23, x + 25, top + 31, BLACK)  # menu titles
    canvas.box(left + 4, top + 38, right - 92, top + 238, WHITE)  # the Main group window
    canvas.caption(left + 26, top + 42, right - 133, top + 59, left + 180, left + 220)
    return canvas


class DetectorTest(unittest.TestCase):
    def test_program_manager_with_its_group_window_is_found(self):
        found = wfw.program_manager(program_manager().frame())
        self.assertTrue(found['found'], found['reason'])
        self.assertFalse(found['maximized'])
        self.assertEqual((found['caption']['y0'], found['caption']['y1']), (52, 69))

    def test_a_maximized_program_manager_is_found(self):
        found = wfw.program_manager(program_manager(maximized=True).frame())
        self.assertTrue(found['found'], found['reason'])
        self.assertTrue(found['maximized'])

    def test_a_dialog_alone_is_not_program_manager(self):
        canvas = Canvas()
        canvas.box(160, 170, 470, 300, GREY)
        canvas.caption(181, 177, 460, 194, 260, 360)
        found = wfw.program_manager(canvas.frame())
        self.assertFalse(found['found'])
        self.assertIn('300 pixels', found['reason'])

    def test_a_window_without_a_group_window_is_not_program_manager(self):
        canvas = Canvas()
        canvas.caption(53, 27, 593, 43, 250, 380)
        canvas.box(53, 44, 593, 400, WHITE)
        self.assertFalse(wfw.program_manager(canvas.frame())['found'])

    def test_the_logo_screen_is_not_program_manager(self):
        canvas = Canvas(fill=BLACK)
        canvas.box(136, 24, 490, 448, (85, 170, 170))
        for y in range(60, 200, 30):
            canvas.box(200, y, 400, y + 20, (0, 0, 255))
        found = wfw.program_manager(canvas.frame())
        self.assertFalse(found['found'])
        self.assertFalse(wfw.text_screen(canvas.frame())['found'])

    def test_a_text_screen(self):
        canvas = Canvas(fill=BLACK)
        for row in range(5):
            canvas.box(0, row * 16, 200, row * 16 + 10, (170, 170, 170))
        self.assertTrue(wfw.text_screen(canvas.frame())['found'])
        self.assertFalse(wfw.text_screen(program_manager().frame())['found'])

    def test_a_kit_grab_is_cut_from_its_borders(self):
        source = program_manager()
        grab = Canvas(1920, 1080, BLACK)
        for y in range(1080):
            row = bytearray()
            for x in range(1440):
                at = ((y * 480 // 1080) * 640 + x * 640 // 1440) * 3
                row += source.pixels[at:at + 3]
            grab.pixels[(y * 1920 + 240) * 3:(y * 1920 + 1680) * 3] = row
        picture = wfw.picture(grab.frame())
        self.assertEqual((picture.width, picture.height), (640, 480))
        self.assertTrue(wfw.program_manager(grab.frame())['found'])

    def test_ppm_parsing_refuses_a_frame_still_being_written(self):
        data = program_manager().ppm()
        self.assertEqual(wfw.parse_ppm(data).width, 640)
        with self.assertRaises(wfw.FrameError):
            wfw.parse_ppm(data[:-100])


class SuiteTest(unittest.TestCase):
    def test_the_watch_stops_after_two_program_manager_frames_in_a_row(self):
        with tempfile.TemporaryDirectory() as directory:
            frames, stop = Path(directory) / 'frames', Path(directory) / 'stop'
            frames.mkdir()
            logo = Canvas(fill=BLACK).ppm()
            manager = program_manager().ppm()
            (frames / 't0003.50.ppm').write_bytes(manager)
            (frames / 't0003.75.ppm').write_bytes(logo)
            (frames / 't0004.00.ppm').write_bytes(manager)
            (frames / 't0004.25.ppm').write_bytes(manager[:-10])  # being written
            watch = sim.ProgramManagerWatch(frames, stop)
            watch()
            self.assertFalse(stop.exists())
            self.assertIsNone(watch.found)
            (frames / 't0004.25.ppm').write_bytes(manager)
            watch()
            self.assertTrue(stop.exists())
            self.assertEqual(watch.found['frame'], 't0004.00.ppm')
            self.assertEqual([result['program_manager'] for result in watch.results], [True, False, True, True])

    def test_the_suite_skips_without_the_drive(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(drives, 'STORE', Path(directory)), \
                mock.patch.dict('os.environ', {'FES_PC386_DRIVES': directory}):
            reason = sim.wfw_drive(mock.Mock(drive=None))
        self.assertIsInstance(reason, str)
        self.assertIn('no WfW drive', reason)

    def test_a_present_drive_that_fails_its_checks_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            store = Path(directory)
            (store / 'by-name').mkdir()
            image = store / ('0' * 64 + '.img')
            image.write_bytes(b'x' * 512)
            image.with_suffix('.json').write_text('{"sha256": "%s"}' % ('1' * 64))
            (store / 'by-name/wfw-msdos.img').symlink_to(image)
            with mock.patch.object(drives, 'STORE', store), \
                    mock.patch.dict('os.environ', {'FES_PC386_DRIVES': str(store)}), self.assertRaises(sim.SimError):
                sim.wfw_drive(mock.Mock(drive=None))
            with mock.patch.object(drives, 'STORE', store), \
                    mock.patch.dict('os.environ', {'FES_PC386_DRIVES': str(store)}), self.assertRaises(sim.SimError):
                sim.wfw_drive(mock.Mock(drive='nothing'))

    def report(self, end='stop-file', found=True, counters=None):
        return {'failures': [], 'end': {'reason': end, 'seconds': 5.0}, 'game_launch': {'seconds': 3.3},
                'rtl': {'stops': [], 'gaps': [], 'counters': counters or {'retired': 100, 'oracle_steps': 0,
                                                                          'protected_steps': 0}},
                'wfw': {'program_manager': {'frame': 't0005.00.ppm', 'seconds': 5.0} if found else None,
                        'frames': [{'frame': 't0005.00.ppm'}], 'launch_seconds': 3.3}}

    def test_program_manager_on_the_rtl_alone_passes(self):
        self.assertEqual(sim.wfw_failures(self.report(), 'rtl'), [])

    def test_a_run_without_program_manager_fails(self):
        failures = sim.wfw_failures(self.report(end='limit', found=False), 'rtl')
        self.assertTrue(any('did not appear' in failure for failure in failures))

    def test_oracle_steps_after_the_handover_fail(self):
        report = self.report(counters={'retired': 100, 'oracle_steps': 7, 'protected_steps': 7})
        self.assertTrue(any('oracle_steps=7' in failure for failure in sim.wfw_failures(report, 'rtl')))
        self.assertEqual(sim.wfw_failures(report, 'oracle'), [])


class KitGoalTest(unittest.TestCase):
    def test_the_nonce_lines_are_plain_dos_text(self):
        nonce = kit.make_nonce()
        self.assertRegex(nonce, r'^[A-Z0-9]{12}$')
        self.assertNotEqual(nonce, kit.make_nonce())

    def test_a_file_reads_back_only_with_its_nonce(self):
        self.assertTrue(kit.holds_nonce(b'FESWFW NOTE ABCDEF123456\r\n', 'ABCDEF123456'))
        self.assertFalse(kit.holds_nonce(b'FESWFW NOTE ABCDEF123450\r\n', 'ABCDEF123456'))
        self.assertFalse(kit.holds_nonce(None, 'ABCDEF123456'))

    def test_a_double_click_on_the_title_bar_must_maximize(self):
        before = wfw.program_manager(program_manager().frame())
        after = wfw.program_manager(program_manager(maximized=True).frame())
        self.assertTrue(kit.maximized_by_click(before, after))
        self.assertFalse(kit.maximized_by_click(before, before))

    def test_a_stop_that_needs_recovery_cleans_up_and_keeps_the_lease(self):
        tree = '/media/fat/fogcast/disk-images/' + '0' * 64 + '.img'
        with tempfile.TemporaryDirectory() as directory:
            run = kit.Run.__new__(kit.Run)
            run.args = mock.Mock(status_file=Path(directory) / 'status', kit='A', owner='Claude')
            run.out = Path(directory)
            run.record, run.game, run.remote, run.recovery_required = {'sha256': '0' * 64}, 'g', None, False
            run.result = {'goals': {}, 'events': []}
            answers = {'/v1/status': {'state': 'idle'}, '/v1/kit/lease': {'state': 'free'},
                       '/v1/health': {'boot_id': 'b'}, '/v1/stop': {'recovery': 'reboot_required'}}
            run.client = mock.Mock(request=lambda path, **_: answers[path])
            listings = iter([[], [tree], []])
            run.kit_paths = lambda: next(listings)
            run.ssh = mock.Mock()
            session = mock.Mock(needs_stop=False)

            def boot():
                session.needs_stop = True
                raise kit.AcceptanceError('Windows hung')
            run.boot_dos = boot
            with mock.patch.object(kit, 'check_claim', return_value='claim'), \
                    mock.patch.object(kit.kitlib, 'Session', return_value=session), \
                    mock.patch('sys.stdout'):
                self.assertEqual(run.main(), 1)
            session.close.assert_not_called()
            session.done.set.assert_called_once()
            run.ssh.assert_called_once_with(f"rm -r -- '{tree}'")
            self.assertTrue(run.result['after']['recovery_required'])
            self.assertFalse(run.result['pass'])

    def test_a_failed_lease_release_still_cleans_up(self):
        tree = '/media/fat/fogcast/disk-images/' + '0' * 64 + '.img'
        with tempfile.TemporaryDirectory() as directory:
            run = kit.Run.__new__(kit.Run)
            run.args = mock.Mock(status_file=Path(directory) / 'status', kit='A', owner='Claude')
            run.out = Path(directory)
            run.record, run.game, run.remote, run.recovery_required = {'sha256': '0' * 64}, 'g', None, False
            run.result = {'goals': {}, 'events': []}
            answers = {'/v1/status': {'state': 'idle'}, '/v1/kit/lease': {'state': 'free'},
                       '/v1/health': {'boot_id': 'b'}}
            run.client = mock.Mock(request=lambda path, **_: answers[path])
            listings = iter([[], [tree], []])
            run.kit_paths = lambda: next(listings)
            run.ssh = mock.Mock()
            session = mock.Mock(needs_stop=False)
            session.close.side_effect = kit.kitlib.KitError('release failed')
            run.boot_dos = mock.Mock(side_effect=kit.AcceptanceError('Windows hung'))
            with mock.patch.object(kit, 'check_claim', return_value='claim'), \
                    mock.patch.object(kit.kitlib, 'Session', return_value=session), \
                    mock.patch('sys.stdout'):
                self.assertEqual(run.main(), 1)
            run.ssh.assert_called_once_with(f"rm -r -- '{tree}'")
            self.assertEqual(run.result['after']['left'], [])

    def test_the_title_bar_target_is_inside_the_caption(self):
        found = wfw.program_manager(program_manager().frame())
        x, y = kit.caption_target(found)
        self.assertTrue(found['caption']['x0'] < x < found['caption']['x1'])
        self.assertTrue(found['caption']['y0'] <= y <= found['caption']['y1'])


if __name__ == '__main__':
    unittest.main()
