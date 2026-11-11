"""The TimeQuest summary rows the Quartus sealers' timing gates read."""

import re

SLACK_ROW = "; Worst-case Slack                                            ; 1.559 ; 0.094 ; 6.508    ; 0.454   ; 1.122               ;"
TNS_ROW = "; Design-wide TNS                                             ; 0.0   ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;"
ILLEGAL_ROW = "; Illegal Clocks                  ; 0     ; 0    ;"
UNCONSTRAINED_ROW = "; Unconstrained Clocks            ; 0     ; 0    ;"

PASSING_REPORT = f"""\
+------------------------------------------------------------------------------------------------------------------------+
; Multicorner Timing Analysis Summary                                                                                    ;
+-------------------------------------------------------------+-------+-------+----------+---------+---------------------+
; Clock                                                       ; Setup ; Hold  ; Recovery ; Removal ; Minimum Pulse Width ;
+-------------------------------------------------------------+-------+-------+----------+---------+---------------------+
{SLACK_ROW}
;  system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; 2.347 ; 0.118 ; 6.508    ; 2.434   ; 6.688               ;
{TNS_ROW}
;  system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; 0.000 ; 0.000 ; 0.000    ; 0.000   ; 0.000               ;
+-------------------------------------------------------------+-------+-------+----------+---------+---------------------+

+------------------------------------------------+
; Unconstrained Paths Summary                    ;
+---------------------------------+-------+------+
; Property                        ; Setup ; Hold ;
+---------------------------------+-------+------+
{ILLEGAL_ROW}
{UNCONSTRAINED_ROW}
; Unconstrained Input Ports       ; 2     ; 2    ;
; Unconstrained Input Port Paths  ; 2     ; 2    ;
; Unconstrained Output Ports      ; 34    ; 34   ;
; Unconstrained Output Port Paths ; 34    ; 34   ;
+---------------------------------+-------+------+
"""

# The summary of Coleco's Quartus build on main at 65ec1f22a, which missed setup.
MISS_CLOCK = "system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk"
MISS_SLACK_ROW = "; Worst-case Slack                                            ; -2.243   ; 0.162 ; 12.528   ; 0.271   ; 1.122               ;"
MISS_REPORT = f"""\
+---------------------------------------------------------------------------------------------------------------------------+
; Multicorner Timing Analysis Summary                                                                                       ;
+-------------------------------------------------------------+----------+-------+----------+---------+---------------------+
; Clock                                                       ; Setup    ; Hold  ; Recovery ; Removal ; Minimum Pulse Width ;
+-------------------------------------------------------------+----------+-------+----------+---------+---------------------+
{MISS_SLACK_ROW}
;  FPGA_CLK1_50                                               ; N/A      ; N/A   ; N/A      ; N/A     ; 9.274               ;
;  {MISS_CLOCK} ; -2.243   ; 0.162 ; 12.528   ; 0.904   ; 8.112               ;
;  system_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk ; 0.745    ; 0.191 ; 79.538   ; 0.271   ; 39.863              ;
;  video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk  ; 6.155    ; 0.188 ; N/A      ; N/A     ; 5.281               ;
; Design-wide TNS                                             ; -616.825 ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
;  FPGA_CLK1_50                                               ; N/A      ; N/A   ; N/A      ; N/A     ; 0.000               ;
;  {MISS_CLOCK} ; -616.825 ; 0.000 ; 0.000    ; 0.000   ; 0.000               ;
+-------------------------------------------------------------+----------+-------+----------+---------+---------------------+

+------------------------------------------------+
; Unconstrained Paths Summary                    ;
+---------------------------------+-------+------+
; Property                        ; Setup ; Hold ;
+---------------------------------+-------+------+
{ILLEGAL_ROW}
{UNCONSTRAINED_ROW}
+---------------------------------+-------+------+
"""
MISS_ERROR = f"timing miss: Setup worst slack -2.243 ns on {MISS_CLOCK} (TNS -616.825 ns)"

