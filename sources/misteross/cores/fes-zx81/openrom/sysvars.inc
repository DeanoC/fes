; SPDX-License-Identifier: GPL-2.0-or-later
; ZX81 system variables. Addresses and meanings are the published ZX81
; interface: programs and .p files depend on them.

ERR_NR   equ 0x4000     ; report code minus one ($FF = OK)
FLAGS    equ 0x4001     ; bit 0 suppress leading space, bit 6 numeric result
ERR_SP   equ 0x4002     ; machine-stack slot holding the error return
RAMTOP   equ 0x4004
MODE     equ 0x4006     ; editor cursor mode (this ROM: 0 K/L, 1 F, 2 G)
PPC      equ 0x4007     ; line being executed ($FFFE = direct command)
VERSN    equ 0x4009     ; first byte of a .p image
E_PPC    equ 0x400A     ; current line in the listing
D_FILE   equ 0x400C
DF_CC    equ 0x400E
VARS     equ 0x4010
DEST     equ 0x4012
E_LINE   equ 0x4014
CH_ADD   equ 0x4016
X_PTR    equ 0x4018
STKBOT   equ 0x401A
STKEND   equ 0x401C
BERG     equ 0x401E
MEM      equ 0x401F
SPARE1   equ 0x4021     ; this ROM: private output flags (OFLAGS)
DF_SZ    equ 0x4022
S_TOP    equ 0x4023
LAST_K   equ 0x4025
DEBOUNCE equ 0x4027
MARGIN   equ 0x4028
NXTLIN   equ 0x4029
OLDPPC   equ 0x402B
FLAGX    equ 0x402D
STRLEN   equ 0x402E
T_ADDR   equ 0x4030
SEED     equ 0x4032
FRAMES   equ 0x4034
COORDS   equ 0x4036
PR_CC    equ 0x4038
S_POSN   equ 0x4039     ; 33 - column, then 24 - line
CDFLAG   equ 0x403B     ; bit 7 SLOW mode
PRBUFF   equ 0x403C     ; 33 bytes; this ROM uses it as text scratch
MEMBOT   equ 0x405D     ; 30 bytes; this ROM uses it as floating-point scratch
SPARE2   equ 0x407B     ; this ROM: workspace start (WORKSP)
PROG     equ 0x407D

OFLAGS   equ SPARE1
WORKSP   equ SPARE2

; OFLAGS bits
OF_LOWER equ 0          ; printing to the lower screen
OF_INPUT equ 1          ; editor is collecting INPUT
OF_REPORT equ 2         ; a report is showing in the lower screen
OF_MEASURE equ 3        ; print without storing
OF_CLIP  equ 4          ; drop characters past the upper screen
OF_RUN   equ 6          ; executing a program line
OF_LVAL  equ 7          ; op_variable: find a destination

; IY-relative offsets (IY is always $4000)
Y_ERR_NR equ 0x00
Y_FLAGS  equ 0x01
Y_MODE   equ 0x06
Y_FLAGX  equ 0x2D
Y_OFLAGS equ 0x21
Y_DF_SZ  equ 0x22
Y_CDFLAG equ 0x3B

; Floating-point scratch in MEMBOT
FA       equ MEMBOT         ; sign, exponent, m1 m2 m3 m4 guard
FB       equ MEMBOT+7
FW       equ MEMBOT+14      ; 9-byte multiply/divide work
FT       equ MEMBOT+23      ; 7 spare bytes

; Character codes
C_SPACE  equ 0x00
C_QUOTE  equ 0x0B
C_DOLLAR equ 0x0D
C_COLON  equ 0x0E
C_QUERY  equ 0x0F
C_LPAREN equ 0x10
C_RPAREN equ 0x11
C_GT     equ 0x12
C_LT     equ 0x13
C_EQ     equ 0x14
C_PLUS   equ 0x15
C_MINUS  equ 0x16
C_STAR   equ 0x17
C_SLASH  equ 0x18
C_SEMI   equ 0x19
C_COMMA  equ 0x1A
C_DOT    equ 0x1B
C_0      equ 0x1C
C_A      equ 0x26
C_E      equ 0x2A
C_F      equ 0x2B
C_G      equ 0x2C
C_K      equ 0x30
C_L      equ 0x31
C_RND    equ 0x40
C_INKEY  equ 0x41
C_PI     equ 0x42
K_UP     equ 0x70
K_DOWN   equ 0x71
K_LEFT   equ 0x72
K_RIGHT  equ 0x73
K_GRAPH  equ 0x74
K_EDIT   equ 0x75
C_NL     equ 0x76
K_RUBOUT equ 0x77
K_FUNC   equ 0x79
C_NUMBER equ 0x7E
C_CURSOR equ 0x7F

; Tokens
T_QQ     equ 0xC0
T_AT     equ 0xC1
T_TAB    equ 0xC2
T_CODE   equ 0xC4
T_VAL    equ 0xC5
T_LEN    equ 0xC6
T_SIN    equ 0xC7
T_COS    equ 0xC8
T_TAN    equ 0xC9
T_ASN    equ 0xCA
T_ACS    equ 0xCB
T_ATN    equ 0xCC
T_LN     equ 0xCD
T_EXP    equ 0xCE
T_INT    equ 0xCF
T_SQR    equ 0xD0
T_SGN    equ 0xD1
T_ABS    equ 0xD2
T_PEEK   equ 0xD3
T_USR    equ 0xD4
T_STR    equ 0xD5
T_CHR    equ 0xD6
T_NOT    equ 0xD7
T_POW    equ 0xD8
T_OR     equ 0xD9
T_AND    equ 0xDA
T_LE     equ 0xDB
T_GE     equ 0xDC
T_NE     equ 0xDD
T_THEN   equ 0xDE
T_TO     equ 0xDF
T_STEP   equ 0xE0
T_LPRINT equ 0xE1
T_REM    equ 0xEA
T_NEXT   equ 0xF3

; Reports (ERR_NR = report - 1)
R_OK       equ 0xFF
R_NEXT     equ 0        ; 1 NEXT without FOR
R_UNDEF    equ 1        ; 2 variable not found
R_SUBSCR   equ 2        ; 3 subscript out of range
R_MEMORY   equ 3        ; 4 out of memory
R_SCREEN   equ 4        ; 5 screen full
R_OVERFLOW equ 5        ; 6 arithmetic overflow
R_RETURN   equ 6        ; 7 RETURN without GOSUB
R_INPUT    equ 7        ; 8 INPUT as a command
R_STOP     equ 8        ; 9 STOP
R_ARG      equ 9        ; A invalid argument
R_RANGE    equ 10       ; B integer out of range
R_NONSENSE equ 11       ; C nonsense in BASIC
R_BREAK    equ 12       ; D BREAK
