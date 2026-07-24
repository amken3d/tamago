// Qualcomm GENI serial engine UART driver (polled, FIFO mode)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// GENI serial engine registers, offsets from the SE base
// (include/linux/soc/qcom/geni-se.h)
const (
	GENI_FW_REVISION_RO = 0x68

	SE_GENI_BYTE_GRAN       = 0x254
	SE_GENI_DMA_MODE_EN     = 0x258
	SE_UART_TX_TRANS_CFG    = 0x25c
	SE_GENI_TX_PACKING_CFG0 = 0x260
	SE_GENI_TX_PACKING_CFG1 = 0x264
	SE_UART_TX_WORD_LEN     = 0x268
	SE_UART_TX_STOP_BIT_LEN = 0x26c
	SE_UART_TX_TRANS_LEN    = 0x270
	SE_UART_RX_TRANS_CFG    = 0x280
	SE_GENI_RX_PACKING_CFG0 = 0x284
	SE_GENI_RX_PACKING_CFG1 = 0x288
	SE_UART_RX_WORD_LEN     = 0x28c
	SE_UART_TX_PARITY_CFG   = 0x2a4
	SE_UART_RX_PARITY_CFG   = 0x2a8

	SE_GENI_M_CMD0         = 0x600
	SE_GENI_M_CMD_CTRL_REG = 0x604
	SE_GENI_M_IRQ_STATUS   = 0x610
	SE_GENI_M_IRQ_EN       = 0x614
	SE_GENI_M_IRQ_CLEAR    = 0x618
	SE_GENI_S_CMD0         = 0x630
	SE_GENI_S_CMD_CTRL_REG = 0x634
	SE_GENI_S_IRQ_STATUS   = 0x640
	SE_GENI_S_IRQ_EN       = 0x644
	SE_GENI_S_IRQ_CLEAR    = 0x648

	SE_GENI_TX_FIFO        = 0x700
	SE_GENI_RX_FIFO        = 0x780
	SE_GENI_TX_FIFO_STATUS = 0x800
	SE_GENI_RX_FIFO_STATUS = 0x804
	SE_GENI_TX_WATERMARK   = 0x80c
	SE_GSI_EVENT_EN        = 0xe18

	FW_REV_PROTOCOL_SHFT = 8
	FW_REV_PROTOCOL_MASK = 0xff
	GENI_SE_PROTO_UART   = 2

	M_OPCODE_SHFT    = 27
	UART_START_TX    = 0x1
	UART_START_READ  = 0x1
	M_CMD_DONE       = 1 << 0
	M_TX_FIFO_WM     = 1 << 30
	GENI_CMD_ABORT   = 1 << 1
	M_CMD_ABORT_DONE = 1 << 5
	RX_FIFO_WC_MASK  = 0x01ffffff
	UART_CTS_MASK    = 1 << 1
)

// packing vector for one 8-bit byte per 32-bit FIFO word, LSB first:
// (start 0, lsb, length 7, stop)
const packing1x8 = 0<<5 | 0<<4 | 7<<1 | 1

// GENIUART represents a GENI serial engine in UART mode. The baud rate
// (serial clock configuration) set by the boot chain is preserved: this
// driver never touches the clock tree.
type GENIUART struct {
	Base uint32

	// Proto is the serial engine protocol found at Init (2 = UART).
	Proto uint32
}

// txPollSpins bounds register polling by iteration count rather than by
// time: Tx backs the runtime console (Printk), which must work at any
// boot stage, including before timers or the scheduler exist. Each
// iteration includes a device register read (>= 100 ns), so the bound
// corresponds to tens of milliseconds.
const txPollSpins = 200000

func (u *GENIUART) txPoll(mask uint32) bool {
	for i := 0; i < txPollSpins; i++ {
		if reg.Read(u.Base+SE_GENI_M_IRQ_STATUS)&mask != 0 {
			return true
		}
	}

	return false
}

