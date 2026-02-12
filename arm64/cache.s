// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "arm64.h"

// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
// D12.2.100 SCTLR_EL2, System Control Register (EL2)
// D12.2.102 SCTLR_EL3, System Control Register (EL3)

// func cache_disable()
TEXT ·cache_disable(SB),$0
	MRS	CurrentEL, R1
	LSR	$2, R1, R1
	AND	$0b11, R1, R1
	CMP	$3, R1
	BEQ	el3_cd

	// EL2
	WORD	$0xd53c1000	// mrs x0, sctlr_el2
	BIC	$1<<12, R0	// disable I-cache
	BIC	$1<<2, R0	// disable D-cache
	WORD	$0xd51c1000	// msr sctlr_el2, x0
	ISB	SY
	RET

el3_cd:
	WORD	$0xd53e1000	// mrs x0, sctlr_el3
	BIC	$1<<12, R0	// disable I-cache
	BIC	$1<<2, R0	// disable D-cache
	WORD	$0xd51e1000	// msr sctlr_el3, x0
	ISB	SY
	RET

// func cache_enable()
TEXT ·cache_enable(SB),$0
	MRS	CurrentEL, R1
	LSR	$2, R1, R1
	AND	$0b11, R1, R1
	CMP	$3, R1
	BEQ	el3_ce

	// EL2
	WORD	$0xd53c1000	// mrs x0, sctlr_el2
	ORR	$1<<12, R0	// enable I-cache
	ORR	$1<<2, R0	// enable D-cache
	WORD	$0xd51c1000	// msr sctlr_el2, x0
	ISB	SY
	RET

el3_ce:
	WORD	$0xd53e1000	// mrs x0, sctlr_el3
	ORR	$1<<12, R0	// enable I-cache
	ORR	$1<<2, R0	// enable D-cache
	WORD	$0xd51e1000	// msr sctlr_el3, x0
	ISB	SY
	RET
