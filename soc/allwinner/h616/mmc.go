// Allwinner H616 SMHC (SD/MMC host controller) driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"fmt"
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// SMHC0 drives the CB1's micro-SD slot (the boot card). The controller
// is the classic sunxi SMHC in the H6-generation layout (FIFO at
// +0x200), operated in new timing mode (NTSR bit 31) with the internal
// divider cleared -- the configuration mainline U-Boot uses on this
// SoC family. Polled PIO only; the card clock tops out at 24 MHz from
// OSC24M, which is plenty for the persistence store and keeps the
// clock tree trivial (no PLL_PERIPH math).
//
// The BROM and SPL have already used the card (pins muxed, card
// powered); Init re-runs the identification sequence from scratch for
// a clean, known state.
const (
	SMHC0_BASE = 0x04020000

	// CCU (ccu-sun50i-h616.c): mod clock + bus gate/reset
	smhc0ClkReg = CCU_BASE + 0x830 // ENABLE bit31, mux 25:24 (0=OSC24M), N 9:8, M 3:0
	smhc0BGR    = CCU_BASE + 0x84c // gate bit0, reset bit16

	smhcClkEnable = 31
	smhc0Gate     = 0
	smhc0Rst      = 16
)

// SMHC registers (offsets)
const (
	smhcGCTRL   = 0x00
	smhcCLKCR   = 0x04
	smhcTIMEOUT = 0x08
	smhcWIDTH   = 0x0c
	smhcBLKSZ   = 0x10
	smhcBYTECNT = 0x14
	smhcCMD     = 0x18
	smhcARG     = 0x1c
	smhcRESP0   = 0x20
	smhcIMASK   = 0x30
	smhcRINT    = 0x38
	smhcSTATUS  = 0x3c
	smhcNTSR    = 0x5c
	smhcSAMPDL  = 0x144
	smhcFIFO    = 0x200
)

// register bits (U-Boot drivers/mmc/sunxi_mmc.h)
const (
	gctrlSoftReset = 1 << 0
	gctrlFIFOReset = 1 << 1
	gctrlDMAReset  = 1 << 2
	gctrlAHBAccess = 1 << 31

	clkEnable = 1 << 16

	cmdRespExpire  = 1 << 6
	cmdLongResp    = 1 << 7
	cmdChkRespCRC  = 1 << 8
	cmdDataExpire  = 1 << 9
	cmdWrite       = 1 << 10
	cmdAutoStop    = 1 << 12
	cmdWaitPreOver = 1 << 13
	cmdSendInitSeq = 1 << 15
	cmdUpClkOnly   = 1 << 21
	cmdStart       = 1 << 31

	rintRespErr   = 1 << 1
	rintCmdDone   = 1 << 2
	rintDataOver  = 1 << 3
	rintRespCRC   = 1 << 6
	rintDataCRC   = 1 << 7
	rintRespTmout = 1 << 8
	rintDataTmout = 1 << 9
	rintFIFORun   = 1 << 11
	rintHWLocked  = 1 << 12
	rintStartBit  = 1 << 13
	rintAutoDone  = 1 << 14
	rintEndBit    = 1 << 15

	rintErrMask = rintRespErr | rintRespCRC | rintDataCRC | rintRespTmout |
		rintDataTmout | rintFIFORun | rintHWLocked | rintStartBit | rintEndBit

	statusFIFOEmpty = 1 << 2
	statusFIFOFull  = 1 << 3
	statusCardBusy  = 1 << 9

	ntsrNewMode = 1 << 31
	calDLSWEn   = 1 << 7
)

// MMC is an SMHC controller instance driving one SD memory card.
type MMC struct {
	Base uint32

	rca     uint32
	highCap bool // SDHC/SDXC: block (not byte) addressing
	inited  bool

	// identification, for Info()
	cid    [4]uint32
	Blocks uint32 // capacity in 512-byte blocks
}

