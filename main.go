package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/alttpo/snes"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"image"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"unsafe"
)

var (
	b02LoadUnderworldSupertilePC     uint32 = 0x02_5200
	b01LoadAndDrawRoomPC             uint32
	b01LoadAndDrawRoomSetSupertilePC uint32
	b00HandleRoomTagsPC              uint32 = 0x00_5300
	b00RunSingleFramePC              uint32 = 0x00_5400
	loadEntrancePC                   uint32
	setEntranceIDPC                  uint32
	loadSupertilePC                  uint32
	donePC                           uint32
)

var (
	roomsWithPitDamage map[Supertile]bool
)

var (
	outputEntranceSupertiles bool
	drawOverlays             bool
	drawNumbers              bool
	supertileGifs            bool
	animateRoomDrawing       bool
	animateRoomDrawingDelay  int
	enemyMovementFrames      int
	drawRoomPNGs             bool
	drawBGLayerPNGs          bool
	drawEG1                  bool
	drawEG2                  bool
	useGammaRamp             bool
	drawBG1p0                bool
	drawBG1p1                bool
	drawBG2p0                bool
	drawBG2p1                bool
	optimizeGIFs             bool
	staticEntranceMap        bool
)

// entranceSupertiles is generated from `mapgen -entrancemap`
var entranceSupertiles = map[uint8][]uint16{
	0x0:  []uint16{},
	0x1:  []uint16{0x104},
	0x2:  []uint16{0x12},
	0x3:  []uint16{0x60, 0x50, 0x1, 0x72, 0x82, 0x81, 0x71, 0x70},
	0x4:  []uint16{0x61},
	0x5:  []uint16{0x62, 0x52},
	0x6:  []uint16{0xf0},
	0x7:  []uint16{0xf1},
	0x8:  []uint16{0xc9, 0xb9, 0xa9, 0xaa, 0xa8, 0xba, 0xb8, 0x99, 0xda, 0xd9, 0xd8, 0xc8, 0x89},
	0x9:  []uint16{0x84, 0x74, 0x75},
	0xa:  []uint16{0x85},
	0xb:  []uint16{0x83, 0x73},
	0xc:  []uint16{0x63, 0x53, 0x43, 0x33},
	0xd:  []uint16{0xf2},
	0xe:  []uint16{0xf3},
	0xf:  []uint16{0xf4, 0xf5},
	0x10: []uint16{},
	0x11: []uint16{},
	0x12: []uint16{},
	0x13: []uint16{0xf8, 0xe8},
	0x14: []uint16{},
	0x15: []uint16{0x23},
	0x16: []uint16{0xfb},
	0x17: []uint16{0xeb},
	0x18: []uint16{0xd5, 0xc5, 0xc4, 0xb4, 0xa4, 0xb5, 0x4},
	0x19: []uint16{0x24, 0x14, 0x13},
	0x1a: []uint16{0xfd},
	0x1b: []uint16{0xed},
	0x1c: []uint16{0xfe},
	0x1d: []uint16{0xee},
	0x1e: []uint16{0xff},
	0x1f: []uint16{0xef},
	0x20: []uint16{0xdf},
	0x21: []uint16{0xf9},
	0x22: []uint16{0xfa},
	0x23: []uint16{0xea},
	0x24: []uint16{0xe0, 0xd0, 0xc0, 0xb0, 0x40, 0x20},
	0x25: []uint16{0x28, 0x38, 0x37, 0x36, 0x26, 0x76, 0x66, 0x16, 0x6, 0x35, 0x34, 0x54, 0x46},
	0x26: []uint16{0x4a, 0x9, 0x3a, 0xa, 0x4b, 0x3b, 0x2b, 0x2a, 0x1a, 0x6a, 0x5a, 0x19, 0x1b, 0xb},
	0x27: []uint16{0x98, 0xd2, 0xc2, 0xc1, 0xb1, 0xb2, 0xa2, 0x93, 0x92, 0x91, 0xa0, 0x90, 0xb3, 0xa3, 0xa1, 0xc3, 0xd1, 0x97},
	0x28: []uint16{},
	0x29: []uint16{0x57},
	0x2a: []uint16{},
	0x2b: []uint16{0x59, 0x49, 0x39, 0x29},
	0x2c: []uint16{},
	0x2d: []uint16{0xe, 0x1e, 0x3e, 0x4e, 0x6e, 0x5e, 0x7e, 0x9e, 0xbe, 0xce, 0xbf, 0x4f, 0x9f, 0xaf, 0xae, 0x8e, 0x7f, 0x5f, 0x3f, 0x1f, 0x2e},
	0x2e: []uint16{0xe6},
	0x2f: []uint16{0xe7},
	0x30: []uint16{0xe4},
	0x31: []uint16{0xe5},
	0x32: []uint16{},
	0x33: []uint16{0x77, 0x31, 0x27, 0x17, 0xa7, 0x7, 0x87},
	0x34: []uint16{0xdb, 0xcb, 0xcc, 0xbc, 0xac, 0xbb, 0xab, 0x64, 0x65, 0x45, 0x44, 0xdc},
	0x35: []uint16{0xd6, 0xc6, 0xb6, 0x15, 0xc7, 0xb7},
	0x36: []uint16{0x10},
	0x37: []uint16{0xc, 0x8c, 0x1c, 0x8b, 0x7b, 0x9b, 0x7d, 0x7c, 0x9c, 0x9d, 0x8d, 0x6b, 0x5b, 0x5c, 0x5d, 0x6d, 0x6c, 0xa5, 0x95, 0x96, 0x3d, 0x4d, 0xa6, 0x4c, 0x1d, 0xd},
	0x38: []uint16{0x8},
	0x39: []uint16{},
	0x3a: []uint16{0x3c},
	0x3b: []uint16{0x2c},
	0x3c: []uint16{0x100},
	0x3d: []uint16{},
	0x3e: []uint16{0x101},
	0x3f: []uint16{},
	0x40: []uint16{0x102},
	0x41: []uint16{0x117},
	0x42: []uint16{},
	0x43: []uint16{},
	0x44: []uint16{0x103},
	0x45: []uint16{0x105},
	0x46: []uint16{0x11f},
	0x47: []uint16{0x106},
	0x48: []uint16{},
	0x49: []uint16{},
	0x4a: []uint16{0x107},
	0x4b: []uint16{0x108},
	0x4c: []uint16{0x109},
	0x4d: []uint16{0x10a},
	0x4e: []uint16{0x10b},
	0x4f: []uint16{},
	0x50: []uint16{0x10c},
	0x51: []uint16{},
	0x52: []uint16{0x11b},
	0x53: []uint16{0x11c},
	0x54: []uint16{},
	0x55: []uint16{0x11e},
	0x56: []uint16{0x120},
	0x57: []uint16{0x110},
	0x58: []uint16{},
	0x59: []uint16{0x111},
	0x5a: []uint16{0x112},
	0x5b: []uint16{0x113},
	0x5c: []uint16{0x114},
	0x5d: []uint16{},
	0x5e: []uint16{0x115},
	0x5f: []uint16{0x10d},
	0x60: []uint16{0x10f},
	0x61: []uint16{0x119, 0x11d},
	0x62: []uint16{},
	0x63: []uint16{0x116},
	0x64: []uint16{0x121},
	0x65: []uint16{},
	0x66: []uint16{0x122},
	0x67: []uint16{0x118},
	0x68: []uint16{0x11a},
	0x69: []uint16{},
	0x6a: []uint16{0x10e},
	0x6b: []uint16{},
	0x6c: []uint16{0x123},
	0x6d: []uint16{},
	0x6e: []uint16{0x124},
	0x6f: []uint16{0x125},
	0x70: []uint16{},
	0x71: []uint16{0x126},
	0x72: []uint16{},
	0x73: []uint16{0x80},
	0x74: []uint16{0x51, 0x41, 0x42, 0x32, 0x22},
	0x75: []uint16{0x30},
	0x76: []uint16{0x58},
	0x77: []uint16{0x67},
	0x78: []uint16{0x68},
	0x79: []uint16{0x56},
	0x7a: []uint16{0xe1},
	0x7b: []uint16{0x0},
	0x7c: []uint16{0x18},
	0x7d: []uint16{0x55},
	0x7e: []uint16{0xe3},
	0x7f: []uint16{0xe2},
	0x80: []uint16{0x2f},
	0x81: []uint16{0x11, 0x2, 0x21},
	0x82: []uint16{0x3},
	0x83: []uint16{0x127},
	0x84: []uint16{},
}

