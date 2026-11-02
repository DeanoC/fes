; SPDX-License-Identifier: GPL-2.0-or-later
; ZX80 system variables. Addresses and meanings are the published ZX80
; interface (Operating Manual, appendix III): .o images and PEEKing
; programs depend on them.

ERR_NR   equ 0x4000     ; report code minus one ($FF = OK)
FLAGS    equ 0x4001     ; this ROM: see F_ bits
PPC      equ 0x4002     ; line being executed ($FFFE = command)
P_PTR    equ 0x4004     ; the cursor: where the next key goes in E_LINE
E_PPC    equ 0x4006     ; current line of the listing
VARS     equ 0x4008
E_LINE   equ 0x400A
D_FILE   equ 0x400C
DF_EA    equ 0x400E     ; start of the lower part of the screen
DF_END   equ 0x4010
DF_SZ    equ 0x4012     ; rows in the lower part
S_TOP    equ 0x4013     ; first line of the listing
X_PTR    equ 0x4015     ; first wrong symbol of the edit line, 0 if none
OLDPPC   equ 0x4017     ; CONTINUE line
FLAGX    equ 0x4019     ; this ROM: see X_ bits
T_ADDR   equ 0x401A     ; this ROM: error stack slot (ERR_SP)
SEED     equ 0x401C
FRAMES   equ 0x401E
DEST     equ 0x4020     ; name of the last variable assigned
RESULT   equ 0x4022     ; this ROM: last keyboard state
S_POSN   equ 0x4024     ; 33 - column (0 = newline due, 1 = row full)
S_LINE   equ 0x4025     ; 23 - row
CH_ADD   equ 0x4026
PROG     equ 0x4028

ERR_SP   equ T_ADDR
LAST_K   equ RESULT

; FLAGS bits
F_SPACE  equ 0          ; a keyword's trailing space was just printed
F_INPUT  equ 1          ; INPUT is collecting a line
F_EDIT   equ 2          ; characters are typed into the edit line (EDIT)
F_JUMP   equ 3          ; PPC holds the next line to run
F_KMODE  equ 4          ; the cursor shows K
F_DNUM   equ 5          ; INPUT wants a number (F_NUM bit 6 just above)
F_NUM    equ 6          ; last expression was numeric
F_CHECK  equ 7          ; syntax check only

; FLAGX bits (output state, all clear while editing)
X_LOWER  equ 0          ; printing to the lower part
X_DROP   equ 1          ; drop output that does not fit (display build)
X_MEASURE equ 2         ; count rows without storing
X_FULL   equ 5          ; output was dropped: the part is full
X_STR    equ 7          ; characters go to a workspace string (STR$)

; IY-relative offsets (IY is always $4000)
Y_ERR_NR equ 0x00
Y_FLAGS  equ 0x01
Y_DF_SZ  equ 0x12
Y_FLAGX  equ 0x19
Y_COL    equ 0x24
Y_LINE   equ 0x25

; Character codes
C_SPACE  equ 0x00
C_QUOTE  equ 0x01       ; string delimiter
C_POUND  equ 0x0C
C_DOLLAR equ 0x0D
C_QUERY  equ 0x0F
C_MINUS  equ 0x12
C_GT     equ 0x17
C_0      equ 0x1C
C_A      equ 0x26
C_K      equ 0x30
C_L      equ 0x31
C_S      equ 0x38
K_UP     equ 0x70
K_DOWN   equ 0x71
K_LEFT   equ 0x72
K_RIGHT  equ 0x73
K_HOME   equ 0x74
K_EDIT   equ 0x75
C_NL     equ 0x76
K_RUBOUT equ 0x77
C_CURSOR equ 0xB0       ; inverse K, the cursor byte in E_LINE

; Tokens
T_QUOTE  equ 0xD4       ; prints as "
T_THEN   equ 0xD5
T_TO     equ 0xD6
T_SEMI   equ 0xD7
T_COMMA  equ 0xD8
T_RPAREN equ 0xD9
T_LPAREN equ 0xDA
T_NOT    equ 0xDB
T_MINUS  equ 0xDC
T_PLUS   equ 0xDD
T_STAR   equ 0xDE
T_SLASH  equ 0xDF
T_AND    equ 0xE0
T_OR     equ 0xE1
T_POWER  equ 0xE2
T_EQ     equ 0xE3
T_GT     equ 0xE4
T_LT     equ 0xE5
T_LIST   equ 0xE6       ; first statement keyword
T_REM    equ 0xFE

; Reports (ERR_NR = report - 1)
R_OK       equ 0xFF
R_NEXT     equ 0        ; 1 NEXT without FOR
R_UNDEF    equ 1        ; 2 variable not found
R_SUBSCR   equ 2        ; 3 subscript out of range
R_MEMORY   equ 3        ; 4 no room
R_SCREEN   equ 4        ; 5 screen full
R_OVERFLOW equ 5        ; 6 arithmetic overflow
R_RETURN   equ 6        ; 7 RETURN without GOSUB
R_INPUT    equ 7        ; 8 INPUT as a command
R_STOP     equ 8        ; 9 STOP

GOSUB_END equ 0x8000    ; below the GOSUB records: not a line number
