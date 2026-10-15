//go:build linux && loong64

#include "textflag.h"

TEXT ·Flush(SB),NOSPLIT,$0-0
	WORD $0x38728000 // ibar 0
	RET
