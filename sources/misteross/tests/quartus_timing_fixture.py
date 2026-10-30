"""The TimeQuest 17.0.2 summary rows the Quartus sealers' timing gates read."""

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

# Each report and the error its gate must raise for it.
FAILING_REPORTS = (
    ("no multicorner summary", PASSING_REPORT.replace(f"{SLACK_ROW}\n", ""), "required analysis categories"),
    ("setup miss", PASSING_REPORT.replace(SLACK_ROW, SLACK_ROW.replace("; 1.559 ", "; -10.015 ")),
     "required analysis categories"),
    ("sub-picosecond hold miss", PASSING_REPORT.replace(SLACK_ROW, SLACK_ROW.replace("; 0.094 ", "; -0.000 ")),
     "required analysis categories"),
    ("sub-picosecond TNS", PASSING_REPORT.replace(TNS_ROW, TNS_ROW.replace("; 0.0   ;", "; -0.000 ;", 1)),
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
