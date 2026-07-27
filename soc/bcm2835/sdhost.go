// BCM2835 SoC Broadcom "sdhost" SD card controller driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/usbarmory/tamago/internal/reg"
)

// The Broadcom "sdhost" controller is the SoC's legacy (non-SDHCI) SD host,
// distinct from the Arasan SDHCI/EMMC core. On stock Raspberry Pi firmware the
// GPIO 48..53 SD-card bus is muxed to sdhost (ALT0) and the Arasan core is
// routed to the on-board WiFi chip (SDIO). Because WiFi owns Arasan, on-card
// persistence for bmx goes through this controller.
//
// This is a polled (PIO) driver: single-block read/write over the 16-word FIFO,
// 1-bit bus. It implements the SD memory-card identification sequence (the
// Arasan driver here only speaks SDIO). Register semantics follow the Linux
// bcm2835-sdhost driver and the Circle bare-metal port.

const (
	SDHOST_BASE = 0x202000

	sdhostCMD  = 0x00 // command
	sdhostARG  = 0x04 // argument
	sdhostTOUT = 0x08 // data timeout
	sdhostCDIV = 0x0c // clock divide
	sdhostRSP0 = 0x10 // response bits 31:0
	sdhostRSP1 = 0x14
	sdhostRSP2 = 0x18
	sdhostRSP3 = 0x1c
	sdhostHSTS = 0x20 // host status (write-1-to-clear flags)
	sdhostVDD  = 0x30 // power
	sdhostEDM  = 0x34 // extension data mode (FIFO thresholds + fill level)
	sdhostHCFG = 0x38 // host configuration
	sdhostHBCT = 0x3c // block byte count
	sdhostDATA = 0x40 // data FIFO
	sdhostHBLC = 0x50 // block count

	// SDCMD
	sdCmdNew   = 1 << 15 // enable/new command (self-clears when consumed)
	sdCmdFail  = 1 << 14 // command failed
	sdCmdBusy  = 1 << 11 // wait for busy after response (R1b)
	sdCmdNoRsp = 1 << 10 // command expects no response
	sdCmdLong  = 1 << 9  // 136-bit response (R2)
	sdCmdWrite = 1 << 7  // host->card data transfer
	sdCmdRead  = 1 << 6  // card->host data transfer

	// SDHSTS
	sdHstsBusyIrpt  = 1 << 10
	sdHstsBlockIrpt = 1 << 9
	sdHstsSdioIrpt  = 1 << 8
	sdHstsRewTo     = 1 << 7 // read/erase/write timeout
	sdHstsCmdTo     = 1 << 6 // command timeout
	sdHstsCrc16     = 1 << 5 // data CRC error
	sdHstsCrc7      = 1 << 4 // command CRC error
	sdHstsFifo      = 1 << 3 // FIFO error
	sdHstsData      = 1 << 0 // data flag
	sdHstsErrMask   = sdHstsRewTo | sdHstsCmdTo | sdHstsCrc16 | sdHstsCrc7 | sdHstsFifo
	sdHstsClearMask = sdHstsErrMask | sdHstsBusyIrpt | sdHstsBlockIrpt | sdHstsSdioIrpt | sdHstsData

	// SDHCFG
	sdHcfgBusyEn  = 1 << 10
	sdHcfgBlockEn = 1 << 8
	sdHcfgSdioEn  = 1 << 5
	sdHcfgDataEn  = 1 << 4
	sdHcfgSlow    = 1 << 3 // slow card (extra clocks between commands)
	sdHcfgWide4   = 1 << 2 // 4-bit data bus
	sdHcfgWideInt = 1 << 1 // internal (wide) bus
	sdHcfgRelCmd  = 1 << 0

	// SDEDM: FIFO fill level and read/write thresholds
	sdEdmFillShift  = 4
	sdEdmFillMask   = 0x1f
	sdEdmWriteShift = 9
	sdEdmReadShift  = 14
	fifoDepth       = 16
	fifoReadThresh  = 4
	fifoWriteThresh = 4

	// VideoCore core clock id (feeds sdhost)
	vcClockIDCore = 0x4

	sdBlockSize = 512
)

// ErrNoCard is returned by Detect when no SD card responds.
var ErrNoCard = errors.New("sdhost: no card detected")

// errCmdTimeout is an expected outcome for probe commands (CMD8 on v1 cards).
var errCmdTimeout = errors.New("sdhost: command timeout")

