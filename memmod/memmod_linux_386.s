//go:build linux && 386

#include "textflag.h"

TEXT ·cCall0(SB), NOSPLIT, $0-8
	MOVL fn+0(FP), AX
	CALL AX
	MOVL AX, ret+4(FP)
	RET

TEXT ·cCall1(SB), NOSPLIT, $0-12
	MOVL fn+0(FP), AX
	MOVL a0+4(FP), BX
	SUBL $4, SP
	MOVL BX, 0(SP)
	CALL AX
	ADDL $4, SP
	MOVL AX, ret+8(FP)
	RET

TEXT ·cCall2(SB), NOSPLIT, $0-16
	MOVL fn+0(FP), AX
	MOVL a0+4(FP), BX
	MOVL a1+8(FP), CX
	SUBL $8, SP
	MOVL BX, 0(SP)
	MOVL CX, 4(SP)
	CALL AX
	ADDL $8, SP
	MOVL AX, ret+12(FP)
	RET
