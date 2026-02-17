//go:build linux && !cgo && amd64

#include "textflag.h"

TEXT ·cCall0(SB), NOSPLIT, $0-16
	MOVQ fn+0(FP), AX
	CALL AX
	MOVQ AX, ret+8(FP)
	RET

TEXT ·cCall1(SB), NOSPLIT, $0-24
	MOVQ fn+0(FP), AX
	MOVQ a0+8(FP), DI
	CALL AX
	MOVQ AX, ret+16(FP)
	RET

TEXT ·cCall2(SB), NOSPLIT, $0-32
	MOVQ fn+0(FP), AX
	MOVQ a0+8(FP), DI
	MOVQ a1+16(FP), SI
	CALL AX
	MOVQ AX, ret+24(FP)
	RET
