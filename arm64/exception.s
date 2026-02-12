// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "go_asm.h"
#include "textflag.h"

#define PAD					 	\
	WORD	$0xd503201f; WORD	$0xd503201f	\ // nop; nop
	WORD	$0xd503201f; WORD	$0xd503201f	\ // ...
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f; WORD	$0xd503201f	\
	WORD	$0xd503201f

TEXT ·handleException(SB),NOSPLIT|NOFRAME,$0
	MRS	CurrentEL, R1
	LSR	$2, R1, R1
	AND	$0b11, R1, R1
	CMP	$3, R1
	BEQ	el3_exc

	// EL2
	WORD	$0xd53c4020	// mrs x0, elr_el2
	B	exc_cont

el3_exc:
	WORD	$0xd53e4020	// mrs x0, elr_el3

exc_cont:
	MOVD	R0, 8(RSP)	// arg
	JMP	·systemException(SB)

// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
// Table D1-7 Vector offsets from vector table base address
TEXT ·vectorTable(SB),NOSPLIT|NOFRAME,$0
	// EL0
	JMP	·handleException(SB); PAD // Synchronous Exception
	JMP	·handleInterrupt(SB); PAD // IRQ or vIRQ
	JMP	·handleInterrupt(SB); PAD // FIQ or vFIQ
	JMP	·handleException(SB); PAD // SError or vSError

	// ELx, x>0
	JMP	·handleException(SB); PAD // Synchronous Exception
	JMP	·handleInterrupt(SB); PAD // IRQ or vIRQ
	JMP	·handleInterrupt(SB); PAD // FIQ or vFIQ
	JMP	·handleException(SB); PAD // SError or vSError

// func set_vbar()
TEXT ·set_vbar(SB),NOSPLIT,$0
	MOVD	$·vectorTable(SB), R0

	MRS	CurrentEL, R1
	LSR	$2, R1, R1
	AND	$0b11, R1, R1
	CMP	$3, R1
	BEQ	el3_vbar

	// EL2
	WORD	$0xd51cc000	// msr vbar_el2, x0
	RET

el3_vbar:
	WORD	$0xd51ec000	// msr vbar_el3, x0
	RET

// func read_el() uint64
TEXT ·read_el(SB),$0-8
	MRS	CurrentEL, R0
	MOVD	R0, ret+0(FP)
	RET