func (u *GENIUART) abortTx() {
	reg.Write(u.Base+SE_GENI_M_CMD_CTRL_REG, GENI_CMD_ABORT)
	u.txPoll(M_CMD_ABORT_DONE)
	reg.Write(u.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
}

// Init prepares the serial engine for polled FIFO-mode operation. The
// boot chain is expected to have configured the engine firmware (UART
// protocol) and the serial clock; word framing and FIFO packing are
// (re)programmed here.
func (u *GENIUART) Init() {
	u.Proto = (reg.Read(u.Base+GENI_FW_REVISION_RO) >> FW_REV_PROTOCOL_SHFT) & FW_REV_PROTOCOL_MASK

	// FIFO mode, no GSI events, no interrupt signaling (polled)
	reg.Write(u.Base+SE_GENI_DMA_MODE_EN, 0)
	reg.Write(u.Base+SE_GSI_EVENT_EN, 0)
	reg.Write(u.Base+SE_GENI_M_IRQ_EN, 0)
	reg.Write(u.Base+SE_GENI_S_IRQ_EN, 0)
	reg.Write(u.Base+SE_GENI_M_IRQ_CLEAR, 0xffffffff)
	reg.Write(u.Base+SE_GENI_S_IRQ_CLEAR, 0xffffffff)

	// 8N1, CTS ignored
	reg.Write(u.Base+SE_UART_TX_TRANS_CFG, UART_CTS_MASK)
	reg.Write(u.Base+SE_UART_TX_WORD_LEN, 8)
	reg.Write(u.Base+SE_UART_TX_STOP_BIT_LEN, 0)
	reg.Write(u.Base+SE_UART_TX_PARITY_CFG, 0)
	reg.Write(u.Base+SE_UART_RX_TRANS_CFG, 0)
	reg.Write(u.Base+SE_UART_RX_WORD_LEN, 8)
	reg.Write(u.Base+SE_UART_RX_PARITY_CFG, 0)

	// one byte per FIFO word, both directions
	reg.Write(u.Base+SE_GENI_BYTE_GRAN, 0)
	reg.Write(u.Base+SE_GENI_TX_PACKING_CFG0, packing1x8)
	reg.Write(u.Base+SE_GENI_TX_PACKING_CFG1, 0)
	reg.Write(u.Base+SE_GENI_RX_PACKING_CFG0, packing1x8)
	reg.Write(u.Base+SE_GENI_RX_PACKING_CFG1, 0)

	reg.Write(u.Base+SE_GENI_TX_WATERMARK, 1)

	// abort any secondary (RX) command left by the boot chain, then
	// start a continuous read (bounded register-read spin: Init runs
	// during early runtime setup, before the scheduler exists)
	reg.Write(u.Base+SE_GENI_S_CMD_CTRL_REG, GENI_CMD_ABORT)

	for i := 0; i < 1000; i++ {
		reg.Read(u.Base + SE_GENI_S_IRQ_STATUS)
	}

	reg.Write(u.Base+SE_GENI_S_IRQ_CLEAR, 0xffffffff)
	reg.Write(u.Base+SE_GENI_S_CMD0, UART_START_READ<<M_OPCODE_SHFT)
}

// Tx transmits a single character to the serial port.
func (u *GENIUART) Tx(c byte) {
	reg.Write(u.Base+SE_UART_TX_TRANS_LEN, 1)
	reg.Write(u.Base+SE_GENI_M_IRQ_CLEAR, M_CMD_DONE|M_TX_FIFO_WM)
	reg.Write(u.Base+SE_GENI_M_CMD0, UART_START_TX<<M_OPCODE_SHFT)

	if !u.txPoll(M_TX_FIFO_WM) {
		u.abortTx()
		return
	}

	reg.Write(u.Base+SE_GENI_TX_FIFO, uint32(c))
	reg.Write(u.Base+SE_GENI_M_IRQ_CLEAR, M_TX_FIFO_WM)

	if !u.txPoll(M_CMD_DONE) {
		u.abortTx()
		return
	}

	reg.Write(u.Base+SE_GENI_M_IRQ_CLEAR, M_CMD_DONE)
}

// Rx receives a single character from the serial port, returning false
// when the receive FIFO is empty.
func (u *GENIUART) Rx() (c byte, valid bool) {
	if reg.Read(u.Base+SE_GENI_RX_FIFO_STATUS)&RX_FIFO_WC_MASK == 0 {
		return 0, false
	}

	return byte(reg.Read(u.Base + SE_GENI_RX_FIFO)), true
}

// Read available data from the serial port into buf.
func (u *GENIUART) Read(buf []byte) (n int, _ error) {
	for n = 0; n < len(buf); n++ {
		c, valid := u.Rx()

		if !valid {
			break
		}

		buf[n] = c
	}

	return
}

// Write data from buf to the serial port.
func (u *GENIUART) Write(buf []byte) (n int, _ error) {
	for n = 0; n < len(buf); n++ {
		u.Tx(buf[n])
	}

	return
}