// SD0 is the CB1 micro-SD slot (SMHC0).
var SD0 = &MMC{Base: SMHC0_BASE}

func (m *MMC) rd(off uint32) uint32    { return reg.Read(m.Base + off) }
func (m *MMC) wr(off uint32, v uint32) { reg.Write(m.Base+off, v) }

// updateClk runs the controller's clock-update command after any
// CLKCR/mod-clock change.
func (m *MMC) updateClk() error {
	m.wr(smhcCMD, cmdStart|cmdUpClkOnly|cmdWaitPreOver)
	deadline := time.Now().Add(100 * time.Millisecond)
	for m.rd(smhcCMD)&cmdStart != 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("clock update stuck")
		}
	}
	m.wr(smhcRINT, m.rd(smhcRINT))
	return nil
}

// setClock programs the card clock from OSC24M: 24 MHz / 2^n / m.
func (m *MMC) setClock(n, mdiv uint32) error {
	// disable card clock
	m.wr(smhcCLKCR, m.rd(smhcCLKCR)&^uint32(clkEnable))
	if err := m.updateClk(); err != nil {
		return err
	}

	// mod clock: OSC24M source (mux 0), requested dividers
	reg.Write(smhc0ClkReg, 1<<smhcClkEnable|n<<8|(mdiv-1))

	// new timing mode, internal divider cleared, calibration delay
	// software-enable with zero delay (U-Boot mmc_config_clock)
	m.wr(smhcNTSR, m.rd(smhcNTSR)|ntsrNewMode)
	m.wr(smhcCLKCR, m.rd(smhcCLKCR)&^uint32(0xff))
	m.wr(smhcSAMPDL, calDLSWEn)

	m.wr(smhcCLKCR, m.rd(smhcCLKCR)|clkEnable)
	return m.updateClk()
}

// cmd issues one command, optionally moving data by PIO, and returns
// the 32-bit R1/R3/R6 response (resp0).
func (m *MMC) cmd(idx uint32, arg uint32, flags uint32, data []byte) (uint32, error) {
	m.wr(smhcRINT, 0xffffffff)
	m.wr(smhcARG, arg)

	write := flags&cmdWrite != 0
	if data != nil {
		m.wr(smhcGCTRL, m.rd(smhcGCTRL)|gctrlFIFOReset|gctrlAHBAccess)
		for m.rd(smhcGCTRL)&gctrlFIFOReset != 0 {
		}
		m.wr(smhcBLKSZ, 512)
		m.wr(smhcBYTECNT, uint32(len(data)))
		flags |= cmdDataExpire | cmdWaitPreOver
		if len(data) > 512 {
			flags |= cmdAutoStop
		}
	}

	m.wr(smhcCMD, cmdStart|idx|flags)

	// PIO data phase
	deadline := time.Now().Add(2 * time.Second)
	for off := 0; data != nil && off < len(data); {
		st := m.rd(smhcSTATUS)
		switch {
		case write && st&statusFIFOFull == 0:
			var w uint32
			for i := 0; i < 4; i++ {
				w |= uint32(data[off+i]) << (8 * i)
			}
			m.wr(smhcFIFO, w)
			off += 4
		case !write && st&statusFIFOEmpty == 0:
			w := m.rd(smhcFIFO)
			for i := 0; i < 4; i++ {
				data[off+i] = byte(w >> (8 * i))
			}
			off += 4
		default:
			if rint := m.rd(smhcRINT); rint&rintErrMask != 0 {
				return 0, fmt.Errorf("CMD%d data error (RINT %#x)", idx, rint)
			}
			if time.Now().After(deadline) {
				return 0, fmt.Errorf("CMD%d data timeout at %d/%d", idx, off, len(data))
			}
		}
	}

	// command completion
	if err := m.waitRINT(idx, rintCmdDone, time.Second); err != nil {
		return 0, err
	}

	// data completion: DATA_OVER, or the auto-stop's own completion
	if data != nil {
		done := uint32(rintDataOver)
		if len(data) > 512 {
			done = rintAutoDone
		}
		if err := m.waitRINT(idx, done, 2*time.Second); err != nil {
			return 0, err
		}
	}

	// R1b / post-write busy
	deadline = time.Now().Add(2 * time.Second)
	for m.rd(smhcSTATUS)&statusCardBusy != 0 {
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("CMD%d: card stuck busy", idx)
		}
	}

	return m.rd(smhcRESP0), nil
}

