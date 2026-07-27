// Allwinner H616 CPU configuration (bare-metal secondary core release)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// CPU configuration registers (cluster 0), per the H616 user manual and
// TF-A plat/allwinner (sunxi_cpucfg_ncat.h, sunxi_cpu_ops.c). R_CPUCFG
// hosts the power-domain controls.
const (
	R_CPUCFG_BASE = 0x07000400

	cpucfgRSTCTRL  = CPUCFG_BASE + 0x0000 // core reset, bit n
	cpucfgCLSCTRL0 = CPUCFG_BASE + 0x0010 // AA64nAA32, bit 24+n
	cpucfgRVBARLO  = CPUCFG_BASE + 0x0040 // + n*8
	cpucfgRVBARHI  = CPUCFG_BASE + 0x0044 // + n*8
	cpucfgDBG0     = CPUCFG_BASE + 0x00c0 // DBGPWRDUP, bit n

	rcpucfgPWRONRST  = R_CPUCFG_BASE + 0x0040 // power-on reset, bit n
	rcpucfgPWROFFGAT = R_CPUCFG_BASE + 0x0044 // output clamp gating, bit n
	rcpucfgPWRCLAMP  = R_CPUCFG_BASE + 0x0050 // + n*4
)

// CPUOnBare releases a parked secondary core through the CPUCFG/R_CPUCFG
// power sequence (the same one TF-A's PSCI implementation uses), without
// any secure-world involvement. The core comes out of reset at EL3,
// MMU and caches off, executing at the given physical entry address.
//
// Unlike PSCI CPU_ON there is no context argument: the entry code must
// find its control block at a known address.
func CPUOnBare(core int, entry uint64) {
	n := uint32(core)

	// reset vector for the released core
	reg.Write(cpucfgRVBARLO+n*8, uint32(entry))
	reg.Write(cpucfgRVBARHI+n*8, uint32(entry>>32))

	// assert core reset and power-on reset
	reg.SetTo(cpucfgRSTCTRL, core, false)
	reg.SetTo(rcpucfgPWRONRST, core, false)

	// AArch64 state
	reg.SetTo(cpucfgCLSCTRL0, 24+core, true)

	// power enable sequence (from the original Allwinner sources, via
	// TF-A sunxi_cpu_enable_power)
	reg.Write(rcpucfgPWRCLAMP+n*4, 0xfe)
	reg.Write(rcpucfgPWRCLAMP+n*4, 0xf8)
	reg.Write(rcpucfgPWRCLAMP+n*4, 0xe0)
	reg.Write(rcpucfgPWRCLAMP+n*4, 0x80)
	reg.Write(rcpucfgPWRCLAMP+n*4, 0x00)
	time.Sleep(time.Microsecond)

	// release the core output clamps
	reg.SetTo(rcpucfgPWROFFGAT, core, false)

	// deassert power-on reset, then core reset
	reg.SetTo(rcpucfgPWRONRST, core, true)
	reg.SetTo(cpucfgRSTCTRL, core, true)

	// DBGPWRDUP
	reg.SetTo(cpucfgDBG0, core, true)
}