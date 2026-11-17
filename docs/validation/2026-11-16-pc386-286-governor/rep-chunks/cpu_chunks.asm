bits 16
cpu 386
org 0x1000
%macro mark 1
  mov al,%1
  out 0x80,al
%endmacro
%macro run_rep 4
  mov word [0x8000],1000
  mark %1
%%loop:
  mov cx,%3
  mov si,0x8100
  mov di,0x8200
  rep %4
  dec word [0x8000]
  jne %%loop
  mark %2
%endmacro
start:
  cli
  cld
  xor ax,ax
  mov ds,ax
  mov es,ax
  run_rep 0,1,24,movsb
  run_rep 2,3,25,movsb
  run_rep 4,5,26,movsb
  run_rep 6,7,27,movsb
  run_rep 8,9,52,movsb
  run_rep 10,11,53,movsb
  run_rep 12,13,64,movsb
  mov al,0
  out 0x8f,al
  hlt
