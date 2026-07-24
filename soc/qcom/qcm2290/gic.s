// Qualcomm QCM2290 interrupt routing
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "../../../arm64/arm64.h"

// func write_icc_igrpen1_el1(val uint64)
TEXT ·write_icc_igrpen1_el1(SB),$0-8
	// ARM IHI 0069G
	// 12.2.16 ICC_IGRPEN1_EL1, Interrupt Controller Interrupt Group 1 Enable register
	MOVD	val+0(FP), R0
	MSR	R0, ICC_IGRPEN1_EL1
	ISB	SY

	RET

// func write_icc_sre_el1(val uint64)
TEXT ·write_icc_sre_el1(SB),$0-8
	// ARM IHI 0069G
	// 12.2.22 ICC_SRE_EL1, Interrupt Controller System Register Enable register (EL1)
	MOVD	val+0(FP), R0
	MSR	R0, ICC_SRE_EL1
	ISB	SY

	RET

// func write_icc_pmr_el1(val uint64)
TEXT ·write_icc_pmr_el1(SB),$0-8
	// ARM IHI 0069G
	// 12.2.18 ICC_PMR_EL1, Interrupt Controller Interrupt Priority Mask Register
	MOVD	val+0(FP), R0
	MSR	R0, ICC_PMR_EL1
	ISB	SY

	RET