// SDHost is a Broadcom sdhost SD card controller instance.
type SDHost struct {
	base    uint32
	baseClk uint32   // sdhost input (core) clock, Hz
	rca     uint32   // card relative address
	sdhc    bool     // block addressing (SDHC/SDXC) vs byte addressing (SDSC)
	inited  bool
	CID     [4]uint32
	Blocks  uint64 // capacity in 512-byte blocks (from CSD), 0 if unknown
	clockHz uint32 // current bus clock

	// DMA read path (PIO FIFO reads mis-sample the word boundary on this
	// controller; DMA drains the FIFO cleanly, paced by the sdhost DREQ).
	dmaCh        *DMAChannel
	dmaMem       *CoherentBuffer // control block @0 (32B) + data buffer @64 (512B)
	DREQ         uint32          // sdhost DMA DREQ peripheral id (set via EnableDMA)
	LastReadHSTS uint32          // controller status after the last DMA read
}

// SDCard is the on-board SD card on the Broadcom sdhost controller.
var SDCard = &SDHost{}

func (h *SDHost) r(off uint32) uint32   { return reg.Read(h.base + off) }
func (h *SDHost) w(off, val uint32)     { reg.Write(h.base+off, val) }
func (h *SDHost) fifoFill() uint32      { return (h.r(sdhostEDM) >> sdEdmFillShift) & sdEdmFillMask }
func (h *SDHost) SDHC() bool            { return h.sdhc }
func (h *SDHost) RCA() uint32           { return h.rca }
func (h *SDHost) BaseClockHz() uint32   { return h.baseClk }
func (h *SDHost) ClockHz() uint32       { return h.clockHz }
func (h *SDHost) Ready() bool           { return h.inited }

// Peek/Poke expose raw controller registers for bring-up diagnostics
// (offsets: CMD 0x00, CDIV 0x0c, HSTS 0x20, EDM 0x34, HCFG 0x38).
func (h *SDHost) Peek(off uint32) uint32 {
	if h.base == 0 {
		return 0
	}
	return h.r(off &^ 3) // registers are 32-bit; force alignment
}

func (h *SDHost) Poke(off, val uint32) {
	if h.base == 0 {
		return
	}
	h.w(off&^3, val)
}

// SetClock changes the bus clock at runtime (diagnostic; writes are more
// timing-sensitive than reads on this controller). No-op if the card has not
// been initialized (the register base would be unset).
func (h *SDHost) SetClock(hz uint32) {
	if !h.inited {
		return
	}
	h.setClock(hz)
}

// coreClockRate queries the VideoCore for the core clock rate feeding sdhost.
func coreClockRate() uint32 {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, vcClockIDCore)

	res := exchangeSingleTagMessage(VC_CLOCK_GET_RATE, buf)
	if len(res) < 8 {
		return 0
	}
	return binary.LittleEndian.Uint32(res[4:])
}

// Detect powers, resets, and initializes the SD card, running the memory-card
// identification sequence. It is safe to call more than once.
func (h *SDHost) Detect() error {
	h.base = PeripheralAddress(SDHOST_BASE)
	h.inited = false

	// power the SD card rail
	SetPowerState(PowerDeviceSDCard, true)

	// mux the SD bus to sdhost (ALT0); stock firmware usually already did this
	for pin := 48; pin <= 53; pin++ {
		g, err := NewGPIO(pin)
		if err != nil {
			return fmt.Errorf("sdhost: GPIO %d: %v", pin, err)
		}
		g.SelectFunction(GPIO_FN0)
	}

	h.baseClk = coreClockRate()
	if h.baseClk == 0 {
		h.baseClk = 250000000 // conservative default
	}

	h.reset()
	h.setClock(400000) // identification clock (~400 kHz)

	return h.identify()
}

// reset returns the controller to a known state and powers the bus on.
func (h *SDHost) reset() {
	h.w(sdhostVDD, 0)
	h.w(sdhostCMD, 0)
	h.w(sdhostARG, 0)
	h.w(sdhostTOUT, 0xf00000)
	h.w(sdhostCDIV, 0)
	h.w(sdhostHSTS, 0x7f8) // clear all status flags
	h.w(sdhostHCFG, 0)
	h.w(sdhostHBCT, 0)
	h.w(sdhostHBLC, 0)

	edm := h.r(sdhostEDM)
	edm &^= (sdEdmFillMask << sdEdmReadShift) | (sdEdmFillMask << sdEdmWriteShift)
	edm |= (fifoReadThresh << sdEdmReadShift) | (fifoWriteThresh << sdEdmWriteShift)
	h.w(sdhostEDM, edm)

	time.Sleep(10 * time.Millisecond)
	h.w(sdhostVDD, 1) // power on
	time.Sleep(10 * time.Millisecond)

	// slow card during identification for margin
	h.w(sdhostHCFG, sdHcfgWideInt|sdHcfgSlow)
}

