// BCM2835 SoC Mailbox support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Mailboxes are used for inter-processor communication, in particular
// the VideoCore processor.

package bcm2835

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/usbarmory/tamago/dma"
	"github.com/usbarmory/tamago/internal/reg"
)

// We reserve the 'gap' above excStack and below TEXT segment start for
// mailbox usage.  There is nothing requiring use of this region, but it
// is convenient since it is always available.  Other regions could be
// used by adjusting ramSize to provide space at top of address range.
const (
	MAILBOX_REGION_BASE = 0xC000
	MAILBOX_REGION_SIZE = 0x4000
)

// Registers for using mailbox
const (
	MAILBOX_BASE       = 0xB880
	MAILBOX_READ_REG   = MAILBOX_BASE + 0x00
	MAILBOX_STATUS_REG = MAILBOX_BASE + 0x18
	MAILBOX_WRITE_REG  = MAILBOX_BASE + 0x20
	MAILBOX_FULL       = 0x80000000
	MAILBOX_EMPTY      = 0x40000000
)

type mailbox struct {
	sync.Mutex

	Region *dma.Region
}

// Mailbox provides access to the BCM2835 mailbox used to communicate with
// the VideoCore CPU
var Mailbox = mailbox{}

func init() {
	// We don't use this region for DMA, but dma package provides a convenient
	// block allocation system.
	//
	// The region lies within the runtime memory range (unused boot padding
	// below the TEXT segment), hence the unsafe flag; VideoCore coherency
	// is handled with explicit cache maintenance in Call. The ARM-side
	// uncached SDRAM alias (DRAM_FLAG_NOCACHE) exists only on the original
	// BCM2835 and must not be used for CPU access on BCM2836 and later.
	Mailbox.Region, _ = dma.NewRegion(MAILBOX_REGION_BASE, MAILBOX_REGION_SIZE, true)
}

type MailboxTag struct {
	ID     uint32
	Buffer []byte

	// Code is the tag's request/response word as the firmware left it. On the
	// way out it is zero; on the way back the firmware sets bit 31 and puts the
	// length it actually wrote in the low bits.
	//
	// It matters because a tag the firmware does not implement is left ALONE:
	// the message still reports overall success, and the tag's buffer still
	// holds whatever was in the shared region -- the request that was written
	// there, or bytes left by a previous mailbox transaction, since the region
	// is reserved and released per call. A caller that reads such a buffer
	// decodes stale memory as a reading. See Responded.
	Code uint32
}

// Responded reports whether the firmware actually wrote this tag's buffer.
//
// Always check it before trusting a "get" tag's contents. Zero would be a safe
// value to read from an unanswered tag; garbage that decodes to a plausible
// number is not, and that is what an unanswered tag contains.
func (t *MailboxTag) Responded() bool {
	return t.Code&0x80000000 != 0
}

type MailboxMessage struct {
	MinSize int
	Code    uint32
	Tags    []MailboxTag
}

func (m *MailboxMessage) Error() bool {
	return m.Code == 0x80000001
}

func (m *MailboxMessage) Tag(code uint32) *MailboxTag {
	for i, tag := range m.Tags {
		if tag.ID&0x7FFFFFFF == code&0x7FFFFFFF {
			return &m.Tags[i]
		}
	}

	return nil
}

