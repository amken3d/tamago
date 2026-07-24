// Qualcomm GENI serial engine SPI master driver (polled, FIFO mode)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"errors"
	"fmt"

	"github.com/usbarmory/tamago/internal/reg"
)

// GENI serial engine SPI registers, offsets from the SE base
// (drivers/spi/spi-geni-qcom.c, include/linux/soc/qcom/geni-se.h)
const (
	SE_GENI_STATUS      = 0x040
	STATUS_M_CMD_ACTIVE = 1 << 0
	GENI_SER_M_CLK_CFG  = 0x048
	SER_CLK_EN          = 1 << 0
	CLK_DIV_SHFT        = 4

	SE_SPI_CPHA             = 0x224
	SE_SPI_LOOPBACK         = 0x22c
	LOOPBACK_ENABLE         = 0x1
	SE_SPI_CPOL             = 0x230
	SPI_CPOL                = 1 << 2
	SE_SPI_DEMUX_OUTPUT_INV = 0x24c
	SE_SPI_DEMUX_SEL        = 0x250
	SE_SPI_TRANS_CFG        = 0x25c
	CS_TOGGLE               = 1 << 0
	SE_SPI_WORD_LEN         = 0x268
	MIN_WORD_LEN            = 4
	SE_SPI_TX_TRANS_LEN     = 0x26c
	SE_SPI_RX_TRANS_LEN     = 0x270

	SPI_FULL_DUPLEX = 3

	M_RX_FIFO_WATERMARK = 1 << 26
	M_RX_FIFO_LAST      = 1 << 27

	GENI_SE_PROTO_SPI = 1
)

// GENISPI represents a GENI serial engine in SPI master mode.
type GENISPI struct {
	Base uint32

	// Proto is the serial engine protocol found at Init (1 = SPI).
	Proto uint32
}

// spiPollSpins bounds register polling by iteration count: a serial
// engine with a gated clock never completes commands, which must
// surface as an error rather than a hang.
const spiPollSpins = 2000000

func (s *GENISPI) poll(mask uint32) bool {
	for i := 0; i < spiPollSpins; i++ {
		if reg.Read(s.Base+SE_GENI_M_IRQ_STATUS)&mask != 0 {
			return true
		}
	}

	return false
}

func (s *GENISPI) abort() {
	reg.Write(s.Base+SE_GENI_M_CMD_CTRL_REG, GENI_CMD_ABORT)
	s.poll(M_CMD_ABORT_DONE)
	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
}

// Probe returns the serial engine protocol and clock configuration
// without touching engine state, for bring-up diagnostics.
func (s *GENISPI) Probe() (proto, clkCfg, status uint32) {
	proto = (reg.Read(s.Base+GENI_FW_REVISION_RO) >> FW_REV_PROTOCOL_SHFT) & FW_REV_PROTOCOL_MASK
	clkCfg = reg.Read(s.Base + GENI_SER_M_CLK_CFG)
	status = reg.Read(s.Base + SE_GENI_STATUS)

	return
}

