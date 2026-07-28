// BCM2835 SoC VideoCore IV QPU (GPU compute) support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// The VideoCore IV has a set of QPUs (Quad Processor Units) -- 16-way SIMD
// floating-point cores usable for general compute (e.g. FFT). They are enabled
// via the mailbox property interface and driven either by the firmware
// "execute QPU" call or directly through the V3D register block.

package bcm2835

import (
	"encoding/binary"

	"github.com/usbarmory/tamago/internal/reg"
)

// Mailbox property tags for QPU control.
const (
	VC_QPU_ENABLE      = 0x00030012
	VC_QPU_ENABLE_LEN  = 4
	VC_EXECUTE_QPU     = 0x00030011
	VC_EXECUTE_QPU_LEN = 16
)

// V3D register block: peripheral base + 0xC00000. Only the read-only identity
// registers are needed to confirm the compute block is powered and reachable.
const (
	v3dOffset = 0xC00000

	V3D_IDENT0 = 0x000 // "V3D" magic + technology revision
	V3D_IDENT1 = 0x004 // slice / QPU / TMU / semaphore counts
	V3D_IDENT2 = 0x008 // VPM size, HDR support

	v3dMagic = 0x02443356 // 'V' '3' 'D', tech rev 2 (little-endian)
)

// V3DIdent reads the VideoCore IV V3D identity registers. It is only valid once
// the QPU clock is enabled (see QPUEnable). ok is true when the block returns
// its "V3D" magic, confirming the GPU compute core is powered and reachable.
type V3DIdent struct {
	Magic   uint32
	Rev     int
	Slices  int
	QPUs    int // total QPUs = slices * QPUs-per-slice
	TMUs    int // total TMUs
	Sems    int // hardware semaphores
	VPMSize int // VPM size in KiB
	OK      bool
}

// ReadV3DIdent decodes the V3D identity registers.
func ReadV3DIdent() (id V3DIdent) {
	base := peripheralBase + v3dOffset

	id.Magic = reg.Read(base + V3D_IDENT0)
	id.OK = (id.Magic & 0x00FFFFFF) == (v3dMagic & 0x00FFFFFF)
	id.Rev = int(id.Magic >> 24)

	if !id.OK {
		return
	}

	i1 := reg.Read(base + V3D_IDENT1)
	slices := int((i1 >> 4) & 0xF)
	qpusPerSlice := int((i1 >> 8) & 0xF)
	tmusPerSlice := int((i1 >> 12) & 0xF)

	id.Slices = slices
	id.QPUs = slices * qpusPerSlice
	id.TMUs = slices * tmusPerSlice
	id.Sems = int((i1 >> 16) & 0xFF)

	i2 := reg.Read(base + V3D_IDENT2)
	id.VPMSize = int((i2 >> 28) & 0xF) // in KiB (0 => 16 KiB on VC4)
	if id.VPMSize == 0 {
		id.VPMSize = 16
	}

	return
}

// QPUEnable turns the QPU/V3D clock on (or off) through the mailbox property
// interface. It must be called before touching the V3D registers or executing
// QPU code. Returns true on a successful (non-error) mailbox exchange.
func QPUEnable(on bool) bool {
	buf := make([]byte, VC_QPU_ENABLE_LEN)
	if on {
		binary.LittleEndian.PutUint32(buf[0:], 1)
	}

	msg := &MailboxMessage{
		Tags: []MailboxTag{{ID: VC_QPU_ENABLE, Buffer: buf}},
	}
	Mailbox.Call(VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	return !msg.Error()
}

// ExecuteQPU launches QPU programs via the firmware "execute QPU" call. control
// is the GPU bus address of an array of numQPUs (uniforms, code) bus-address
// pairs. The call blocks in firmware until the programs signal completion or
// timeoutMs elapses. noFlush skips the L2 cache flush between runs.
//
// This is the plumbing for offloading a compiled QPU kernel (e.g. an FFT
// shader); it is inert without a valid code blob in GPU memory.
func ExecuteQPU(numQPUs int, control uint32, noFlush bool, timeoutMs uint32) bool {
	buf := make([]byte, VC_EXECUTE_QPU_LEN)
	binary.LittleEndian.PutUint32(buf[0:], uint32(numQPUs))
	binary.LittleEndian.PutUint32(buf[4:], control)
	if noFlush {
		binary.LittleEndian.PutUint32(buf[8:], 1)
	}
	binary.LittleEndian.PutUint32(buf[12:], timeoutMs)

	msg := &MailboxMessage{
		Tags: []MailboxTag{{ID: VC_EXECUTE_QPU, Buffer: buf}},
	}
	Mailbox.Call(VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	if msg.Error() {
		return false
	}
	tag := msg.Tag(VC_EXECUTE_QPU)
	if tag == nil || len(tag.Buffer) < 4 {
		return false
	}
	// firmware returns 0 on success
	return binary.LittleEndian.Uint32(tag.Buffer) == 0
}
