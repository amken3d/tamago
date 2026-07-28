// Qualcomm QCM2290 DPU (display processor) single-layer scanout
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

// DPU block offsets from the MDP base (dpu_6_5_qcm2290.h catalog).
// The static configuration replays a live register snapshot of this
// hardware scanning out 1080p60 XRGB8888 through VIG0 -> LM0 -> PP0 ->
// INTF1 (DSI); only the source addresses are repointed at the caller's
// framebuffer.
const (
	DPU_BASE = 0x05e01000

	dpuTop   = 0x0
	dpuCTL0  = 0x1000
	dpuVIG0  = 0x4000
	dpuLM0   = 0x44000
	dpuPP0   = 0x70000
	dpuINTF1 = 0x6a800

	// CTL kickoff registers (excluded from the snapshot)
	ctlFlush     = 0x018
	ctlStart     = 0x01c
	ctlIntfFlush = 0x110

	// VIG0 source address registers (repointed)
	vigSrc0Addr = 0x014
	vigSrcAddrB = 0x0a4

	// INTF timing engine enable (written last)
	intfTimingEn = 0x000

	// display SMMU (MMU-500): global bypass via sCR0.CLIENTPD --
	// scanout then fetches physical addresses directly
	SMMU_BASE    = 0x0c600000
	smmuSCR0     = 0x0
	smmuClientPD = 0
)

var dpuTopSnap = []regVal{
	{0x00c, 0x00000200}, {0x014, 0x08000100}, {0x19c, 0x00000455},
	{0x1e8, 0x00000038}, {0x1ec, 0x00000001}, {0x2ac, 0x40000444},
	{0x2b0, 0x00000001}, {0x2b4, 0x40000444}, {0x2bc, 0x44444444},
	{0x2c4, 0x00004444}, {0x2e0, 0x00011111}, {0x300, 0x00010e00},
	{0x304, 0x0001008b}, {0x308, 0x00010944}, {0x364, 0x51500555},
	{0x374, 0x00000001}, {0x384, 0x00000002}, {0x394, 0x00000002},
	{0x3a8, 0x44444044}, {0x3ac, 0x00090080}, {0x3b0, 0x44444044},
	{0x3b8, 0x44444040}, {0x3d0, 0x00444000}, {0x3d8, 0x000000e4},
	{0x3dc, 0x000000e4}, {0x3e0, 0x00004000}, {0x3e4, 0x00004000},
	{0x3f8, 0x00000001}, {0x424, 0x00000002}, {0x434, 0x00000002},
	{0x444, 0x00000002}, {0x484, 0x00000002}, {0x488, 0x00000011},
}

var dpuCTL0Snap = []regVal{
	{0x000, 0x01000002}, {0x03c, 0x00000001}, {0x064, 0x00000001},
	{0x0d8, 0xc068e038}, {0x0dc, 0x1f971fc7}, {0x0e0, 0x00000001},
	{0x0f4, 0x00000002},
}

var dpuVIG0Snap = []regVal{
	{0x000, 0x04380780}, {0x00c, 0x04380780}, {0x024, 0x00001e00},
	{0x030, 0x000236ff}, {0x034, 0x03020001}, {0x038, 0x80000000},
	{0x044, 0xc0000101}, {0x048, 0x00000087}, {0x04c, 0x00000707},
	{0x060, 0x000000ff}, {0x064, 0x0000fff0}, {0x06c, 0x00000001},
	{0x074, 0x22335777}, {0x078, 0x00112222}, {0x0d0, 0x00054a80},
	{0x0e0, 0x00054527}, {0x0f0, 0x000f000f}, {0x0f4, 0x00010030},
	{0x0f8, 0x02d102d1}, {0x108, 0x04380780}, {0x118, 0x04380780},
	{0x128, 0x04380780}, {0x134, 0x00000009}, {0x13c, 0x0000000f},
	{0x17c, 0x80000000}, {0x194, 0x00001001}, {0x198, 0x00ff0000},
	{0x19c, 0x00400040}, {0x1a0, 0x000000ff}, {0x1a4, 0x00000024},
	{0x1a8, 0x00ffffff}, {0x1bc, 0x00000010}, {0x1f0, 0x00070502},
	{0x1f4, 0x00070502},
}

var dpuLM0Snap = []regVal{
	{0x000, 0x00000002}, {0x004, 0x04380780}, {0x020, 0x00000100},
	{0x024, 0x00ff0000},
}

var dpuPP0Snap = []regVal{
	{0x0ac, 0x0002c688}, {0x0c8, 0x0002c688},
}