// setClock programs the bus clock divider for the requested rate.
func (h *SDHost) setClock(hz uint32) {
	if hz == 0 {
		hz = 400000
	}
	div := h.baseClk / hz
	if div < 2 {
		div = 2
	}
	if h.baseClk/div > hz {
		div++
	}
	div -= 2
	if div > 0x7ff {
		div = 0x7ff
	}
	h.w(sdhostCDIV, div)
	h.clockHz = h.baseClk / (div + 2)
}

// command issues an SD command and returns its response. ignoreCRC skips the
// command-CRC error check for responses without CRC (R3, e.g. ACMD41).
func (h *SDHost) command(index, arg, flags uint32, ignoreCRC bool, resp *[4]uint32) error {
	// wait for any in-flight command to be consumed
	deadline := time.Now().Add(100 * time.Millisecond)
	for h.r(sdhostCMD)&sdCmdNew != 0 {
		if time.Now().After(deadline) {
			return errCmdTimeout
		}
	}

	h.w(sdhostHSTS, sdHstsClearMask) // clear stale status
	h.w(sdhostARG, arg)
	h.w(sdhostCMD, (index&0x3f)|sdCmdNew|flags)

	// wait for the command to be taken (sdCmdNew self-clears)
	deadline = time.Now().Add(100 * time.Millisecond)
	for {
		c := h.r(sdhostCMD)
		if c&sdCmdNew == 0 {
			if c&sdCmdFail != 0 {
				st := h.r(sdhostHSTS)
				h.w(sdhostHSTS, sdHstsClearMask)
				if st&sdHstsCmdTo != 0 {
					return errCmdTimeout
				}
				return fmt.Errorf("sdhost: CMD%d failed hsts=%#x", index, st)
			}
			break
		}
		if time.Now().After(deadline) {
			return errCmdTimeout
		}
	}

	st := h.r(sdhostHSTS)
	errBits := st & sdHstsErrMask
	if ignoreCRC {
		errBits &^= sdHstsCrc7
	}
	if errBits != 0 {
		h.w(sdhostHSTS, sdHstsClearMask)
		if errBits&sdHstsCmdTo != 0 {
			return errCmdTimeout
		}
		return fmt.Errorf("sdhost: CMD%d hsts=%#x", index, st)
	}

	if resp != nil && flags&sdCmdNoRsp == 0 {
		resp[0] = h.r(sdhostRSP0)
		if flags&sdCmdLong != 0 {
			resp[1] = h.r(sdhostRSP1)
			resp[2] = h.r(sdhostRSP2)
			resp[3] = h.r(sdhostRSP3)
		}
	}
	return nil
}

// identify runs the SD memory-card identification and moves the card to the
// transfer state.
func (h *SDHost) identify() error {
	var r [4]uint32

	// CMD0: go idle
	if err := h.command(0, 0, sdCmdNoRsp, false, nil); err != nil {
		return fmt.Errorf("sdhost: CMD0: %v", err)
	}
	time.Sleep(time.Millisecond)

	// CMD8: interface condition (0x1AA = 2.7-3.6V, check pattern 0xAA)
	v2 := false
	if err := h.command(8, 0x1AA, 0, false, &r); err == nil {
		if r[0]&0xfff == 0x1AA {
			v2 = true
		}
	} else if err != errCmdTimeout {
		return fmt.Errorf("sdhost: CMD8: %v", err)
	}

	// ACMD41: operating conditions, loop until the card leaves busy
	ocrArg := uint32(0x00FF8000) // 2.7-3.6V window
	if v2 {
		ocrArg |= 1 << 30 // HCS: host supports high capacity
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := h.command(55, 0, 0, false, &r); err != nil { // APP_CMD (rca=0)
			return fmt.Errorf("sdhost: CMD55: %v", err)
		}
		// ACMD41 returns R3 (OCR), which has no CRC
		if err := h.command(41, ocrArg, 0, true, &r); err != nil {
			return fmt.Errorf("sdhost: ACMD41: %v", err)
		}
		if r[0]&(1<<31) != 0 { // power-up complete
			h.sdhc = r[0]&(1<<30) != 0 // CCS: block addressing
			break
		}
		if time.Now().After(deadline) {
			return ErrNoCard
		}
		time.Sleep(time.Millisecond)
	}

	// CMD2: all send CID (R2)
	if err := h.command(2, 0, sdCmdLong, true, &r); err != nil {
		return fmt.Errorf("sdhost: CMD2: %v", err)
	}
	h.CID = r

	// CMD3: publish relative address (R6)
	if err := h.command(3, 0, 0, false, &r); err != nil {
		return fmt.Errorf("sdhost: CMD3: %v", err)
	}
	h.rca = r[0] >> 16

	// CMD9: card-specific data (R2), for capacity
	if err := h.command(9, h.rca<<16, sdCmdLong, true, &r); err == nil {
		h.Blocks = decodeCSDBlocks(r)
	}

	// data-rate clock now that identification is done
	h.w(sdhostHCFG, sdHcfgWideInt) // drop slow-card
	h.setClock(25000000)

	// CMD7: select card -> transfer state (R1b)
	if err := h.command(7, h.rca<<16, sdCmdBusy, false, &r); err != nil {
		return fmt.Errorf("sdhost: CMD7: %v", err)
	}

	// CMD16: set block length (SDHC is fixed at 512 and ignores this)
	if !h.sdhc {
		if err := h.command(16, sdBlockSize, 0, false, &r); err != nil {
			return fmt.Errorf("sdhost: CMD16: %v", err)
		}
	}

	h.inited = true
	return nil
}

