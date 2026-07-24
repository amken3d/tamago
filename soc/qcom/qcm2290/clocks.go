// Qualcomm QCM2290 Global Clock Controller support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"errors"

	"github.com/usbarmory/tamago/internal/reg"
)

// GCC registers for the QUP0 SE5 serial clock
// (drivers/clk/qcom/gcc-qcm2290.c)
const (
	GCC_BASE = 0x01400000

	// HLOS branch enable vote register
	GCC_APCS_BRANCH_ENA_VOTE = 0x7900c
	QUP0_S5_CLK_ENA          = 15

	// gcc_qupv3_wrap0_s5_clk branch halt status
	GCC_QUP0_S5_CBCR = 0x1f734
	CLK_OFF          = 31

	// gcc_qupv3_wrap0_s5_clk_src RCG
	GCC_QUP0_S5_CMD_RCGR = 0x1f738
	CMD_UPDATE           = 0
	// CFG_RCGR: SRC_SEL [10:8] (0 = XO 19.2 MHz), SRC_DIV [4:0]
	// (2*div-1; 1 = divide by 1)
	cfgXODiv1 = 0<<8 | 1
)

const clkPollSpins = 1000000

// QUP0 SE clock register layout: per-SE RCGs are 0x130 apart starting
// at S0 (gcc-qcm2290.c gcc_qupv3_wrap0_sN_clk_src), the branch CBCR
// precedes each cmd_rcgr, and the HLOS vote bits are contiguous from
// S0 = bit 10 in GCC_APCS_BRANCH_ENA_VOTE.
const (
	qup0S0CmdRCGR  = 0x1f148
	qup0RCGRStride = 0x130
	qup0S0VoteBit  = 10
)

// EnableSEClock configures and enables a QUP0 serial engine clock
// (SE 0-5): the RCG root is set to the 19.2 MHz crystal and the branch
// is voted on.
//
// U-Boot only clocks the debug UART's serial engine; every other SE's
// GCC branch is off until enabled here, with the symptom of a FIFO
// that accepts writes but never shifts.
func EnableSEClock(se int) error {
	if se < 0 || se > 5 {
		return errors.New("invalid serial engine")
	}

	cmdRCGR := uint32(GCC_BASE + qup0S0CmdRCGR + se*qup0RCGRStride)

	// root: XO source, divide by 1
	reg.Write(cmdRCGR+0x4, cfgXODiv1)

	reg.Set(cmdRCGR, CMD_UPDATE)

	for i := 0; ; i++ {
		if !reg.Get(cmdRCGR, CMD_UPDATE) {
			break
		}

		if i >= clkPollSpins {
			return errors.New("RCG update timeout")
		}
	}

	// branch: HLOS vote
	reg.Set(GCC_BASE+GCC_APCS_BRANCH_ENA_VOTE, qup0S0VoteBit+se)

	for i := 0; ; i++ {
		if !reg.Get(cmdRCGR-0x4, CLK_OFF) {
			break
		}

		if i >= clkPollSpins {
			return errors.New("SE branch stuck off")
		}
	}

	return nil
}

// EnableSPI5Clock enables the SE5 serial clock (the header SPI).
func EnableSPI5Clock() error {
	return EnableSEClock(5)
}
