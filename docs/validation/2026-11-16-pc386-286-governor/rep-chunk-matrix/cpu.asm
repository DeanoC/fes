bits 16
cpu 386
org 0x1000
start:
  cli
  cld
  xor ax,ax
  mov ds,ax
  mov es,ax
  mov di,0x8100
  mov cx,512
  mov al,0xa5
  rep stosb
  mov word [0x8000],1000
  mov al,0
  out 0x80,al
.loop0:
  mov cx,28
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep movsb
  dec word [0x8000]
  jne .loop0
  mov al,1
  out 0x80,al
  mov word [0x8000],1000
  mov al,2
  out 0x80,al
.loop1:
  mov cx,40
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep movsb
  dec word [0x8000]
  jne .loop1
  mov al,3
  out 0x80,al
  mov word [0x8000],1000
  mov al,4
  out 0x80,al
.loop2:
  mov cx,54
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep movsb
  dec word [0x8000]
  jne .loop2
  mov al,5
  out 0x80,al
  mov word [0x8000],1000
  mov al,6
  out 0x80,al
.loop3:
  mov cx,80
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep movsb
  dec word [0x8000]
  jne .loop3
  mov al,7
  out 0x80,al
  mov word [0x8000],1000
  mov al,8
  out 0x80,al
.loop4:
  mov cx,1
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep stosb
  dec word [0x8000]
  jne .loop4
  mov al,9
  out 0x80,al
  mov word [0x8000],1000
  mov al,10
  out 0x80,al
.loop5:
  mov cx,8
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep stosb
  dec word [0x8000]
  jne .loop5
  mov al,11
  out 0x80,al
  mov word [0x8000],1000
  mov al,12
  out 0x80,al
.loop6:
  mov cx,22
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep stosb
  dec word [0x8000]
  jne .loop6
  mov al,13
  out 0x80,al
  mov word [0x8000],1000
  mov al,14
  out 0x80,al
.loop7:
  mov cx,40
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep stosb
  dec word [0x8000]
  jne .loop7
  mov al,15
  out 0x80,al
  mov word [0x8000],1000
  mov al,16
  out 0x80,al
.loop8:
  mov cx,1
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep lodsb
  dec word [0x8000]
  jne .loop8
  mov al,17
  out 0x80,al
  mov word [0x8000],1000
  mov al,18
  out 0x80,al
.loop9:
  mov cx,8
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep lodsb
  dec word [0x8000]
  jne .loop9
  mov al,19
  out 0x80,al
  mov word [0x8000],1000
  mov al,20
  out 0x80,al
.loop10:
  mov cx,22
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep lodsb
  dec word [0x8000]
  jne .loop10
  mov al,21
  out 0x80,al
  mov word [0x8000],1000
  mov al,22
  out 0x80,al
.loop11:
  mov cx,40
  mov si,0x8100
  mov di,0x8200
  mov al,165
  rep lodsb
  dec word [0x8000]
  jne .loop11
  mov al,23
  out 0x80,al
  mov word [0x8000],1000
  mov al,24
  out 0x80,al
.loop12:
  mov cx,1
  mov si,0x8100
  mov di,0x8200
  mov al,0
  repne scasb
  dec word [0x8000]
  jne .loop12
  mov al,25
  out 0x80,al
  mov word [0x8000],1000
  mov al,26
  out 0x80,al
.loop13:
  mov cx,8
  mov si,0x8100
  mov di,0x8200
  mov al,0
  repne scasb
  dec word [0x8000]
  jne .loop13
  mov al,27
  out 0x80,al
  mov word [0x8000],1000
  mov al,28
  out 0x80,al
.loop14:
  mov cx,14
  mov si,0x8100
  mov di,0x8200
  mov al,0
  repne scasb
  dec word [0x8000]
  jne .loop14
  mov al,29
  out 0x80,al
  mov word [0x8000],1000
  mov al,30
  out 0x80,al
.loop15:
  mov cx,28
  mov si,0x8100
  mov di,0x8200
  mov al,0
  repne scasb
  dec word [0x8000]
  jne .loop15
  mov al,31
  out 0x80,al
  mov al,0
  out 0x8f,al
  hlt