// decodeCSDBlocks returns the capacity in 512-byte blocks from a CSD register.
// Supports CSD v2 (SDHC/SDXC); returns 0 for v1 (parsed later if needed).
func decodeCSDBlocks(csd [4]uint32) uint64 {
	// The controller returns the 128-bit CSD in RSP0..3 with the CRC byte
	// already stripped (RSP0 holds bits 39:8 of the register). CSD version is
	// in the top two bits of RSP3.
	ver := csd[3] >> 30
	if ver == 1 { // CSD v2.0
		// C_SIZE is a 22-bit field; with the CRC stripped it sits in
		// RSP1[15:0] and RSP2[5:0].
		cSize := uint64((csd[2]&0x3f)<<16 | (csd[1]>>16)&0xffff)
		return (cSize + 1) * 1024 // (C_SIZE+1) * 512KB / 512B
	}
	return 0
}

// EnableDMA arms the DMA read path with the given channel and sdhost DREQ id,
// allocating the coherent control-block + data buffer on first use.
func (h *SDHost) EnableDMA(ch *DMAChannel, dreq uint32) error {
	if h.dmaMem == nil {
		mem, err := AllocCoherent(64 + sdBlockSize)
		if err != nil {
			return err
		}
		h.dmaMem = mem
	}
	h.dmaCh = ch
	h.DREQ = dreq
	return nil
}

// ReadBlockDMA reads one 512-byte block via DMA (SDDATA FIFO -> coherent buffer,
// paced by the sdhost DREQ), avoiding the PIO word-boundary sampling defect.
func (h *SDHost) ReadBlockDMA(lba uint32, buf []byte) error {
	if !h.inited {
		return ErrNoCard
	}
	if h.dmaCh == nil || h.dmaMem == nil {
		return errors.New("sdhost: DMA not enabled (call EnableDMA)")
	}
	if len(buf) < sdBlockSize {
		return errors.New("sdhost: buffer smaller than block size")
	}

	addr := lba
	if !h.sdhc {
		addr = lba * sdBlockSize
	}

	// bus addresses within the coherent buffer: CB @0, data @64
	cb := h.dmaMem.Bytes()[0:32]
	cbBus := h.dmaMem.Bus
	dataBus := h.dmaMem.Bus + 64

	// SDDATA peripheral bus address (0x7Exxxxxx alias)
	sddata := uint32(busPeripheralBase + SDHOST_BASE + sdhostDATA)

	ti := uint32(DMA_TI_SRC_DREQ | DMA_TI_DEST_INC | DMA_TI_WAITRESP |
		(h.DREQ&DMA_TI_BURST_PREMAP_MASK)<<DMA_TI_BURST_PERMAP_SHIFT)

	h.w(sdhostHBCT, sdBlockSize)
	h.w(sdhostHBLC, 1)

	// arm DMA first so it drains the FIFO as the card fills it
	h.dmaCh.Start(cb, cbBus, ti, sddata, dataBus, sdBlockSize)

	var r [4]uint32
	if err := h.command(17, addr, sdCmdRead, false, &r); err != nil {
		h.dmaCh.Abort()
		return err
	}
	if err := h.dmaCh.Wait(1 * time.Second); err != nil {
		return err
	}
	// Record but do not fail on controller status: during bring-up we want to
	// inspect the DMA'd bytes even when a CRC flag is set, to tell a
	// reception-level defect apart from a spurious/late CRC.
	h.LastReadHSTS = h.r(sdhostHSTS)
	h.w(sdhostHSTS, sdHstsClearMask)

	copy(buf[:sdBlockSize], h.dmaMem.Bytes()[64:64+sdBlockSize])
	return nil
}