var fastRomBank uint32 = 0

func main() {
	entranceMinStr, entranceMaxStr := "", ""
	flag.BoolVar(&optimizeGIFs, "optimize", true, "optimize GIFs for size with delta frames")
	flag.BoolVar(&outputEntranceSupertiles, "entrancemap", false, "dump entrance-supertile map to stdout")
	flag.BoolVar(&drawRoomPNGs, "roompngs", false, "create individual room PNGs")
	flag.BoolVar(&drawBGLayerPNGs, "bgpngs", false, "create individual room BG layer PNGs")
	flag.BoolVar(&drawBG1p0, "bg1p0", true, "draw BG1 priority 0 tiles")
	flag.BoolVar(&drawBG1p1, "bg1p1", true, "draw BG1 priority 1 tiles")
	flag.BoolVar(&drawBG2p0, "bg2p0", true, "draw BG2 priority 0 tiles")
	flag.BoolVar(&drawBG2p1, "bg2p1", true, "draw BG2 priority 1 tiles")
	flag.BoolVar(&drawNumbers, "numbers", true, "draw room numbers")
	flag.BoolVar(&drawEG1, "eg1", false, "create eg1.png")
	flag.BoolVar(&drawEG2, "eg2", false, "create eg2.png")
	flag.BoolVar(&drawOverlays, "overlay", false, "draw reachable overlays on eg1/eg2")
	flag.BoolVar(&useGammaRamp, "gamma", false, "use bsnes gamma ramp")
	flag.BoolVar(&supertileGifs, "gifs", false, "render room GIFs")
	flag.BoolVar(&animateRoomDrawing, "animate", false, "render animated room drawing GIFs")
	flag.IntVar(&animateRoomDrawingDelay, "animdelay", 15, "room drawing GIF frame delay")
	flag.IntVar(&enemyMovementFrames, "movementframes", 0, "render N frames in animated GIF of enemy movement after room load")
	flag.StringVar(&entranceMinStr, "entmin", "0", "entrance ID range minimum (hex)")
	flag.StringVar(&entranceMaxStr, "entmax", "84", "entrance ID range maximum (hex)")
	flag.BoolVar(&staticEntranceMap, "static", false, "use static entrance->supertile map from JP 1.0")
	flag.Parse()

	var err error

	// create the CPU-only SNES emulator:
	e := System{
		//Logger:    os.Stdout,
		LoggerCPU:  nil,
		BusMapping: BusLoROM,
		ROM:        make([]byte, 0x100_0000),
	}

	args := flag.Args()

	var romPath string
	{
		if len(args) > 0 {
			romPath = args[0]
			args = args[1:]
		} else {
			romPath = "alttp-jp.sfc"
		}

		var f *os.File
		f, err = os.Open(romPath)
		if err != nil {
			panic(err)
		}

		_, err = f.Read(e.ROM[:])
		if err != nil {
			panic(err)
		}
		err = f.Close()
		if err != nil {
			panic(err)
		}
	}

	// read the ROM header
	{
		var h snes.Header
		if err = h.ReadHeader(bytes.NewReader(e.ROM[0x7FB0:0x8000])); err != nil {
			panic(err)
		}
		mapper := h.MapMode & ^uint8(0x10)
		if mapper == 0x20 {
			e.BusMapping = BusLoROM
		} else if mapper == 0x21 {
			e.BusMapping = BusHiROM
		} else if mapper == 0x22 {
			e.BusMapping = BusExLoROM
		} else if mapper == 0x25 {
			e.BusMapping = BusExHiROM
		} else {
			panic("unrecognized MapMode in ROM header")
		}

		if h.MapMode&0x10 != 0 {
			fmt.Println("FastROM")
			fastRomBank = 0x80_0000
		} else {
			fmt.Println("SlowROM")
			fastRomBank = 0
		}
	}

	if err = e.InitEmulator(); err != nil {
		panic(err)
	}

	setupAlttp(&e)

	{
		// find Underworld_LoadHeader routine to check the RoomHeader pointer it uses
		// analyze the overlaps of the room headers and mark invalid any overlapping values
		// iterate through entrance IDs to determine dungeon IDs
		// iterate through supertile links

		// RoomHeader_RoomToPointer#_04F1E2
		// RoomHeader_Room0000#_04F462

		// #_01B5DC: LDA.l RoomHeader_RoomToPointer,X
		if e.Bus.Read8(fastRomBank|0x01_b5dc) != 0xBF {
			panic("unexpected opcode at #_01B5DC; expecting `LDA.l $nnnnnn,X`")
		}
		roomHeaderTableLong := e.Bus.Read24(fastRomBank | 0x01_b5dc + 1)
		fmt.Printf("table: %06x\n", roomHeaderTableLong)

		aliases := map[uint16]uint16{}
		roomHeaderPointers := [0x140]uint16{}
		for i := uint16(0); i < 0x140; i++ {
			roomHeaderPointers[i] = e.Bus.Read16(roomHeaderTableLong + uint32(i)<<1)
			fmt.Printf("[%03x]: %04x\n", i, roomHeaderPointers[i])

			// find our alias:
			for j := uint16(0); j < i; j++ {
				if roomHeaderPointers[i] == roomHeaderPointers[j] {
					aliases[i] = j
					fmt.Printf("  alias for %03x\n", j)
					break
				}
			}
		}

		// make a map of which room index owns which bytes in the table (earliest room ID "wins"):
		addrUsed := map[uint16]uint16{}
		for i := uint16(0); i < 0x140; i++ {
			// skip overlap analysis for aliased pointers:
			if _, ok := aliases[i]; ok {
				continue
			}

			for j := uint16(0); j < 14; j++ {
				p := roomHeaderPointers[i] + j
				addrUsed[p] = i
			}
		}

		roomHeaders := [0x140][14]uint8{}
		for i := uint16(0); i < 0x140; i++ {
			owner := i
			if alias, ok := aliases[owner]; ok {
				owner = alias
			}
			for j := uint16(0); j < 14; j++ {
				p := roomHeaderPointers[owner] + j
				if addrUsed[p] == owner {
					roomHeaders[i][j] = e.Bus.Read8(roomHeaderTableLong&0xFF_0000 | uint32(p))
				}
			}
		}

		for i := uint16(0); i < 0x140; i++ {
			fmt.Printf("[%03x]: %#v\n", i, roomHeaders[i])
		}

		os.Exit(0)
	}

	//RoomsWithPitDamage#_00990C [0x70]uint16
	roomsWithPitDamage = make(map[Supertile]bool, 0x128)
	for i := Supertile(0); i < 0x128; i++ {
		roomsWithPitDamage[i] = false
	}
	for i := 0; i <= 0x70; i++ {
		romaddr, _ := lorom.BusAddressToPak(0x00_990C)
		st := Supertile(read16(e.ROM[:], romaddr+uint32(i)<<1))
		roomsWithPitDamage[st] = true
	}

	const entranceCount = 0x85
	entranceGroups := make([]Entrance, entranceCount)

	// iterate over entrances:
	var entranceMin, entranceMax uint8

	var entranceMin64 uint64
	entranceMin64, err = strconv.ParseUint(entranceMinStr, 16, 8)
	if err != nil {
		entranceMin64 = 0
	}
	entranceMin = uint8(entranceMin64)

	var entranceMax64 uint64
	entranceMax64, err = strconv.ParseUint(entranceMaxStr, 16, 8)
	if err != nil {
		entranceMax64 = entranceCount - 1
	}
	entranceMax = uint8(entranceMax64)

	if entranceMax < entranceMin {
		entranceMin, entranceMax = entranceMax, entranceMin
	}

	if entranceMin > entranceCount-1 {
		entranceMin = entranceCount - 1
	}
	if entranceMax > entranceCount-1 {
		entranceMax = entranceCount - 1
	}

	//entranceMin, entranceMax := uint8(0), uint8(entranceCount-1)

	wg := sync.WaitGroup{}
	for eID := entranceMin; eID <= entranceMax; eID++ {
		g := &entranceGroups[eID]
		g.EntranceID = eID

		// process entrances in parallel
		wg.Add(1)
		go func() {
			processEntrance(&e, g, &wg)
			wg.Done()
		}()
	}

	wg.Wait()

	if outputEntranceSupertiles {
		fmt.Printf("rooms := map[uint8][]uint16{\n")
		for i := range entranceGroups {
			g := &entranceGroups[i]
			sts := make([]uint16, 0, 0x100)
			for _, r := range g.Rooms {
				sts = append(sts, uint16(r.Supertile))
			}
			fmt.Printf("\t%#v: %#v,\n", g.EntranceID, sts)
		}
		fmt.Printf("}\n")
	}

	// condense all maps into big atlas images:
	if drawEG1 {
		wg.Add(1)
		go func() {
			renderAll("eg1", entranceGroups, 0x00, 0x10)
			wg.Done()
		}()
	}
	if drawEG2 {
		wg.Add(1)
		go func() {
			renderAll("eg2", entranceGroups, 0x10, 0x3)
			wg.Done()
		}()
	}
	if drawEG1 || drawEG2 {
		wg.Wait()
	}
}

