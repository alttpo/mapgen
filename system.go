package main

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/emulator/cpualt"
	"io"
)

type WRAMArray = [0x20000]byte
type SRAMArray = [0x10000]byte
type VRAMArray = [0x10000]byte
type OAMArray = [544]byte

type BusMapping int

const (
	BusUnspec BusMapping = iota
	BusLoROM
	BusHiROM
	BusExLoROM
	BusExHiROM
)

type System struct {
	// emulated system:
	cpualt.CPU
	HWIO HWIO

	BusMapping BusMapping

	ROM  []byte
	WRAM *WRAMArray
	SRAM *SRAMArray

	VRAM *VRAMArray
	OAM  *OAMArray

	Logger    io.Writer
	LoggerCPU io.Writer
}

func (s *System) InitMemory() {
	// allocate memory space if not already assigned:
	if s.WRAM == nil {
		s.WRAM = &WRAMArray{}
	}
	if s.SRAM == nil {
		s.SRAM = &SRAMArray{}
	}
	if s.VRAM == nil {
		s.VRAM = &VRAMArray{}
	}
	if s.OAM == nil {
		s.OAM = &OAMArray{}
	}
}

func (s *System) InitEmulatorFrom(initEmu *System) (err error) {
	s.InitMemory()

	s.Logger = initEmu.Logger
	s.LoggerCPU = initEmu.LoggerCPU

	s.BusMapping = initEmu.BusMapping

	s.ROM = initEmu.ROM

	// copy memory contents:
	*s.WRAM = *initEmu.WRAM
	*s.SRAM = *initEmu.SRAM
	*s.VRAM = *initEmu.VRAM
	*s.OAM = *initEmu.OAM

	s.HWIO = initEmu.HWIO

	s.CPU.InitFrom(&initEmu.CPU)

	switch s.BusMapping {
	case BusLoROM:
		if err = s.InitBusLoROM(); err != nil {
			return
		}
		break
	case BusHiROM:
		if err = s.InitBusHiROM(); err != nil {
			return
		}
		break
	default:
		panic("unsupported bus mapping type")
	}

	return
}

func (s *System) InitEmulator() (err error) {
	s.InitMemory()

	// create CPU and Bus:
	s.CPU.Init()

	return s.InitBusLoROM()
}

func (s *System) InitBusWRAM() (err error) {
	// WRAM:
	s.Bus.AttachReader(
		0x7E_0000,
		0x7F_FFFF,
		func(addr uint32) uint8 { return s.WRAM[addr-0x7E_0000] },
	)
	s.Bus.AttachWriter(
		0x7E_0000,
		0x7F_FFFF,
		func(addr uint32, val uint8) { s.WRAM[addr-0x7E_0000] = val },
	)

	// map in first $2000 of each bank as a mirror of WRAM:
	for b := uint32(0); b < 0x40; b++ {
		bank := b << 16
		s.Bus.AttachReader(
			bank,
			bank|0x1FFF,
			func(addr uint32) uint8 { return s.WRAM[addr-bank] },
		)
		s.Bus.AttachWriter(
			bank,
			bank|0x1FFF,
			func(addr uint32, val uint8) { s.WRAM[addr-bank] = val },
		)
	}
	for b := uint32(0x80); b < 0xC0; b++ {
		bank := b << 16
		s.Bus.AttachReader(
			bank,
			bank|0x1FFF,
			func(addr uint32) uint8 { return s.WRAM[addr-bank] },
		)
		s.Bus.AttachWriter(
			bank,
			bank|0x1FFF,
			func(addr uint32, val uint8) { s.WRAM[addr-bank] = val },
		)
	}

	return
}

func (s *System) InitBusHWIO() (err error) {
	s.HWIO.s = s

	// Memory-mapped IO registers:
	for b := uint32(0); b < 0x40; b++ {
		bank := b << 16
		s.Bus.AttachReader(
			bank|0x2000,
			bank|0x5FFF,
			s.HWIO.Read,
		)
		s.Bus.AttachWriter(
			bank|0x2000,
			bank|0x5FFF,
			s.HWIO.Write,
		)
	}
	for b := uint32(0x80); b < 0xC0; b++ {
		bank := b << 16
		s.Bus.AttachReader(
			bank|0x2000,
			bank|0x5FFF,
			s.HWIO.Read,
		)
		s.Bus.AttachWriter(
			bank|0x2000,
			bank|0x5FFF,
			s.HWIO.Write,
		)
	}

	return
}

