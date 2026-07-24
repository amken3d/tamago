// Qualcomm QCM2290 virtual timer alarm
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func read_cntvct() uint64
TEXT ·read_cntvct(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.8.26 CNTVCT_EL0, Counter-timer Virtual Count register
	ISB	$0b1111
	MRS	CNTVCT_EL0, R0
	MOVD	R0, ret+0(FP)

	RET

// func write_cntvtval(val uint32, enable bool)
TEXT ·write_cntvtval(SB),$0-5
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.8.28 CNTV_TVAL_EL0, Counter-timer Virtual Timer TimerValue register
	MOVW	val+0(FP), R0
	MOVB	enable+4(FP), R1

	MSR	R0, CNTV_TVAL_EL0
	MSR	R1, CNTV_CTL_EL0

	RET

// func read_cntvctl() uint64
TEXT ·read_cntvctl(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.8.27 CNTV_CTL_EL0, Counter-timer Virtual Timer Control register
	ISB	$0b1111
	MRS	CNTV_CTL_EL0, R0
	MOVD	R0, ret+0(FP)

	RET