func processEntrance(
	initEmu *System,
	g *Entrance,
	wg *sync.WaitGroup,
) {
	var err error

	e := &System{}
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

	eID := g.EntranceID
	fmt.Printf("entrance $%02x load start\n", eID)

	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID

	// load the entrance and draw the room:
	if eID > 0 {
		//e.LoggerCPU = os.Stdout
	}
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}
	e.LoggerCPU = nil

	fmt.Printf("entrance $%02x load complete\n", eID)

	g.Supertile = Supertile(read16(e.WRAM[:], 0xA0))

	{
		// if this is the entrance, Link should be already moved to his starting position:
		wram := e.WRAM[:]
		linkX := read16(wram, 0x22)
		linkY := read16(wram, 0x20)
		linkLayer := read16(wram, 0xEE)
		g.EntryCoord = AbsToMapCoord(linkX, linkY, linkLayer)
		//fmt.Printf("  link coord = {%04x, %04x, %04x}\n", linkX, linkY, linkLayer)
	}

	g.Rooms = make([]*RoomState, 0, 0x20)
	g.Supertiles = make(map[Supertile]*RoomState, 0x128)

	// build a stack (LIFO) of supertile entry points to visit:
	lifo := make([]EntryPoint, 0, 0x100)
	lifo = append(lifo, EntryPoint{g.Supertile, g.EntryCoord, DirNone, ExitPoint{}})
	if staticEntranceMap {
		// queue up all supertiles that belong to this entrance:
		for _, supertile := range entranceSupertiles[g.EntranceID] {
			lifo = append(lifo, EntryPoint{
				Supertile: Supertile(supertile),
				Point:     g.EntryCoord, // TODO!
				Direction: DirNone,
				From:      ExitPoint{},
			})
		}
	}

	// process the LIFO:
	for len(lifo) != 0 {
		// pop off the stack:
		lifoEnd := len(lifo) - 1
		ep := lifo[lifoEnd]
		lifo = lifo[0:lifoEnd]

		this := ep.Supertile

		//fmt.Printf("  ep = %s\n", ep)

		// create a room:
		var room *RoomState

		g.SupertilesLock.Lock()
		var ok bool
		if room, ok = g.Supertiles[this]; ok {
			//fmt.Printf("    reusing room %s\n", this)
			//if eID != room.Entrance.EntranceID {
			//	panic(fmt.Errorf("conflicting entrances for room %s", this))
			//}
		} else {
			// create new room:
			room = CreateRoom(g, this, e)
			g.Rooms = append(g.Rooms, room)
			g.Supertiles[this] = room
		}
		g.SupertilesLock.Unlock()

		// emulate loading the room:
		room.Lock()
		//fmt.Printf("entrance $%02x supertile %s discover from entry %s start\n", eID, room.Supertile, ep)

		if err = room.Init(ep); err != nil {
			panic(err)
		}

		if !staticEntranceMap {
			// check if room causes pit damage vs warp:
			// RoomsWithPitDamage#_00990C [0x70]uint16
			pitDamages := roomsWithPitDamage[this]

			warpExitTo := room.WarpExitTo
			stairExitTo := &room.StairExitTo
			warpExitLayer := room.WarpExitLayer
			stairTargetLayer := &room.StairTargetLayer

			pushEntryPoint := func(ep EntryPoint, name string) {
				// for EG2:
				if this >= 0x100 {
					ep.Supertile |= 0x100
				}

				room.EntryPoints = append(room.EntryPoints, ep)
				room.ExitPoints = append(room.ExitPoints, ExitPoint{
					Supertile:    ep.Supertile,
					Point:        ep.From.Point,
					Direction:    ep.From.Direction,
					WorthMarking: ep.From.WorthMarking,
				})

				lifo = append(lifo, ep)
				//fmt.Printf("    %s to %s\n", name, ep)
			}

			// dont need to read interroom stair list from $06B0; just link stair tile number to STAIRnTO exit

			// flood fill to find reachable tiles:
			tiles := &room.Tiles
			room.FindReachableTiles(
				ep,
				func(s ScanState, v uint8) {
					t := s.t
					d := s.d

					exit := ExitPoint{
						ep.Supertile,
						t,
						d,
						false,
					}

					// here we found a reachable tile:
					room.Reachable[t] = v

					if v == 0x00 {
						// detect edge walkways:
						if ok, edir, _, _ := t.IsEdge(); ok {
							if sn, _, ok := this.MoveBy(edir); ok {
								pushEntryPoint(EntryPoint{sn, t.OppositeEdge(), edir, exit}, fmt.Sprintf("%s walkway", edir))
							}
						}
						return
					}

					// door objects:
					if v >= 0xF0 {
						//fmt.Printf("    door tile $%02x at %s\n", v, t)
						// dungeon exits are already patched out, so this should be a normal door
						lyr, row, col := t.RowCol()
						if row >= 0x3A {
							// south:
							if sn, sd, ok := this.MoveBy(DirSouth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x06 << 6) | col), sd, exit}, "south door")
							}
						} else if row <= 0x06 {
							// north:
							if sn, sd, ok := this.MoveBy(DirNorth); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (0x3A << 6) | col), sd, exit}, "north door")
							}
						} else if col >= 0x3A {
							// east:
							if sn, sd, ok := this.MoveBy(DirEast); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x06), sd, exit}, "east door")
							}
						} else if col <= 0x06 {
							// west:
							if sn, sd, ok := this.MoveBy(DirWest); ok {
								pushEntryPoint(EntryPoint{sn, MapCoord(lyr | (row << 6) | 0x3A), sd, exit}, "west door")
							}
						}

						return
					}

					// interroom doorways:
					if (v >= 0x80 && v <= 0x8D) || (v >= 0x90 && v <= 97) {
						if ok, edir, _, _ := t.IsDoorEdge(); ok && edir == d {
							// at or beyond the door edge zones:
							swapLayers := MapCoord(0)

							{
								// search the doorway for layer-swaps:
								tn := t
								for i := 0; i < 8; i++ {
									vd := tiles[tn]
									room.Reachable[tn] = vd
									if vd >= 0x90 && vd <= 0x9F {
										swapLayers = 0x1000
									}
									if vd >= 0xA8 && vd <= 0xAF {
										swapLayers = 0x1000
									}
									if _, ok := room.SwapLayers[tn]; ok {
										swapLayers = 0x1000
									}

									// advance into the doorway:
									tn, _, ok = tn.MoveBy(edir, 1)
									if !ok {
										break
									}
								}
							}

							if v&1 == 0 {
								// north-south normal doorway (no teleport doorways for north-south):
								if sn, _, ok := this.MoveBy(edir); ok {
									pushEntryPoint(EntryPoint{sn, t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "north-south doorway")
								}
							} else {
								// east-west doorway:
								if v == 0x89 {
									// teleport doorway:
									if edir == DirWest {
										pushEntryPoint(EntryPoint{stairExitTo[2], t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "west teleport doorway")
									} else if edir == DirEast {
										pushEntryPoint(EntryPoint{stairExitTo[3], t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "east teleport doorway")
									} else {
										panic("invalid direction approaching east-west teleport doorway")
									}
								} else {
									// normal doorway:
									if sn, _, ok := this.MoveBy(edir); ok {
										pushEntryPoint(EntryPoint{sn, t.OnEdge(edir.Opposite()) ^ swapLayers, edir, exit}, "east-west doorway")
									}
								}
							}
						}
						return
					}

					if v >= 0x30 && v < 0x38 {
						var vn uint8
						vn = tiles[t-0x40]
						if vn == 0x80 || vn == 0x26 {
							vn = tiles[t+0x40]
						}

						if vn == 0x5E || vn == 0x5F {
							// spiral staircase
							tgtLayer := stairTargetLayer[v&3]
							dt := t
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									dt += 0x80
								}
								if tgtLayer != 0 {
									dt += 0x80
								}
								pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d.Opposite(), exit}, fmt.Sprintf("spiralStair(%s)", t))
							} else {
								// going down
								if t&0x1000 != 0 {
									dt -= 0x80
								}
								if tgtLayer != 0 {
									dt -= 0x80
								}
								pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d.Opposite(), exit}, fmt.Sprintf("spiralStair(%s)", t))
							}
							return
						} else if vn == 0x38 {
							// north stairs:
							tgtLayer := stairTargetLayer[v&3]
							dt := t.Col() + 0xFC0 - 2<<6
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
							} else {
								// going down
								// module #$07 submodule #$12 is going down north stairs (e.g. $042)
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
							}
							pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d, exit}, fmt.Sprintf("northStair(%s)", t))
							return
						} else if vn == 0x39 {
							// south stairs:
							tgtLayer := stairTargetLayer[v&3]
							dt := t.Col() + 2<<6
							if v&4 == 0 {
								// going up
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt -= 4 << 6
								}
							} else {
								// going down
								if t&0x1000 != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
								if tgtLayer != 0 {
									// 32 pixels = 4 8x8 tiles
									dt += 4 << 6
								}
							}
							pushEntryPoint(EntryPoint{stairExitTo[v&3], dt&0x0FFF | tgtLayer, d, exit}, fmt.Sprintf("southStair(%s)", t))
							return
						} else if vn == 0x00 {
							// straight stairs:
							pushEntryPoint(EntryPoint{stairExitTo[v&3], t&0x0FFF | stairTargetLayer[v&3], d.Opposite(), exit}, fmt.Sprintf("stair(%s)", t))
							return
						}
						panic(fmt.Errorf("unhandled stair exit at %s %s", t, d))
						return
					}

					// pit exits:
					if !pitDamages {
						if v == 0x20 {
							// pit tile
							exit.WorthMarking = !room.markedPit
							room.markedPit = true
							pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("pit(%s)", t))
							return
						} else if v == 0x62 {
							// bombable floor tile
							exit.WorthMarking = !room.markedFloor
							room.markedFloor = true
							pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("bombableFloor(%s)", t))
							return
						}
					}
					if v == 0x4B {
						// warp floor tile
						exit.WorthMarking = t&0x40 == 0 && t&0x01 == 0
						pushEntryPoint(EntryPoint{warpExitTo, t&0x0FFF | warpExitLayer, d, exit}, fmt.Sprintf("warp(%s)", t))
						return
					}

					if true {
						// manipulables (pots, hammer pegs, push blocks):
						if v&0xF0 == 0x70 {
							// find gfx tilemap position:
							j := (uint32(v) & 0x0F) << 1
							p := read16(room.WRAM[:], 0x0500+j)
							//fmt.Printf("    manip(%s) %02x = %04x\n", t, v, p)
							if p == 0 {
								//fmt.Printf("    pushBlock(%s)\n", t)

								// push block flips 0x0641
								write8(room.WRAM[:], 0x0641, 0x01)
								if read8(room.WRAM[:], 0xAE)|read8(room.WRAM[:], 0xAF) != 0 {
									// handle tags if there are any after the push to see if it triggers a secret:
									room.HandleRoomTags()
									// TODO: properly determine which tag was activated
									room.TilesVisited = room.TilesVisitedTag0
								}
							}
							return
						}

						v16 := read16(room.Tiles[:], uint32(t))
						if v16 == 0x3A3A || v16 == 0x3B3B {
							//fmt.Printf("    star(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							room.HandleRoomTags()

							// swap out visited maps:
							if read8(room.WRAM[:], 0x04BC) == 0 {
								//fmt.Printf("    star0\n")
								room.TilesVisited = room.TilesVisitedStar0
								//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap0", uint16(this)), room.Tiles[:], 0644)
							} else {
								//fmt.Printf("    star1\n")
								room.TilesVisited = room.TilesVisitedStar1
								//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap1", uint16(this)), room.Tiles[:], 0644)
							}
							return
						}

						// floor or pressure switch:
						if v16 == 0x2323 || v16 == 0x2424 {
							//fmt.Printf("    switch(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							if room.HandleRoomTags() {
								// reset current room visited state:
								for i := range room.TilesVisited {
									delete(room.TilesVisited, i)
								}
								//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap0", uint16(this)), room.Tiles[:], 0644)
							}
							return
						}
					}
				},
			)

			//ioutil.WriteFile(fmt.Sprintf("data/%03X.rch", uint16(this)), room.Reachable[:], 0644)

			//fmt.Printf("entrance $%02x supertile %s discover from entry %s complete\n", eID, room.Supertile, ep)
		}

		room.Unlock()
	}

	// render all supertiles found:
	for _, room := range g.Rooms {
		if supertileGifs || animateRoomDrawing {
			wg.Add(1)
			go func(r *RoomState) {
				r.Lock()
				defer r.Unlock()

				fmt.Printf("entrance $%02x supertile %s draw start\n", g.EntranceID, r.Supertile)

				if supertileGifs {
					RenderGIF(&r.GIF, fmt.Sprintf("data/%03x.%02x.gif", uint16(r.Supertile), r.Entrance.EntranceID))
				}

				if animateRoomDrawing {
					RenderGIF(&r.Animated, fmt.Sprintf("data/%03x.%02x.room.gif", uint16(r.Supertile), r.Entrance.EntranceID))
				}

				fmt.Printf("entrance $%02x supertile %s draw complete\n", g.EntranceID, r.Supertile)
				wg.Done()
			}(room)
		}

		// render VRAM BG tiles to a PNG:
		if false {
			cgram := (*(*[0x100]uint16)(unsafe.Pointer(&room.WRAM[0xC300])))[:]
			pal := cgramToPalette(cgram)

			tiles := 0x4000 / 32
			g := image.NewPaletted(image.Rect(0, 0, 16*8, (tiles/16)*8), pal)
			for t := 0; t < tiles; t++ {
				// palette 2
				z := uint16(t) | (2 << 10)
				draw4bppTile(
					g,
					z,
					(&room.VRAMTileSet)[:],
					t%16,
					t/16,
				)
			}

			if err = exportPNG(fmt.Sprintf("data/%03X.vram.png", uint16(room.Supertile)), g); err != nil {
				panic(err)
			}
		}
	}
}