var dpuINTF1Snap = []regVal{
	{0x004, 0x00800000}, {0x008, 0x0898002c}, {0x00c, 0x0025c3f8},
	{0x014, 0x00002af8}, {0x01c, 0x00016058}, {0x024, 0x0025a197},
	{0x03c, 0x083f00c0}, {0x048, 0x000000ff}, {0x060, 0x00000010},
	{0x064, 0x083f00c0}, {0x08c, 0x00000011}, {0x090, 0x00002100},
	{0x094, 0x76543210}, {0x0ac, 0x0000554a}, {0x0b0, 0x0000033b},
	{0x0b8, 0x00000010}, {0x178, 0xffffffff}, {0x180, 0x01000000},
	{0x1c4, 0x00000a00}, {0x1d8, 0x00000002}, {0x1dc, 0x00020001},
	{0x1f0, 0xffffffff}, {0x250, 0xffffffff}, {0x25c, 0x000f0000},
	{0x268, 0xffffffff}, {0x26c, 0x00000001},
}

// SMMUBypass makes unmatched streams bypass translation (physical
// addressing) by clearing sCR0.USFCFG -- and ensures CLIENTPD stays
// clear: setting it TERMINATES all client transactions, and the DPU's
// aborted fetch escalates to a fatal NoC error (learned by ramdump).
// Returns the previous sCR0 value.
func SMMUBypass() uint32 {
	old := reg.Read(SMMU_BASE + smmuSCR0)
	reg.Write(SMMU_BASE+smmuSCR0, old&^(1<<10|1<<0))

	return old
}

// InitDPU replays the scanout pipeline configuration with the VIG0
// source repointed at the framebuffer (physical address; SMMUBypass
// first). The display power domain must be up.
func InitDPU(fb uint32) {
	replay(DPU_BASE+dpuTop, dpuTopSnap)
	replay(DPU_BASE+dpuCTL0, dpuCTL0Snap)
	replay(DPU_BASE+dpuVIG0, dpuVIG0Snap)

	reg.Write(DPU_BASE+dpuVIG0+vigSrc0Addr, fb)
	reg.Write(DPU_BASE+dpuVIG0+vigSrcAddrB, fb)

	replay(DPU_BASE+dpuLM0, dpuLM0Snap)
	replay(DPU_BASE+dpuPP0, dpuPP0Snap)
	replay(DPU_BASE+dpuINTF1, dpuINTF1Snap)
}

// StartDPU flushes the configuration and starts the interface timing
// engine: pixels begin streaming into the DSI controller.
func StartDPU() {
	reg.Write(DPU_BASE+dpuCTL0+ctlIntfFlush, 1<<1) // INTF1
	reg.Write(DPU_BASE+dpuCTL0+ctlFlush, 0x7fffffff)
	reg.Write(DPU_BASE+dpuCTL0+ctlStart, 1)

	reg.Write(DPU_BASE+dpuINTF1+intfTimingEn, 1)
}

// DPUStatus returns the INTF line count (advancing = scanning) and
// the CTL flush register (0 = flush consumed).
func DPUStatus() (line, flush uint32) {
	line = reg.Read(DPU_BASE + dpuINTF1 + 0x0b4)
	flush = reg.Read(DPU_BASE + dpuCTL0 + ctlFlush)

	return
}

// EnableMDPClocks brings up dispcc PLL0 (768 MHz) and the MDP core
// clock at 384 MHz plus its branches, required for DPU operation.
func EnableMDPClocks() error {
	pll := uint32(DISPCC_BASE) // PLL0 at dispcc base

	// L, user/config control (snapshot values; alpha = 0 -> 768 MHz
	// integer mode from the 19.2 MHz XO)
	reg.Write(pll+0x04, 0x28)
	reg.Write(pll+0x10, 0x00200001)
	reg.Write(pll+0x14, 0x00000004)
	reg.Write(pll+0x18, 0x4001055b)

	// alpha PLL enable: BYPASSNL -> RESET_N -> lock -> OUTCTRL
	reg.Set(pll+0x00, 1)
	udelaySpin(10)
	reg.Set(pll+0x00, 2)
	udelaySpin(50)

	locked := false

	for i := 0; i < 1000; i++ {
		if reg.Get(pll+0x00, 31) {
			locked = true
			break
		}

		udelaySpin(10)
	}

	if !locked {
		return errors.New("dispcc PLL0 lock timeout")
	}

	reg.Set(pll+0x00, 0)

	// MDP RCG: PLL0/2 = 384 MHz (src 1, div field 3), then branches
	if err := rcgUpdate(DISPCC_BASE, 0x2074, 1<<8|3); err != nil {
		return errors.New("mdp rcg: " + err.Error())
	}

	if err := dispClkOn(DISPCC_BASE, 0x2008); err != nil { // mdp
		return errors.New("mdp branch: " + err.Error())
	}

	dispClkOn(DISPCC_BASE, 0x2010) // mdp lut
	dispClkOn(DISPCC_BASE, 0x2018) // vsync

	if err := rcgUpdate(DISPCC_BASE, 0x208c, cfgSrcXODiv1); err != nil { // vsync rcg
		return errors.New("vsync rcg: " + err.Error())
	}

	return nil
}
