//go:build darwin && !ios && amd64 && !cgo

#include "textflag.h"

TEXT ·cCall10(SB), NOSPLIT, $0-96
	MOVQ fn+0(FP), AX
	MOVQ a0+8(FP), DI
	MOVQ a1+16(FP), SI
	MOVQ a2+24(FP), DX
	MOVQ a3+32(FP), CX
	MOVQ a4+40(FP), R8
	MOVQ a5+48(FP), R9

	// Keep SysV stack alignment before calling C++ dyld internals:
	// reserve 32 bytes for stack args (a6-a9) plus 8 bytes padding.
	SUBQ $40, SP
	// These offsets include the 40 bytes just reserved. FP arguments are
	// otherwise assembled relative to the current SP and would read a1-a4.
	MOVQ 112(SP), R10 // a6
	MOVQ R10, 0(SP)
	MOVQ 120(SP), R10 // a7
	MOVQ R10, 8(SP)
	MOVQ 128(SP), R10 // a8
	MOVQ R10, 16(SP)
	MOVQ 136(SP), R10 // a9
	MOVQ R10, 24(SP)

	CALL AX

	ADDQ $40, SP
	MOVQ AX, ret+88(FP)
	RET