# Each report and the error its gate must raise for it.
FAILING_REPORTS = (
    ("no multicorner summary", PASSING_REPORT.replace(f"{SLACK_ROW}\n", ""), "required analysis categories"),
    ("setup miss", MISS_REPORT, re.escape(MISS_ERROR)),
    ("setup miss on an unreported clock", PASSING_REPORT.replace(SLACK_ROW, SLACK_ROW.replace("; 1.559 ", "; -10.015 ")),
     re.escape("timing miss: Setup worst slack -10.015 ns on an unreported clock (TNS 0.0 ns)")),
    ("sub-picosecond hold miss", PASSING_REPORT.replace(SLACK_ROW, SLACK_ROW.replace("; 0.094 ", "; -0.000 ")),
     re.escape("timing miss: Hold worst slack -0.000 ns on an unreported clock (TNS 0.0 ns)")),
    ("sub-picosecond TNS", PASSING_REPORT.replace(TNS_ROW, TNS_ROW.replace("; 0.0   ;", "; -0.000 ;", 1)),
     re.escape("timing miss: Setup worst slack 1.559 ns on an unreported clock (TNS -0.000 ns)")),
    ("two analyses miss", MISS_REPORT.replace(MISS_SLACK_ROW, MISS_SLACK_ROW.replace("; 0.162 ", "; -0.051 ")),
     re.escape(f"{MISS_ERROR}; Hold worst slack -0.051 ns on an unreported clock (TNS 0.0 ns)")),
    ("sub-picosecond setup miss beside a zero-slack clock",
     MISS_REPORT.replace(MISS_SLACK_ROW, MISS_SLACK_ROW.replace("; -2.243   ", "; -0.000   "))
     .replace(f";  {MISS_CLOCK} ; -2.243   ", f";  {MISS_CLOCK} ; -0.000   ")
     .replace("; 0.745    ; 0.191 ", "; 0.000    ; 0.191 "),
     re.escape(f"timing miss: Setup worst slack -0.000 ns on {MISS_CLOCK} (TNS -616.825 ns)") + r"\Z"),
    ("worst-case slack row twice", PASSING_REPORT.replace(SLACK_ROW, f"{SLACK_ROW}\n{SLACK_ROW}"),
     "required analysis categories"),
    ("N/A in the worst-case slack row", PASSING_REPORT.replace(SLACK_ROW, SLACK_ROW.replace("; 6.508    ", "; N/A      ")),
     "required analysis categories"),
    ("no unconstrained paths summary",
     PASSING_REPORT.replace(f"{ILLEGAL_ROW}\n", "").replace(f"{UNCONSTRAINED_ROW}\n", ""),
     "unconstrained paths summary"),
    ("no illegal clocks row", PASSING_REPORT.replace(f"{ILLEGAL_ROW}\n", ""), "unconstrained paths summary"),
    ("no unconstrained clocks row", PASSING_REPORT.replace(f"{UNCONSTRAINED_ROW}\n", ""),
     "unconstrained paths summary"),
    ("unconstrained clocks row twice",
     PASSING_REPORT.replace(UNCONSTRAINED_ROW, f"{UNCONSTRAINED_ROW}\n{UNCONSTRAINED_ROW}"),
     "unconstrained paths summary"),
    ("unconstrained setup clock", PASSING_REPORT.replace(UNCONSTRAINED_ROW, UNCONSTRAINED_ROW.replace("; 0     ;", "; 1     ;")),
     "unconstrained clocks: 1 setup, 0 hold"),
    ("illegal hold clock", PASSING_REPORT.replace(ILLEGAL_ROW, ILLEGAL_ROW.replace("; 0    ;", "; 3    ;")),
     "illegal clocks: 0 setup, 3 hold"),
)


def assert_timing_gate(test, validate, error) -> None:
    """The gate passes the report only while every analysis passes and
    TimeQuest analysed every clock."""
    timing = validate(PASSING_REPORT)
    test.assertEqual([entry["type"] for entry in timing],
                     ["Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width"])
    test.assertEqual(timing[0]["slack_ns"], 1.559)
    for name, report, message in FAILING_REPORTS:
        test.assertNotEqual(report, PASSING_REPORT, name)
        with test.subTest(name), test.assertRaisesRegex(error, message):
            validate(report)