func scanForTileTypes(e *System) {
	var err error

	// scan underworld for certain tile types:
	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC-0x5000] = 0x00
	// load the entrance and draw the room:
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}

	for st := uint16(0); st < 0x128; st++ {
		// load and draw current supertile:
		write16(e.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, st)
		if err = e.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
			panic(err)
		}

		found := false
		for t, v := range e.WRAM[0x12000:0x14000] {
			if v == 0x0A {
				found = true
				fmt.Printf("%s: %s = $0A\n", Supertile(st), MapCoord(t))
			}
		}

		if found {
			ioutil.WriteFile(fmt.Sprintf("data/%03x.tmap", st), e.WRAM[0x12000:0x14000], 0644)
		}
	}

	return

}

func setupAlttp(e *System) {
	var a *asm.Emitter
	var err error

	// initialize game:
	e.CPU.Reset()
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this
	// this is useless zeroing of memory; don't need to run it
	//#_00802C: JSR Startup_InitializeMemory
	if err = e.Exec(fastRomBank | 0x00_8029); err != nil {
		panic(err)
	}

	b00RunSingleFramePC |= fastRomBank
	b00HandleRoomTagsPC |= fastRomBank
	b01LoadAndDrawRoomPC |= fastRomBank
	b01LoadAndDrawRoomSetSupertilePC |= fastRomBank
	b02LoadUnderworldSupertilePC |= fastRomBank

	// NOTE: appears unused
	{
		// must execute in bank $01
		a = asm.NewEmitter(e.HWIO.Dyn[0x01_5100&0xFFFF-0x5000:], true)
		a.SetBase(fastRomBank | 0x01_5100)

		{
			b01LoadAndDrawRoomPC = a.Label("loadAndDrawRoom")
			a.REP(0x30)
			b01LoadAndDrawRoomSetSupertilePC = a.Label("loadAndDrawRoomSetSupertile") + 1
			a.LDA_imm16_w(0x0000)
			a.STA_dp(0xA0)
			a.STA_abs(0x048E)
			a.SEP(0x30)

			// loads header and draws room
			a.Comment("Underworld_LoadRoom#_01873A")
			a.JSL(fastRomBank | 0x01_873A)

			a.Comment("Underworld_LoadCustomTileAttributes#_0FFD65")
			a.JSL(fastRomBank | 0x0F_FD65)
			a.Comment("Underworld_LoadAttributeTable#_01B8BF")
			a.JSL(fastRomBank | 0x01_B8BF)

			// then JSR Underworld_LoadHeader#_01B564 to reload the doors into $19A0[16]
			//a.BRA("jslUnderworld_LoadHeader")
			a.STP()
		}

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	// this routine renders a supertile assuming gfx tileset and palettes already loaded:
	{
		// emit into our custom $02:5100 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b02LoadUnderworldSupertilePC&0xFFFF-0x5000:], true)
		a.SetBase(b02LoadUnderworldSupertilePC)
		a.Comment("setup bank restore back to $00")
		a.SEP(0x30)
		a.LDA_imm8_b(0x00)
		a.PHA()
		a.PLB()
		a.Comment("in Underworld_LoadEntrance_DoPotsBlocksTorches at PHB and bank switch to $7e")
		a.JSR_abs(0xD854)
		a.Comment("Module06_UnderworldLoad after JSR Underworld_LoadEntrance")
		a.JMP_abs_imm16_w(0x8157)
		a.Comment("implied RTL")
		a.WriteTextTo(e.Logger)
	}

	if false {
		// TODO: pit detection using Link_ControlHandler
		// bank 07
		// force a pit detection:
		// set $02E4 = 0 to allow control of link
		// set $55 = 0 to disable cape
		// set $5D base state to $01 to check pits
		// set $5B = $02
		// JSL Link_Main#_078000
		// output $59 != 0 if pit detected; $A0 changed
	}

	{
		// emit into our custom $00:5000 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[:], true)
		a.SetBase(fastRomBank | 0x00_5000)
		a.SEP(0x30)

		a.Comment("InitializeTriforceIntro#_0CF03B: sets up initial state")
		a.JSL(fastRomBank | 0x0C_F03B)
		a.Comment("Intro_CreateTextPointers#_028022")
		a.JSL(fastRomBank | 0x02_8022)
		a.Comment("Overworld_LoadAllPalettes_long#_02802A")
		a.JSL(fastRomBank | 0x02_802A)
		a.Comment("DecompressFontGFX#_0EF572")
		a.JSL(fastRomBank | 0x0E_F572)

		a.Comment("LoadDefaultGraphics#_00E310")
		a.JSL(fastRomBank | 0x00_E310)
		a.Comment("LoadDefaultTileTypes#_0FFD2A")
		a.JSL(fastRomBank | 0x0F_FD2A)
		//a.Comment("DecompressAnimatedUnderworldTiles#_00D377")
		//a.JSL(fastRomBank | 0x00_D377)
		//a.Comment("InitializeTilesets#_00E1DB")
		//a.JSL(fastRomBank | 0x00_E1DB)

		// general world state:
		a.Comment("disable rain")
		a.LDA_imm8_b(0x02)
		a.STA_long(0x7EF3C5)

		a.Comment("no bed cutscene")
		a.LDA_imm8_b(0x10)
		a.STA_long(0x7EF3C6)

		loadEntrancePC = a.Label("loadEntrance")
		a.SEP(0x30)
		// prepare to call the underworld room load module:
		a.Comment("module $06, submodule $00:")
		a.LDA_imm8_b(0x06)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)

		a.Comment("dungeon entrance DungeonID")
		setEntranceIDPC = a.Label("setEntranceID") + 1
		a.LDA_imm8_b(0x08)
		a.STA_abs(0x010E)

		// loads a dungeon given an entrance ID:
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | 0x00_80B5)
		a.BRA("updateVRAM")

		loadSupertilePC = a.Label("loadSupertile")
		a.SEP(0x30)
		a.INC_abs(0x0710)
		a.Comment("Intro_InitializeDefaultGFX after JSL DecompressAnimatedUnderworldTiles")
		a.JSL(fastRomBank | 0x0C_C237)
		a.STZ_dp(0x11)
		a.Comment("LoadUnderworldSupertile")
		a.JSL(b02LoadUnderworldSupertilePC)
		a.STZ_dp(0x11)

		//a.Comment("check module=7, submodule!=f:")
		//a.LDA_dp(0x10)
		//a.CMP_imm8_b(0x07)
		//a.BNE("done")
		//a.LDA_dp(0x11)
		//a.BEQ("done")
		//a.Comment("clear submodule to avoid spotlight:")
		//a.STZ_dp(0x11)

		a.Label("updateVRAM")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates

		// WDM triggers an abort for values >= 10
		donePC = a.Label("done")
		a.STP()

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	{
		// emit into our custom $00:5300 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b00HandleRoomTagsPC&0xFFFF-0x5000:], true)
		a.SetBase(b00HandleRoomTagsPC)

		a.SEP(0x30)

		a.Comment("Module07_Underworld")
		a.LDA_imm8_b(0x07)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)

		//write8(e.WRAM[:], 0x04BA, 0)
		a.Comment("no cutscene")
		a.STZ_abs(0x02E4)
		a.Comment("enable tags")
		a.STZ_abs(0x04C7)

		//a.Comment("Graphics_LoadChrHalfSlot#_00E43A")
		//a.JSL(fastRomBank | 0x00_E43A)
		a.Comment("Underworld_HandleRoomTags#_01C2FD")
		a.JSL(fastRomBank | 0x01_C2FD)

		// check if submodule changed:
		a.LDA_dp(0x11)
		a.BEQ("no_submodule")

		a.Label("continue_submodule")
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | 0x00_80B5)

		a.Label("no_submodule")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)

		// fake NMI:
		//a.REP(0x30)
		//a.PHD()
		//a.PHB()
		//a.LDA_imm16_w(0)
		//a.TCD()
		//a.PHK()
		//a.PLB()
		//a.SEP(0x30)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates
		//a.PLB()
		//a.PLD()

		a.Comment("capture frame")
		a.WDM(0xFF)

		a.LDA_dp(0x11)
		a.BNE("continue_submodule")

		a.STZ_dp(0x11)
		a.STP()

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	{
		// emit into our custom $00:5300 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b00RunSingleFramePC&0xFFFF-0x5000:], true)
		a.SetBase(b00RunSingleFramePC)

		a.SEP(0x30)

		//a.Label("continue_submodule")
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | 0x00_80B5)

		a.Label("no_submodule")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)

		// fake NMI:
		//a.REP(0x30)
		//a.PHD()
		//a.PHB()
		//a.LDA_imm16_w(0)
		//a.TCD()
		//a.PHK()
		//a.PLB()
		//a.SEP(0x30)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates
		//a.PLB()
		//a.PLD()

		a.STP()

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	{
		// skip over music & sfx loading since we did not implement APU registers:
		a = newEmitterAt(e, fastRomBank|0x02_8293, true)
		//#_028293: JSR Underworld_LoadSongBankIfNeeded
		a.JMP_abs_imm16_w(0x82BC)
		//.exit
		//#_0282BC: SEP #$20
		//#_0282BE: RTL
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out RebuildHUD:
		a = newEmitterAt(e, fastRomBank|0x0D_FA88, true)
		//RebuildHUD_Keys:
		//	#_0DFA88: STA.l $7EF36F
		a.RTL()
		a.WriteTextTo(e.Logger)
	}

	//e.LoggerCPU = os.Stdout
	_ = loadSupertilePC

	// run the initialization code:
	//e.LoggerCPU = os.Stdout
	if err = e.ExecAt(0x00_5000, donePC); err != nil {
		panic(err)
	}
	//e.LoggerCPU = nil
	//os.Exit(0)

	return
}

func newEmitterAt(s *System, addr uint32, generateText bool) *asm.Emitter {
	lin, _ := lorom.BusAddressToPak(addr)
	a := asm.NewEmitter(s.ROM[lin:], generateText)
	a.SetBase(addr)
	return a
}

type empty = struct{}

type Entrance struct {
	EntranceID uint8
	Supertile

	EntryCoord MapCoord

	Rooms []*RoomState

	Supertiles     map[Supertile]*RoomState
	SupertilesLock sync.Mutex
}

func read16(b []byte, addr uint32) uint16 {
	return binary.LittleEndian.Uint16(b[addr : addr+2])
}

func read8(b []byte, addr uint32) uint8 {
	return b[addr]
}

func write8(b []byte, addr uint32, value uint8) {
	b[addr] = value
}

func write16(b []byte, addr uint32, value uint16) {
	binary.LittleEndian.PutUint16(b[addr:addr+2], value)
}

func write24(b []byte, addr uint32, value uint32) {
	binary.LittleEndian.PutUint16(b[addr:addr+2], uint16(value&0x00FFFF))
	b[addr+3] = byte(value >> 16)
}