func (m *MMC) waitRINT(idx, bit uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		rint := m.rd(smhcRINT)
		if rint&rintErrMask != 0 {
			return fmt.Errorf("CMD%d error (RINT %#x)", idx, rint)
		}
		if rint&bit != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("CMD%d timeout (RINT %#x)", idx, rint)
		}
	}
}

// resp128 returns the long (R2) response as four words, MSW first.
func (m *MMC) resp128() [4]uint32 {
	return [4]uint32{
		m.rd(smhcRESP0 + 12), m.rd(smhcRESP0 + 8),
		m.rd(smhcRESP0 + 4), m.rd(smhcRESP0),
	}
}

// unstuff extracts a bit field from a 128-bit response (Linux
// UNSTUFF_BITS: bit 0 = LSB of the last word, r[0] = MSW).
func unstuff(r [4]uint32, start, size uint) uint32 {
	i := 3 - start/32
	v := uint64(r[i])
	if i > 0 {
		v |= uint64(r[i-1]) << 32
	}
	return uint32(v>>(start%32)) & uint32(1<<size-1)
}

// Init brings up the controller and identifies the card.
func (m *MMC) Init() error {
	// bus clock + reset
	reg.Set(smhc0BGR, smhc0Gate)
	reg.Set(smhc0BGR, smhc0Rst)

	// controller reset
	m.wr(smhcGCTRL, gctrlSoftReset|gctrlFIFOReset|gctrlDMAReset)
	deadline := time.Now().Add(100 * time.Millisecond)
	for m.rd(smhcGCTRL)&(gctrlSoftReset|gctrlFIFOReset|gctrlDMAReset) != 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("SMHC reset stuck")
		}
	}

	m.wr(smhcIMASK, 0)
	m.wr(smhcTIMEOUT, 0xffffffff)
	m.wr(smhcWIDTH, 0) // 1-bit during identification

	// identification clock: 24 MHz / 4 / 15 = 400 kHz
	if err := m.setClock(2, 15); err != nil {
		return err
	}

	// CMD0 GO_IDLE (with the 74-clock init sequence)
	if _, err := m.cmd(0, 0, cmdSendInitSeq, nil); err != nil {
		return fmt.Errorf("CMD0: %v", err)
	}

	// CMD8 SEND_IF_COND: 2.7-3.6V, pattern 0xAA (absent on old cards)
	sdV2 := false
	if r, err := m.cmd(8, 0x1aa, cmdRespExpire|cmdChkRespCRC, nil); err == nil && r&0xff == 0xaa {
		sdV2 = true
	}

	// ACMD41 until powered up; HCS only for V2 cards
	ocrArg := uint32(0x00ff8000)
	if sdV2 {
		ocrArg |= 1 << 30
	}
	deadline = time.Now().Add(time.Second)
	for {
		if _, err := m.cmd(55, 0, cmdRespExpire|cmdChkRespCRC, nil); err != nil {
			return fmt.Errorf("CMD55: %v", err)
		}
		// R3: no CRC on the response
		ocr, err := m.cmd(41, ocrArg, cmdRespExpire, nil)
		if err != nil {
			return fmt.Errorf("ACMD41: %v", err)
		}
		if ocr&(1<<31) != 0 {
			m.highCap = ocr&(1<<30) != 0
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ACMD41: card did not power up")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// CMD2 ALL_SEND_CID, CMD3 SEND_RELATIVE_ADDR
	if _, err := m.cmd(2, 0, cmdRespExpire|cmdLongResp|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("CMD2: %v", err)
	}
	m.cid = m.resp128()
	r, err := m.cmd(3, 0, cmdRespExpire|cmdChkRespCRC, nil)
	if err != nil {
		return fmt.Errorf("CMD3: %v", err)
	}
	m.rca = r >> 16

	// CMD9 SEND_CSD (deselected): capacity
	if _, err := m.cmd(9, m.rca<<16, cmdRespExpire|cmdLongResp|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("CMD9: %v", err)
	}
	csd := m.resp128()
	if unstuff(csd, 126, 2) == 1 { // CSD v2 (SDHC/SDXC)
		cSize := unstuff(csd, 48, 22)
		m.Blocks = (cSize + 1) << 10 // (C_SIZE+1) * 512 KiB / 512
	} else { // CSD v1
		cSize := unstuff(csd, 62, 12)
		mult := unstuff(csd, 47, 3)
		blLen := unstuff(csd, 80, 4)
		m.Blocks = uint32((uint64(cSize+1) << (mult + 2 + blLen)) / 512)
	}

	// CMD7 select, ACMD6 4-bit bus, full clock (24 MHz / 1 / 1)
	if _, err := m.cmd(7, m.rca<<16, cmdRespExpire|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("CMD7: %v", err)
	}
	if _, err := m.cmd(55, m.rca<<16, cmdRespExpire|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("CMD55: %v", err)
	}
	if _, err := m.cmd(6, 2, cmdRespExpire|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("ACMD6: %v", err)
	}
	m.wr(smhcWIDTH, 1) // 4-bit

	if err := m.setClock(0, 1); err != nil {
		return err
	}

	// CMD16 SET_BLOCKLEN (no-op on high capacity but harmless)
	if _, err := m.cmd(16, 512, cmdRespExpire|cmdChkRespCRC, nil); err != nil {
		return fmt.Errorf("CMD16: %v", err)
	}

	m.inited = true
	return nil
}

// Ready reports whether Init has completed.
func (m *MMC) Ready() bool { return m.inited }

// CID returns the raw card identification register.
func (m *MMC) CID() [4]uint32 { return m.cid }

// addr converts an LBA to the card's addressing (block vs byte).
func (m *MMC) addr(lba uint32) uint32 {
	if m.highCap {
		return lba
	}
	return lba * 512
}

// ReadBlocks reads len(buf)/512 blocks starting at lba by PIO.
func (m *MMC) ReadBlocks(lba uint32, buf []byte) error {
	if !m.inited {
		return fmt.Errorf("card not initialized")
	}
	if len(buf) == 0 || len(buf)%512 != 0 {
		return fmt.Errorf("buffer must be a multiple of 512")
	}
	idx := uint32(17) // READ_SINGLE_BLOCK
	if len(buf) > 512 {
		idx = 18 // READ_MULTIPLE_BLOCK (auto-stop)
	}
	_, err := m.cmd(idx, m.addr(lba), cmdRespExpire|cmdChkRespCRC, buf)
	return err
}

// WriteBlocks writes len(buf)/512 blocks starting at lba by PIO.
func (m *MMC) WriteBlocks(lba uint32, buf []byte) error {
	if !m.inited {
		return fmt.Errorf("card not initialized")
	}
	if len(buf) == 0 || len(buf)%512 != 0 {
		return fmt.Errorf("buffer must be a multiple of 512")
	}
	idx := uint32(24) // WRITE_BLOCK
	if len(buf) > 512 {
		idx = 25 // WRITE_MULTIPLE_BLOCK (auto-stop)
	}
	_, err := m.cmd(idx, m.addr(lba), cmdRespExpire|cmdChkRespCRC|cmdWrite, buf)
	return err
}
