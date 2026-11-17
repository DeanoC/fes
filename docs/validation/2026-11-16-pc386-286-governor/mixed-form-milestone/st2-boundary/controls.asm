bits 16
cpu 286
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax

 ; ram: source ES:5000, frame targets ES:6ffe/6ffc/6ffa
 mov ax,0x0000
 mov ss,ax
 mov es,ax
 mov sp,0x7000
 mov bp,0x5002
 mov word [es:0x5000],0xabcd
 jmp .clean0
.clean0:
 nop
 nop
 nop
 nop
 mov al,0
 out 0x80,al
 enter 0,2
 mov al,1
 out 0x80,al
 cmp bp,0x6ffe
 jne fail
 cmp sp,0x6ffa
 jne fail
 cmp word [es:0x6ffe],0x5002
 jne fail
 cmp word [es:0x6ffc],0xabcd
 jne fail
 cmp word [es:0x6ffa],0x6ffe
 jne fail
 leave
 cmp bp,0x5002
 jne fail
 cmp sp,0x7000
 jne fail

 ; source_page_end: source ES:4ffe, frame targets ES:6ffe/6ffc/6ffa
 mov ax,0x0000
 mov ss,ax
 mov es,ax
 mov sp,0x7000
 mov bp,0x5000
 mov word [es:0x4ffe],0xabcd
 jmp .clean1
.clean1:
 nop
 nop
 nop
 nop
 mov al,2
 out 0x80,al
 enter 0,2
 mov al,3
 out 0x80,al
 cmp bp,0x6ffe
 jne fail
 cmp sp,0x6ffa
 jne fail
 cmp word [es:0x6ffe],0x5000
 jne fail
 cmp word [es:0x6ffc],0xabcd
 jne fail
 cmp word [es:0x6ffa],0x6ffe
 jne fail
 leave
 cmp bp,0x5000
 jne fail
 cmp sp,0x7000
 jne fail

 ; source_page_cross: source ES:4fff, frame targets ES:6ffe/6ffc/6ffa
 mov ax,0x0000
 mov ss,ax
 mov es,ax
 mov sp,0x7000
 mov bp,0x5001
 mov word [es:0x4fff],0xabcd
 jmp .clean2
.clean2:
 nop
 nop
 nop
 nop
 mov al,4
 out 0x80,al
 enter 0,2
 mov al,5
 out 0x80,al
 cmp bp,0x6ffe
 jne fail
 cmp sp,0x6ffa
 jne fail
 cmp word [es:0x6ffe],0x5001
 jne fail
 cmp word [es:0x6ffc],0xabcd
 jne fail
 cmp word [es:0x6ffa],0x6ffe
 jne fail
 leave
 cmp bp,0x5001
 jne fail
 cmp sp,0x7000
 jne fail

 ; target_ram_line_cross: source ES:5000, frame targets ES:7001/6fff/6ffd
 mov ax,0x0000
 mov ss,ax
 mov es,ax
 mov sp,0x7003
 mov bp,0x5002
 mov word [es:0x5000],0xabcd
 jmp .clean3
.clean3:
 nop
 nop
 nop
 nop
 mov al,6
 out 0x80,al
 enter 0,2
 mov al,7
 out 0x80,al
 cmp bp,0x7001
 jne fail
 cmp sp,0x6ffd
 jne fail
 cmp word [es:0x7001],0x5002
 jne fail
 cmp word [es:0x6fff],0xabcd
 jne fail
 cmp word [es:0x6ffd],0x7001
 jne fail
 leave
 cmp bp,0x5002
 jne fail
 cmp sp,0x7003
 jne fail

 ; source_mmio: source ES:8000, frame targets ES:6ffe/6ffc/6ffa
 mov ax,0x9800
 mov ss,ax
 mov es,ax
 mov sp,0x7000
 mov bp,0x8002
 mov word [es:0x8000],0xabcd
 jmp .clean4
.clean4:
 nop
 nop
 nop
 nop
 mov al,8
 out 0x80,al
 enter 0,2
 mov al,9
 out 0x80,al
 cmp bp,0x6ffe
 jne fail
 cmp sp,0x6ffa
 jne fail
 cmp word [es:0x6ffe],0x8002
 jne fail
 cmp word [es:0x6ffc],0xabcd
 jne fail
 cmp word [es:0x6ffa],0x6ffe
 jne fail
 leave
 cmp bp,0x8002
 jne fail
 cmp sp,0x7000
 jne fail

 ; target_mmio: source ES:7000, frame targets ES:8004/8002/8000
 mov ax,0x9800
 mov ss,ax
 mov es,ax
 mov sp,0x8006
 mov bp,0x7002
 mov word [es:0x7000],0xabcd
 jmp .clean5
.clean5:
 nop
 nop
 nop
 nop
 mov al,10
 out 0x80,al
 enter 0,2
 mov al,11
 out 0x80,al
 cmp bp,0x8004
 jne fail
 cmp sp,0x8000
 jne fail
 cmp word [es:0x8004],0x7002
 jne fail
 cmp word [es:0x8002],0xabcd
 jne fail
 cmp word [es:0x8000],0x8004
 jne fail
 leave
 cmp bp,0x7002
 jne fail
 cmp sp,0x8006
 jne fail

 ; target_mmio_ram_cross: source ES:a000, frame targets ES:8001/7fff/7ffd
 mov ax,0xb800
 mov ss,ax
 mov es,ax
 mov sp,0x8003
 mov bp,0xa002
 mov word [es:0xa000],0xabcd
 jmp .clean6
.clean6:
 nop
 nop
 nop
 nop
 mov al,12
 out 0x80,al
 enter 0,2
 mov al,13
 out 0x80,al
 cmp bp,0x8001
 jne fail
 cmp sp,0x7ffd
 jne fail
 cmp word [es:0x8001],0xa002
 jne fail
 cmp word [es:0x7fff],0xabcd
 jne fail
 cmp word [es:0x7ffd],0x8001
 jne fail
 leave
 cmp bp,0xa002
 jne fail
 cmp sp,0x8003
 jne fail

 ; prefix_fallback: source ES:5000, frame targets ES:6ffe/6ffc/6ffa
 mov ax,0x0000
 mov ss,ax
 mov es,ax
 mov sp,0x7000
 mov bp,0x5002
 mov word [es:0x5000],0xabcd
 jmp .clean7
.clean7:
 nop
 nop
 nop
 nop
 mov al,14
 out 0x80,al
 db 0x26
 enter 0,2
 mov al,15
 out 0x80,al
 cmp bp,0x6ffe
 jne fail
 cmp sp,0x6ffa
 jne fail
 cmp word [es:0x6ffe],0x5002
 jne fail
 cmp word [es:0x6ffc],0xabcd
 jne fail
 cmp word [es:0x6ffa],0x6ffe
 jne fail
 leave
 cmp bp,0x5002
 jne fail
 cmp sp,0x7000
 jne fail
 cli
 xor al,al
 out 0x8f,al
 hlt
fail:
 cli
 mov al,0xee
 out 0x8f,al
 hlt
