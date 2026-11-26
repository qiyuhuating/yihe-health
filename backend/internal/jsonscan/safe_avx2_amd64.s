//go:build amd64 && !purego

#include "textflag.h"

// Each low nibble selects a bitset of forbidden high-nibble classes.
// Bits 0, 1, 2, 3 denote control/non-ASCII, 0x2_, 0x3_, 0x5_.
DATA avxLow<>+0(SB)/8, $0x0103010101030101
DATA avxLow<>+8(SB)/8, $0x0105010d01010101
GLOBL avxLow<>(SB), RODATA|NOPTR, $16
DATA avxHigh<>+0(SB)/8, $0x0000080004020101
DATA avxHigh<>+8(SB)/8, $0x0101010101010101
GLOBL avxHigh<>(SB), RODATA|NOPTR, $16
DATA nibbleMask<>+0(SB)/1, $0x0f
GLOBL nibbleMask<>(SB), RODATA|NOPTR, $1

// Check both CPU support and OS preservation of XMM/YMM state.
TEXT ·cpuHasAVX2(SB), NOSPLIT, $0-1
 XORL AX, AX
 CPUID
 CMPL AX, $7
 JB unavailable
 MOVL $1, AX
 CPUID
 ANDL $0x18000000, CX
 CMPL CX, $0x18000000
 JNE unavailable
 XORL CX, CX
 XGETBV
 ANDL $6, AX
 CMPL AX, $6
 JNE unavailable
 MOVL $7, AX
 XORL CX, CX
 CPUID
 TESTL $0x20, BX
 JZ unavailable
 MOVB $1, ret+0(FP)
 RET
unavailable:
 MOVB $0, ret+0(FP)
 RET

// Caller guarantees at least 32 bytes. The final load may overlap the
// preceding block but never extends past the end of the string.
TEXT ·safeAVX2(SB), NOSPLIT, $0-17
 MOVQ s_base+0(FP), SI
 MOVQ s_len+8(FP), CX
 LEAQ -32(SI)(CX*1), DI
 VBROADCASTI128 avxLow<>(SB), Y2
 VBROADCASTI128 avxHigh<>(SB), Y3
 VPBROADCASTB nibbleMask<>(SB), Y4
vector:
 VMOVDQU (SI), Y0
 VPSRLW $4, Y0, Y1
 VPAND Y4, Y0, Y0
 VPAND Y4, Y1, Y1
 VPSHUFB Y0, Y2, Y0
 VPSHUFB Y1, Y3, Y1
 VPTEST Y0, Y1
 JNZ unsafe
 CMPQ SI, DI
 JE safe
 ADDQ $32, SI
 CMPQ SI, DI
 CMOVQHI DI, SI
 JMP vector
safe:
 VZEROUPPER
 MOVB $1, ret+16(FP)
 RET
unsafe:
 VZEROUPPER
 MOVB $0, ret+16(FP)
 RET
