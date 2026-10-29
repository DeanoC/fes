"""Run the real Tcl controller with offline master-service/register mocks only."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
MOCK = r'''
set expected 123456789abcdef00123456789abcdef
set writes {}; set reads {}; set closed 0; set claimed 0; set mode done
array set registers {}
proc expect {condition message} {if {![uplevel 1 [list expr $condition]]} {error $message}}
proc put {bank name value {extra 0}} {
    set ::registers([expr {[de25ramtest::address $bank $name] + $extra}]) $value
}
proc get_service_paths {kind} {return [list /explicit/master /another/master]}
proc claim_service {kind path owner} {incr ::claimed; return /claimed/master}
proc close_service {kind path} {incr ::closed}
proc master_read_32 {master address count} {
    lappend ::reads $address
    if {[info exists ::registers($address)]} {return [list $::registers($address)]}
    return {0}
}
proc master_write_32 {master address words} {
    lappend ::writes [list $address [lindex $words 0]]
    set ::registers($address) [lindex $words 0]
    foreach bank {fpga sdram} {
        if {$address == [de25ramtest::address $bank COMMAND_OFFSET] &&
            [lindex $words 0] == [de25ramtest::constant COMMAND_START] && $::mode eq "uncertain_start"} {
            error "START transport result unknown"
        }
        if {$address == [de25ramtest::address $bank COMMAND_OFFSET] &&
            [lindex $words 0] == [de25ramtest::constant COMMAND_SNAPSHOT]} {
            set serial [de25ramtest::address $bank SERIAL_OFFSET]
            if {$::mode ne "stuck_serial"} {incr ::registers($serial)}
            put $bank SNAPSHOT_GENERATION_OFFSET 1
            put $bank SNAPSHOT_SEQUENCE_OFFSET $::registers([de25ramtest::address $bank SEQUENCE_OFFSET])
            put $bank SNAPSHOT_STATUS_OFFSET [expr {[de25ramtest::constant STATUS_CALIBRATED] |
                [de25ramtest::constant [expr {$::mode eq "busy" ? "STATUS_BUSY" : "STATUS_DONE"}]]}]
            put $bank ERRORS_OFFSET [expr {$::mode eq "errors" ? 3 : 0}]
            put $bank SNAPSHOT_FAULT_OFFSET [expr {$::mode eq "fault" ? [de25ramtest::constant FAULT_RESPONSE] : 0}]
            put $bank WRITTEN_OFFSET 2
            put $bank WRITTEN_OFFSET 1 4
        }
    }
}
foreach bank {hps fpga sdram} {
    foreach {name value} [list ID_OFFSET [de25ramtest::constant IDENTITY] \
        VERSION_OFFSET [de25ramtest::constant VERSION] \
        CHANNEL_OFFSET [de25ramtest::constant [string toupper $bank]_CHANNEL] \
        CAPACITY_OFFSET [de25ramtest::constant [expr {$bank eq "sdram" ? "SDRAM_BYTES" : "LPDDR4_BYTES"}]] \
        CLOCK_HZ_OFFSET [expr {$bank eq "sdram" ? 50000000 : 100000000}] \
        DATA_BYTES_OFFSET [expr {$bank eq "sdram" ? 2 : 16}] \
        STATUS_OFFSET [de25ramtest::constant STATUS_CALIBRATED] GENERATION_OFFSET 1 SEQUENCE_OFFSET 1 SERIAL_OFFSET 0] {
        put $bank $name $value
    }
    for {set index 0} {$index < 4} {incr index} {
        put $bank BUILD_ID_OFFSET [expr {(0x123456789abcdef00123456789abcdef >> (32*$index)) & 0xffffffff}] [expr {4*$index}]
    }
}
'''


@unittest.skipUnless(shutil.which("tclsh"), "requires Tcl 8.6 interpreter")
class RamtestJtagTests(unittest.TestCase):
    def run_tcl(self, body, *, mock=True):
        script = "source {" + str(HERE / "jtag.tcl") + "}\n" + (MOCK if mock else "") + body
        with tempfile.TemporaryDirectory(prefix="de25-jtag-mock-") as temporary:
            path = Path(temporary) / "offline.tcl"
            path.write_text("try {\n" + script + "\n} on error {message options} {puts stderr [dict get $options -errorinfo]; exit 1}\n")
            result = subprocess.run(["tclsh", str(path)], capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_source_has_no_hardware_side_effects(self):
        self.run_tcl('puts "source only"', mock=False)

    def test_both_local_banks_verify_every_identity_before_writes(self):
        for bank in ("fpga", "sdram"):
            with self.subTest(bank=bank):
                self.run_tcl('''
set result [de25ramtest::run /explicit/master $expected ''' + bank + ''' 100 1 32]
expect {[dict get $result outcome] eq "PASS"} "expected successful mock diagnostic"
expect {[dict get $result written_bytes] == 0x100000002} "64-bit counter was truncated"
expect {[dict size [dict get $result endpoints]] == 3} "not all endpoints identified"
expect {$closed == 1 && $claimed == 1} "service lifetime mismatch"
foreach write $writes {
    set address [lindex $write 0]
    expect {$address < [de25ramtest::address hps ID_OFFSET] || $address >= [de25ramtest::address hps ID_OFFSET] + [de25ramtest::constant SPAN]} "HPS was written"
}
''')

    def test_hps_and_implicit_master_are_rejected_before_service_access(self):
        self.run_tcl('''
foreach invocation [list [list /explicit/master $expected hps] [list {} $expected fpga] [list /missing/master $expected fpga]] {
    expect {[catch {de25ramtest::run {*}$invocation}]} "unsafe invocation accepted"
}
expect {$claimed == 0 && [llength $writes] == 0} "rejected invocation touched a master"
expect {[catch {de25ramtest::write_word /claimed/master hps COMMAND_OFFSET 8}]} "HPS snapshot allowed"
''')

    def test_bad_identity_on_any_endpoint_prevents_all_writes(self):
        self.run_tcl('''
foreach bank {hps fpga sdram} {
    set old $registers([de25ramtest::address $bank ID_OFFSET])
    put $bank ID_OFFSET 0
    expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32}]} "identity mismatch accepted"
    expect {[llength $writes] == 0} "write before all identities verified"
    put $bank ID_OFFSET $old
}
''')

    def test_upper_half_of_build_id_is_checked(self):
        self.run_tcl('''
put sdram BUILD_ID_OFFSET 0 12
expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32} message]} "128-bit build mismatch accepted"
expect {[string match {*build ID mismatch: sdram*} $message]} "wrong rejection"
expect {[llength $writes] == 0} "write before build verification"
''')

    def test_uncalibrated_busy_or_faulted_bank_is_not_started(self):
        self.run_tcl('''
foreach status [list 0 [expr {[de25ramtest::constant STATUS_CALIBRATED] | [de25ramtest::constant STATUS_BUSY]}] \
    [expr {[de25ramtest::constant STATUS_CALIBRATED] | [de25ramtest::constant STATUS_RESET_UNCERTAIN]}]] {
    put fpga STATUS_OFFSET $status
    expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32}]} "unsafe status accepted"
}
put fpga STATUS_OFFSET [de25ramtest::constant STATUS_CALIBRATED]
put fpga FAULT_OFFSET 1
expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32}]} "fault ignored"
expect {[llength $writes] == 0} "unsafe status caused write"
''')

    def test_host_timeout_requests_stop_and_keeps_drain_unproven(self):
        self.run_tcl('''
set mode busy
set result [de25ramtest::run /explicit/master $expected fpga 1 1 32]
expect {[dict get $result outcome] eq "HOST_TIMEOUT" && [dict get $result drain_proven] == 0} "timeout became success/drain"
expect {[lindex [lindex $writes end] 1] == [de25ramtest::constant COMMAND_STOP]} "timeout did not request STOP"
expect {$closed == 1} "service not closed"
''')

    def test_snapshot_failure_after_start_attempts_stop_and_raises(self):
        self.run_tcl('''
set mode stuck_serial
expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32} message]} "stale snapshot accepted"
expect {[string match {*drain remains unproven*} $message]} "uncertain drain omitted"
expect {[lindex [lindex $writes end] 1] == [de25ramtest::constant COMMAND_STOP]} "snapshot error did not request STOP"
''')

    def test_uncertain_start_is_not_replayed_and_attempts_one_stop(self):
        self.run_tcl('''
set mode uncertain_start
expect {[catch {de25ramtest::run /explicit/master $expected fpga 100 1 32} message]} "uncertain START accepted"
expect {[string match {*drain remains unproven*} $message]} "uncertain drain omitted"
set starts 0; set stops 0
foreach write $writes {
    if {[lindex $write 0] == [de25ramtest::address fpga COMMAND_OFFSET]} {
        if {[lindex $write 1] == [de25ramtest::constant COMMAND_START]} {incr starts}
        if {[lindex $write 1] == [de25ramtest::constant COMMAND_STOP]} {incr stops}
    }
}
expect {$starts == 1 && $stops == 1 && $closed == 1} "uncertain command replayed or not contained"
''')

    def test_errors_and_response_fault_are_individually_reported_as_failure(self):
        self.run_tcl('''
foreach mode {errors fault} {
    set result [de25ramtest::run /explicit/master $expected sdram 100 1 32]
    expect {[dict get $result outcome] eq "FAIL"} "memory error/fault became pass"
    if {$mode eq "errors"} {expect {[dict get $result errors] == 3} "error count missing"}
    if {$mode eq "fault"} {expect {[dict get $result fault] == [de25ramtest::constant FAULT_RESPONSE]} "response fault missing"}
}
''')

    def test_generated_constants_reject_duplicates_and_unsupported_forms(self):
        self.run_tcl('''
set channel [file tempfile path]
puts $channel "`define FES_DE25_RAMTEST_BUILD_ID_WORD_COUNT 32'h00000004"
puts $channel "`define FES_DE25_RAMTEST_BUILD_ID_WORD_COUNT 32'h00000004"
close $channel
expect {[catch {de25ramtest::load_constants $path}]} "duplicate constant accepted"
set channel [open $path w]
puts $channel "`define FES_DE25_RAMTEST_BUILD_ID_WORD_COUNT 64'h0000000000000004"
close $channel
expect {[catch {de25ramtest::load_constants $path}]} "unsupported constant accepted"
file delete $path
''')


if __name__ == "__main__":
    unittest.main()
