// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "go_asm.h"

// func fp_enable()
TEXT ·fp_enable(SB),$0
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.29 CPACR_EL1, Architectural Feature Access Control Register
	MRS	CPACR_EL1, R0
	ORR	$(3 << 20), R0	// set CPACR_EL1.FPEN
	MSR	R0, CPACR_EL1
	ISB	$1

	// At EL2, also clear CPTR_EL2.TFP to avoid trapping FP operations.
	// D12.2.31 CPTR_EL2, Architectural Feature Trap Register (EL2)
	MRS	CurrentEL, R0
	LSR	$2, R0, R0
	AND	$0b11, R0, R0
	CMP	$2, R0
	BNE	fp_done

	WORD	$0xd53c1140	// mrs x0, cptr_el2
	BIC	$1<<10, R0	// clear TFP bit
	WORD	$0xd51c1140	// msr cptr_el2, x0
	ISB	$1

fp_done:
	RET