// Call exchanges message via a mailbox channel
//
// The caller is responsible for ensuring the 'tags' in the
// message have sufficient buffer allocated for the response
// expected.  The response replaces the input message.
func (mb *mailbox) Call(channel int, message *MailboxMessage) {
	// Serialize the entire transaction: the shared scratch region, the two
	// full-cache flushes, the doorbell exchange, and the response parse must
	// not interleave with another caller (framebuffer, USB power, SD power,
	// GPU alloc all use the mailbox concurrently at boot).
	mb.Lock()
	defer mb.Unlock()

	size := 8 // Message Header
	for _, tag := range message.Tags {
		// 3 word tag header + tag data (padded to 32-bits)
		size += int(12 + (uint32(len(tag.Buffer)+3) & 0xFFFFFFFC))
	}

	size += 4 // null tag

	// Allow client to request bigger buffer for response
	if size < message.MinSize {
		size = message.MinSize
	}

	// Allocate temporary location-fixed buffer
	addr, buf := mb.Region.Reserve(size, 16)
	defer mb.Region.Release(addr)

	binary.LittleEndian.PutUint32(buf[0:], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:], 0)

	offset := 8
	for _, tag := range message.Tags {
		binary.LittleEndian.PutUint32(buf[offset:], tag.ID)
		binary.LittleEndian.PutUint32(buf[offset+4:], uint32(len(tag.Buffer)))
		binary.LittleEndian.PutUint32(buf[offset+8:], 0)
		copy(buf[offset+12:], tag.Buffer)

		offset += int(12 + (uint32(len(tag.Buffer)+3) & 0xFFFFFFFC))
	}

	// terminating null tag
	binary.LittleEndian.PutUint32(buf[offset:], 0x0)

	// The VideoCore reads the message through its uncached SDRAM alias:
	// clean the cached message out to DRAM first, then drop the (stale)
	// cached view before parsing the response written by the VideoCore.
	//
	// By-VA-to-PoC maintenance of just the message buffer, NOT a full set/way
	// flush: set/way maintenance is architecturally power-down-only and races a
	// second core sharing memory coherently (the bmx step generator), which can
	// lose a cache line the other work just wrote -> heap/stack corruption that
	// surfaces later. By-VA-to-PoC ops are broadcast to the inner-shareable
	// domain and are SMP-safe (and cheaper -- they touch only the buffer).
	bufStart := uint32(addr)
	bufEnd := bufStart + uint32(size)
	ARM.FlushDataCacheRange(bufStart, bufEnd)

	mb.exchangeMessage(channel, uint32(addr)|DRAM_FLAG_NOCACHE)

	ARM.FlushDataCacheRange(bufStart, bufEnd)

	message.Tags = make([]MailboxTag, 0, len(message.Tags))
	message.Code = binary.LittleEndian.Uint32(buf[4:])
	offset = 8

	for offset < len(buf) {
		tag := MailboxTag{}
		tag.ID = binary.LittleEndian.Uint32(buf[offset:])

		// Terminating null tag
		if tag.ID == 0 {
			break
		}

		len := binary.LittleEndian.Uint32(buf[offset+4:])

		if len > uint32(size-offset) {
			panic("malformed mailbox response, over-sized tag")
		}

		tag.Code = binary.LittleEndian.Uint32(buf[offset+8:])

		tag.Buffer = make([]byte, len)
		copy(tag.Buffer, buf[offset+12:])

		// Move to next tag
		offset += int(12 + (len+3)&0xFFFFFFFC)
		message.Tags = append(message.Tags, tag)
	}
}

// mailboxTimeout bounds each half of an exchange.
//
// The VideoCore answers a property request in microseconds, so this is four
// orders of magnitude of headroom and can only expire if the firmware is not
// going to answer at all.
//
// It exists because the wait used to be unbounded, and Call holds a package-wide
// mutex across the whole transaction -- one that the USB power path, the SD
// power path and the framebuffer all take. So a firmware that never answered
// did not fail one reading; it held that lock forever and took every future
// caller down with it, silently. An unbounded wait on another processor, under
// a shared lock, is a deadlock waiting for an excuse.
const mailboxTimeout = 1 * time.Second

// mailboxWait spins until the given status bit clears, or the timeout expires.
// Returns false if it expired.
func mailboxWait(bit uint32) bool {
	deadline := read_systimer() + int64(mailboxTimeout/time.Microsecond)

	for (reg.Read(peripheralBase+MAILBOX_STATUS_REG) & bit) != 0 {
		if read_systimer() > deadline {
			print("bcm2835: mailbox timed out waiting for the VideoCore; reading abandoned\n")

			return false
		}

		runtime.Gosched()
	}

	return true
}

func (mb *mailbox) exchangeMessage(channel int, addr uint32) {
	if (addr & 0xF) != 0 {
		panic("Mailbox message must be 16-byte aligned")
	}

	// The mailbox mutex is held by the calling Call() for the whole
	// transaction (buffer, cache flushes, exchange, parse).

	// Wait for space to send
	if !mailboxWait(MAILBOX_FULL) {
		return
	}

	// Send
	reg.Write(peripheralBase+MAILBOX_WRITE_REG, uint32(channel&0xF)|uint32(addr&0xFFFFFFF0))

	// Wait for response
	if !mailboxWait(MAILBOX_EMPTY) {
		return
	}

	// Read response
	data := reg.Read(peripheralBase + MAILBOX_READ_REG)

	// Ensure response corresponds to request (note response data over-writes request data)
	if (data & 0xF) != uint32(channel&0xF) {
		panic(fmt.Sprintf("overlapping messages, got response for channel %d, expecting %d", data&0xF, channel&0xF))
	}
	if (data & 0xFFFFFFF0) != (addr & 0xFFFFFFF0) {
		panic(fmt.Sprintf("overlapping messages, got response for channel %d, expecting %d", data&0xFFFFFFF0, addr&0xFFFFFFF0))
	}
}
