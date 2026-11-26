//go:build amd64 && !purego && (!linux || !cgo)

#include "textflag.h"

DATA blue<>+0(SB)/8, $0x000000ff000000ff
DATA blue<>+8(SB)/8, $0x000000ff000000ff
GLOBL blue<>(SB), RODATA|NOPTR, $16
DATA green<>+0(SB)/8, $0x0000ff000000ff00
DATA green<>+8(SB)/8, $0x0000ff000000ff00
GLOBL green<>(SB), RODATA|NOPTR, $16
DATA alpha<>+0(SB)/8, $0xff000000ff000000
DATA alpha<>+8(SB)/8, $0xff000000ff000000
GLOBL alpha<>(SB), RODATA|NOPTR, $16

// Four BGRX pixels per iteration. Caller supplies whole 16-byte blocks.
TEXT ·unpackBGRX32SSE2(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), DI
	MOVQ src_base+24(FP), SI
	MOVQ src_len+32(FP), CX
	MOVOU blue<>(SB), X3
	MOVOU green<>(SB), X4
	MOVOU alpha<>(SB), X5
loop:
	TESTQ CX, CX
	JZ done
	MOVOU (SI), X0
	MOVOU X0, X1
	MOVOU X0, X2
	PAND X3, X0
	PSLLL $16, X0
	PSRLL $16, X1
	PAND X3, X1
	PAND X4, X2
	POR X1, X0
	POR X2, X0
	POR X5, X0
	MOVOU X0, (DI)
	ADDQ $16, SI
	ADDQ $16, DI
	SUBQ $16, CX
	JMP loop
done:
	RET

TEXT ·cpuHasSSSE3(SB), NOSPLIT, $0-1
	MOVL $1, AX
	XORL CX, CX
	CPUID
	SHRL $9, CX
	ANDL $1, CX
	MOVB CL, ret+0(FP)
	RET

DATA shuffle24<>+0(SB)/8, $0x8003040580000102
DATA shuffle24<>+8(SB)/8, $0x80090a0b80060708
GLOBL shuffle24<>(SB), RODATA|NOPTR, $16
DATA opaque24<>+0(SB)/8, $0xff000000ff000000
DATA opaque24<>+8(SB)/8, $0xff000000ff000000
GLOBL opaque24<>(SB), RODATA|NOPTR, $16

// No vector load crosses the supplied row. Four BGR pixels become RGBA.
TEXT ·unpackBGR24SSSE3(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), DI
	MOVQ src_base+24(FP), SI
	MOVQ src_len+32(FP), CX
	MOVOU shuffle24<>(SB), X1
	MOVOU opaque24<>(SB), X2
loop:
	CMPQ CX, $16
	JB done
	MOVOU (SI), X0
	PSHUFB X1, X0
	POR X2, X0
	MOVOU X0, (DI)
	ADDQ $12, SI
	ADDQ $16, DI
	SUBQ $12, CX
	JMP loop
done:
	RET