func (s *System) InitBusLoROM() (err error) {
	// map in ROM to Bus; parts of this mapping will be overwritten:
	for b := uint32(0); b < 0x40; b++ {
		halfBank := b << 15
		bank := b << 16
		s.Bus.AttachReader(
			bank|0x8000,
			bank|0xFFFF,
			func(addr uint32) uint8 { return s.ROM[halfBank+(addr-(bank|0x8000))] },
		)

		// mirror:
		s.Bus.AttachReader(
			(bank+0x80_0000)|0x8000,
			(bank+0x80_0000)|0xFFFF,
			func(addr uint32) uint8 { return s.ROM[halfBank+(addr-((bank+0x80_0000)|0x8000))] },
		)
	}

	// SRAM (banks 70-7D,F0-FF); (7E,7F) will be overwritten with WRAM:
	for b := uint32(0); b < uint32(len(s.SRAM)>>15); b++ {
		bank := b << 16
		halfBank := b << 15
		s.Bus.AttachReader(
			bank+0x70_0000,
			bank+0x70_7FFF,
			func(addr uint32) uint8 { return s.SRAM[halfBank+(addr-(bank+0x70_0000))] },
		)
		s.Bus.AttachReader(
			bank+0xF0_0000,
			bank+0xF0_7FFF,
			func(addr uint32) uint8 { return s.SRAM[halfBank+(addr-(bank+0xF0_0000))] },
		)
		s.Bus.AttachWriter(
			bank+0x70_0000,
			bank+0x70_7FFF,
			func(addr uint32, val uint8) { s.SRAM[halfBank+(addr-(bank+0x70_0000))] = val },
		)
		s.Bus.AttachWriter(
			bank+0xF0_0000,
			bank+0xF0_7FFF,
			func(addr uint32, val uint8) { s.SRAM[halfBank+(addr-(bank+0xF0_0000))] = val },
		)
	}

	if err = s.InitBusWRAM(); err != nil {
		return
	}
	if err = s.InitBusHWIO(); err != nil {
		return
	}

	return
}

func (s *System) InitBusHiROM() (err error) {
	// map in ROM to Bus; parts of this mapping will be overwritten:
	for b := uint32(0); b < 0x40; b++ {
		halfBank := b << 15
		bank := b << 16
		s.Bus.AttachReader(
			bank|0x8000,
			bank|0xFFFF,
			func(addr uint32) uint8 { return s.ROM[halfBank+(addr-(bank|0x8000))] },
		)

		// mirror to 80..BF:
		bank80 := bank | 0x80_0000
		s.Bus.AttachReader(
			bank80|0x8000,
			bank80|0xFFFF,
			func(addr uint32) uint8 { return s.ROM[halfBank+(addr-(bank80|0x8000))] },
		)

		// mirror full banks 40..7F:
		bank40 := (b + 0x40) << 16
		if bank40 < 0x7E { // precaution: dont overwrite WRAM mapping
			s.Bus.AttachReader(
				bank40,
				bank40|0xFFFF,
				func(addr uint32) uint8 { return s.ROM[addr-bank40] },
			)
		}
		// mirror full banks C0..FF:
		bankC0 := (b + 0xC0) << 16
		s.Bus.AttachReader(
			bankC0,
			bankC0|0xFFFF,
			func(addr uint32) uint8 { return s.ROM[addr-bankC0] },
		)
	}

	// SRAM (banks 20-3F,A0-BF : 6000-7FFF):
	for b, offs := uint32(0x20), uint32(0); b < 0x40; b, offs = b+1, offs+0x2000 {
		bank20 := b<<16 | 0x6000
		s.Bus.AttachReader(
			bank20,
			bank20|0x1FFF,
			func(addr uint32) uint8 { return s.SRAM[((addr&0x1F_0000)>>13)|(addr-bank20)] },
		)
		bankA0 := (b+0x80)<<16 | 0x6000
		s.Bus.AttachReader(
			bankA0,
			bankA0|0x1FFF,
			func(addr uint32) uint8 { return s.SRAM[((addr&0x1F_0000)>>13)|(addr-bankA0)] },
		)
	}

	if err = s.InitBusHWIO(); err != nil {
		return
	}
	if err = s.InitBusWRAM(); err != nil {
		return
	}

	return
}

func (s *System) ReadWRAM24(offs uint32) uint32 {
	lohi := uint32(binary.LittleEndian.Uint16(s.WRAM[offs : offs+2]))
	bank := uint32(s.WRAM[offs+3])
	return bank<<16 | lohi
}

func (s *System) ReadWRAM16(offs uint32) uint16 {
	return binary.LittleEndian.Uint16(s.WRAM[offs : offs+2])
}

func (s *System) ReadWRAM8(offs uint32) uint8 {
	return s.WRAM[offs]
}

func (s *System) SetPC(pc uint32) {
	s.CPU.RK = byte(pc >> 16)
	s.CPU.PC = uint16(pc & 0xFFFF)
}

func (s *System) GetPC() uint32 {
	return uint32(s.CPU.RK)<<16 | uint32(s.CPU.PC)
}

func (s *System) RunUntil(targetPC uint32, maxCycles uint64) (stopPC uint32, expectedPC uint32, cycles uint64) {
	// clear stopped flag:
	s.CPU.Stopped = false

	expectedPC = targetPC
	for cycles = uint64(0); cycles < maxCycles; {
		if s.LoggerCPU != nil {
			s.CPU.DisassembleCurrentPC(s.LoggerCPU)
			fmt.Fprintln(s.LoggerCPU)
		}
		if s.GetPC() == targetPC {
			break
		}

		nCycles, abort := s.CPU.Step()
		cycles += uint64(nCycles)

		if abort {
			// fake that it's ok:
			stopPC = s.GetPC()
			expectedPC = s.GetPC()
			return
		}
	}

	stopPC = s.GetPC()
	return
}

func (s *System) ExecAt(startPC, donePC uint32) (err error) {
	s.SetPC(startPC)
	return s.Exec(donePC)
}

func (s *System) Exec(donePC uint32) (err error) {
	var stopPC uint32
	var expectedPC uint32
	var cycles uint64

	if stopPC, expectedPC, cycles = s.RunUntil(donePC, 0x1000_0000); stopPC != expectedPC {
		err = fmt.Errorf("CPU ran too long and did not reach PC=%#06x; actual=%#06x; took %d cycles", expectedPC, stopPC, cycles)
		return
	}

	return
}