// Init prepares the serial engine for polled FIFO-mode SPI master
// operation with the argument mode (0-3). The SE firmware must already
// hold the SPI protocol.
//
// The serial clock configuration set by the boot chain is preserved:
// GENI_SER_M_CLK_CFG (and everything else below SE offset 0x200, the
// GENI4_CFG region) is firmware/TrustZone-owned on this platform — a
// non-secure write resets the SoC into ramdump mode.
func (s *GENISPI) Init(mode int) error {
	s.Proto = (reg.Read(s.Base+GENI_FW_REVISION_RO) >> FW_REV_PROTOCOL_SHFT) & FW_REV_PROTOCOL_MASK

	if s.Proto != GENI_SE_PROTO_SPI {
		return fmt.Errorf("serial engine protocol %d, want %d (SPI)", s.Proto, GENI_SE_PROTO_SPI)
	}

	// FIFO mode, no GSI events, no interrupt signaling (polled)
	reg.Write(s.Base+SE_GENI_DMA_MODE_EN, 0)
	reg.Write(s.Base+SE_GSI_EVENT_EN, 0)
	reg.Write(s.Base+SE_GENI_M_IRQ_EN, 0)
	reg.Write(s.Base+SE_GENI_S_IRQ_EN, 0)
	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(s.Base+SE_GENI_S_IRQ_CLEAR, 0xffffffff)

	// one byte per FIFO word, both directions
	reg.Write(s.Base+SE_GENI_BYTE_GRAN, 0)
	reg.Write(s.Base+SE_GENI_TX_PACKING_CFG0, packing1x8)
	reg.Write(s.Base+SE_GENI_TX_PACKING_CFG1, 0)
	reg.Write(s.Base+SE_GENI_RX_PACKING_CFG0, packing1x8)
	reg.Write(s.Base+SE_GENI_RX_PACKING_CFG1, 0)

	// mode: CPHA bit 0, CPOL bit 2
	reg.Write(s.Base+SE_SPI_CPHA, uint32(mode)&0b1)
	reg.Write(s.Base+SE_SPI_CPOL, (uint32(mode)&0b10)<<1)

	// native CS0, no inversion, no per-word CS toggle
	reg.Write(s.Base+SE_SPI_DEMUX_SEL, 0)
	reg.Write(s.Base+SE_SPI_DEMUX_OUTPUT_INV, 0)
	reg.Write(s.Base+SE_SPI_TRANS_CFG, 0)

	// 8-bit words
	reg.Write(s.Base+SE_SPI_WORD_LEN, 8-MIN_WORD_LEN)

	reg.Write(s.Base+SE_GENI_TX_WATERMARK, 1)

	return nil
}

// SetLoopback controls the engine-internal MOSI-to-MISO loopback, for
// self-testing without pin multiplexing.
func (s *GENISPI) SetLoopback(on bool) {

	if on {
		reg.Write(s.Base+SE_SPI_LOOPBACK, LOOPBACK_ENABLE)
	} else {
		reg.Write(s.Base+SE_SPI_LOOPBACK, 0)
	}

}

// Transfer performs a full-duplex SPI transaction: tx is transmitted
// while len(tx) bytes are captured into rx (which must be at least as
// long). Transfers are bounded by the FIFO depth in polled single-fill
// operation (16 FIFO words at one byte per word).
func (s *GENISPI) Transfer(tx, rx []byte) error {
	n := len(tx)

	if n == 0 || n > 16 {
		return errors.New("transfer length must be 1-16 bytes")
	}

	if len(rx) < n {
		return errors.New("rx buffer too short")
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(s.Base+SE_SPI_TX_TRANS_LEN, uint32(n))
	reg.Write(s.Base+SE_SPI_RX_TRANS_LEN, uint32(n))

	reg.Write(s.Base+SE_GENI_M_CMD0, SPI_FULL_DUPLEX<<M_OPCODE_SHFT)

	if !s.poll(M_TX_FIFO_WM) {
		s.abort()
		return errors.New("timeout awaiting TX FIFO (SE clock gated?)")
	}

	for _, c := range tx[:n] {
		reg.Write(s.Base+SE_GENI_TX_FIFO, uint32(c))
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, M_TX_FIFO_WM)

	for i := 0; i < n; i++ {
		if !s.pollRx() {
			s.abort()
			return errors.New("timeout awaiting RX FIFO")
		}

		rx[i] = byte(reg.Read(s.Base + SE_GENI_RX_FIFO))
	}

	if !s.poll(M_CMD_DONE) {
		s.abort()
		return errors.New("timeout awaiting command completion")
	}

	reg.Write(s.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)

	return nil
}

func (s *GENISPI) pollRx() bool {
	for i := 0; i < spiPollSpins; i++ {
		if reg.Read(s.Base+SE_GENI_RX_FIFO_STATUS)&RX_FIFO_WC_MASK != 0 {
			return true
		}
	}

	return false
}