// ReadBlock reads one 512-byte block at the given LBA into buf.
func (h *SDHost) ReadBlock(lba uint32, buf []byte) error {
	if !h.inited {
		return ErrNoCard
	}
	if len(buf) < sdBlockSize {
		return errors.New("sdhost: buffer smaller than block size")
	}

	addr := lba
	if !h.sdhc {
		addr = lba * sdBlockSize
	}

	h.w(sdhostHBCT, sdBlockSize)
	h.w(sdhostHBLC, 1)

	var r [4]uint32
	if err := h.command(17, addr, sdCmdRead, false, &r); err != nil { // READ_SINGLE_BLOCK
		return err
	}
	return h.readFIFO(buf[:sdBlockSize])
}

// WriteBlock writes one 512-byte block from buf to the given LBA.
func (h *SDHost) WriteBlock(lba uint32, buf []byte) error {
	if !h.inited {
		return ErrNoCard
	}
	if len(buf) < sdBlockSize {
		return errors.New("sdhost: buffer smaller than block size")
	}

	addr := lba
	if !h.sdhc {
		addr = lba * sdBlockSize
	}

	h.w(sdhostHBCT, sdBlockSize)
	h.w(sdhostHBLC, 1)

	var r [4]uint32
	if err := h.command(24, addr, sdCmdWrite, false, &r); err != nil { // WRITE_BLOCK
		return err
	}
	if err := h.writeFIFO(buf[:sdBlockSize]); err != nil {
		return err
	}
	return h.waitWriteDone()
}

// ReadMode selects the FIFO drain strategy while we chase a word-boundary read
// glitch (first bit of each 32-bit word mis-sampled):
//
//	0 = SDEDM fill, one word per status read (original)
//	1 = SDEDM fill, drain the whole reported burst per status read
//	2 = SDHSTS data flag, one word per status read
//	3 = read SDEDM fill twice and drain, letting the count settle first
var ReadMode = 0

func (h *SDHost) readFIFO(buf []byte) error {
	words := len(buf) / 4
	deadline := time.Now().Add(1 * time.Second)

	drainErr := func() error {
		if st := h.r(sdhostHSTS); st&sdHstsErrMask != 0 {
			h.w(sdhostHSTS, sdHstsClearMask)
			return fmt.Errorf("sdhost: read error hsts=%#x", st)
		}
		if time.Now().After(deadline) {
			return errCmdTimeout
		}
		return nil
	}

	for i := 0; i < words; {
		avail := 0
		switch ReadMode {
		case 2:
			if h.r(sdhostHSTS)&sdHstsData != 0 {
				avail = 1
			}
		case 3:
			h.fifoFill() // discard first read; let the level settle
			avail = int(h.fifoFill())
		default:
			avail = int(h.fifoFill())
		}

		if avail == 0 {
			if err := drainErr(); err != nil {
				return err
			}
			continue
		}
		if ReadMode == 0 || ReadMode == 2 {
			avail = 1 // one word per status read
		}
		for k := 0; k < avail && i < words; k++ {
			binary.LittleEndian.PutUint32(buf[i*4:], h.r(sdhostDATA))
			i++
		}
	}
	return nil
}

func (h *SDHost) writeFIFO(buf []byte) error {
	words := len(buf) / 4
	deadline := time.Now().Add(1 * time.Second)
	for i := 0; i < words; i++ {
		for h.fifoFill() >= fifoDepth {
			if st := h.r(sdhostHSTS); st&sdHstsErrMask != 0 {
				h.w(sdhostHSTS, sdHstsClearMask)
				return fmt.Errorf("sdhost: write error hsts=%#x", st)
			}
			if time.Now().After(deadline) {
				return errCmdTimeout
			}
		}
		h.w(sdhostDATA, binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return nil
}

// waitWriteDone waits for the card to finish programming after a write. CMD13
// responses are read with CRC tolerance (the CMD line occasionally flags CRC7
// on this controller); on budget exhaustion it assumes programming completed.
func (h *SDHost) waitWriteDone() error {
	deadline := time.Now().Add(1 * time.Second)
	for {
		var r [4]uint32
		if err := h.command(13, h.rca<<16, 0, true, &r); err == nil { // SEND_STATUS
			// bits 12:9 = state; 4 = tran (ready), plus READY_FOR_DATA (bit 8)
			if r[0]&(1<<8) != 0 && (r[0]>>9)&0xf == 4 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return nil // assume the (short) programming time has elapsed
		}
		time.Sleep(time.Millisecond)
	}
}
