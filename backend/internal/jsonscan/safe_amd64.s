//go:build amd64 && !purego

#include "textflag.h"

// No load crosses the string bounds. Signed byte comparison against 0x20
// rejects both control bytes and high-bit bytes (which are negative).
DATA space<>+0(SB)/8, $0x2020202020202020
DATA space<>+8(SB)/8, $0x2020202020202020
GLOBL space<>(SB), RODATA|NOPTR, $16
DATA quote<>+0(SB)/8, $0x2222222222222222
DATA quote<>+8(SB)/8, $0x2222222222222222
GLOBL quote<>(SB), RODATA|NOPTR, $16
DATA slash<>+0(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA slash<>+8(SB)/8, $0x5c5c5c5c5c5c5c5c
GLOBL slash<>(SB), RODATA|NOPTR, $16
DATA less<>+0(SB)/8, $0x3c3c3c3c3c3c3c3c
DATA less<>+8(SB)/8, $0x3c3c3c3c3c3c3c3c
GLOBL less<>(SB), RODATA|NOPTR, $16
DATA greater<>+0(SB)/8, $0x3e3e3e3e3e3e3e3e
DATA greater<>+8(SB)/8, $0x3e3e3e3e3e3e3e3e
GLOBL greater<>(SB), RODATA|NOPTR, $16
DATA amp<>+0(SB)/8, $0x2626262626262626
DATA amp<>+8(SB)/8, $0x2626262626262626
GLOBL amp<>(SB), RODATA|NOPTR, $16

TEXT ·safeVector(SB), NOSPLIT, $0-17
 MOVQ s_len+8(FP), CX
 CMPQ CX, $64
 JB sse2
 CMPB ·hasAVX2(SB), $0
 JE sse2
 JMP ·safeAVX2(SB)
sse2:
 MOVQ s_base+0(FP), SI
 MOVOU space<>(SB), X2
 MOVOU quote<>(SB), X3
 MOVOU slash<>(SB), X4
 MOVOU less<>(SB), X5
 MOVOU greater<>(SB), X6
 MOVOU amp<>(SB), X7
vector:
 CMPQ CX, $16
 JB tail
 MOVOU (SI), X0
 MOVOU X2, X1
 PCMPGTB X0, X1
 MOVOU X3, X8
 PCMPEQB X0, X8
 POR X8, X1
 MOVOU X4, X8
 PCMPEQB X0, X8
 POR X8, X1
 MOVOU X5, X8
 PCMPEQB X0, X8
 POR X8, X1
 MOVOU X6, X8
 PCMPEQB X0, X8
 POR X8, X1
 MOVOU X7, X8
 PCMPEQB X0, X8
 POR X8, X1
 PMOVMSKB X1, AX
 TESTL AX, AX
 JNZ unsafe
 ADDQ $16, SI
 SUBQ $16, CX
 JMP vector
tail:
 TESTQ CX, CX
 JZ safe
 MOVBLZX (SI), AX
 CMPB AX, $0x20
 JB unsafe
 CMPB AX, $0x80
 JAE unsafe
 CMPB AX, $0x22
 JE unsafe
 CMPB AX, $0x5c
 JE unsafe
 CMPB AX, $0x3c
 JE unsafe
 CMPB AX, $0x3e
 JE unsafe
 CMPB AX, $0x26
 JE unsafe
 INCQ SI
 DECQ CX
 JMP tail
safe:
 MOVB $1, ret+16(FP)
 RET
unsafe:
 MOVB $0, ret+16(FP)
 RET
