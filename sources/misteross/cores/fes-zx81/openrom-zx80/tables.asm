; SPDX-License-Identifier: GPL-2.0-or-later
; Keyboard, keyword and function tables. Key index = row * 5 + column:
; rows are address lines A8..A15, columns data bits 0..4; index 0 is SHIFT.

key_plain:
        db 0x00,0x3f,0x3d,0x28,0x3b             ; - Z X C V
        db 0x26,0x38,0x29,0x2b,0x2c             ; A S D F G
        db 0x36,0x3c,0x2a,0x37,0x39             ; Q W E R T
        db 0x1d,0x1e,0x1f,0x20,0x21             ; 1 2 3 4 5
        db 0x1c,0x25,0x24,0x23,0x22             ; 0 9 8 7 6
        db 0x35,0x34,0x2e,0x3a,0x3e             ; P O I U Y
        db 0x76,0x31,0x30,0x2f,0x2d             ; NEWLINE L K J H
        db 0x00,0x1b,0x32,0x33,0x27             ; SPACE . M N B
key_shift:
        db 0x00,0x0e,0xd7,0x0f,0xdf             ; - : ; ? /
        db 0x09,0x08,0x06,0x07,0x0b             ; graphics
        db 0x02,0x03,0x04,0x05,0x0a             ; graphics
        db 0xdb,0xe0,0xd5,0xd6,0x72             ; NOT AND THEN TO left
        db 0x77,0x74,0x73,0x70,0x71             ; RUBOUT HOME right up down
        db 0xde,0xd9,0xda,0x0d,0x01             ; * ) ( $ "
        db 0x75,0xe3,0xdd,0xdc,0xe2             ; EDIT = + - **
        db 0x0c,0xd8,0xe4,0xe5,0xe1             ; £ , > < OR

        charset zx80
; Token texts from $D4, the last character of each with bit 7 set.
tokens: dz """
        dz "THEN"
        dz "TO"
        dz ";"
        dz ","
        dz ")"
        dz "("
        dz "NOT"
        dz "-"
        dz "+"
        dz "*"
        dz "/"
        dz "AND"
        dz "OR"
        dz "**"
        dz "="
        dz ">"
        dz "<"
        dz "LIST"
        dz "RETURN"
        dz "CLS"
        dz "DIM"
        dz "SAVE"
        dz "FOR"
        dz "GO TO"
        dz "POKE"
        dz "INPUT"
        dz "RANDOMISE"
        dz "LET"
        dz "?"
        dz "?"
        dz "NEXT"
        dz "PRINT"
        dz "?"
        dz "NEW"
        dz "RUN"
        dz "STOP"
        dz "CONTINUE"
        dz "IF"
        dz "GO SUB"
        dz "LOAD"
        dz "CLEAR"
        dz "REM"
        dz "?"

; Built-in functions: each name with its "(", then its routine; bit 15:
; it takes a string, bit 14: it gives a number.
fn_table:
        db C_A, C_A + 1, C_S, T_LPAREN                          ; ABS(
        dw fn_abs + 0x4000
        db C_A + 2, C_A + 14, C_A + 3, C_A + 4, T_LPAREN        ; CODE(
        dw fn_code + 0xc000
        db C_A + 15, C_A + 4, C_A + 4, C_A + 10, T_LPAREN       ; PEEK(
        dw fn_peek + 0x4000
        db C_A + 17, C_A + 13, C_A + 3, T_LPAREN                ; RND(
        dw fn_rnd + 0x4000
        db C_A + 20, C_S, C_A + 17, T_LPAREN                    ; USR(
        dw fn_usr + 0x4000
        db C_A + 2, C_A + 7, C_A + 17, C_DOLLAR, T_LPAREN       ; CHR$(
        dw fn_chr
        db C_S, C_A + 19, C_A + 17, C_DOLLAR, T_LPAREN          ; STR$(
        dw fn_str
        db C_A + 19, C_A + 11, C_DOLLAR, T_LPAREN               ; TL$(
        dw fn_tl + 0x8000
        db 0
