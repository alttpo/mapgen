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
	"image/gif"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const hackhackhack = false

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

var dungeonSupertiles = map[uint8][]uint16{
	0x00: {0x012, 0x002, 0x011, 0x021, 0x022, 0x032, 0x042, 0x041, 0x051, 0x061, 0x060, 0x050, 0x001, 0x072, 0x082, 0x081, 0x071, 0x070, 0x080, 0x052, 0x062},
	0x02: {0x060, 0x061, 0x051, 0x041, 0x042, 0x032, 0x022, 0x021, 0x011, 0x002, 0x012, 0x062, 0x052, 0x001, 0x072, 0x082, 0x081, 0x071, 0x070, 0x080, 0x050},
	0x04: {0x0c9, 0x0b9, 0x0b8, 0x0a8, 0x0a9, 0x089, 0x0aa, 0x0ba, 0x099, 0x0da, 0x0d9, 0x0d8, 0x0c8},
	0x06: {0x084, 0x085, 0x075, 0x074, 0x073, 0x083, 0x063, 0x053, 0x043, 0x033},
	0x08: {0x0e0, 0x0d0, 0x0c0, 0x0b0, 0x040, 0x030, 0x020},
	0x0a: {0x028, 0x038, 0x037, 0x036, 0x046, 0x035, 0x034, 0x054, 0x026, 0x076, 0x066, 0x016, 0x006},
	0x0c: {0x04a, 0x009, 0x03a, 0x00a, 0x02a, 0x02b, 0x03b, 0x04b, 0x01b, 0x00b, 0x01a, 0x06a, 0x05a, 0x019},
	0x0e: {0x098, 0x0d2, 0x0c2, 0x0b2, 0x0b3, 0x0c3, 0x0a3, 0x0a2, 0x093, 0x092, 0x091, 0x0a0, 0x090, 0x0a1, 0x0b1, 0x0c1, 0x0d1, 0x097},
	0x10: {0x056, 0x057, 0x067, 0x068, 0x058, 0x059, 0x049, 0x039, 0x029},
	0x12: {0x00e, 0x01e, 0x03e, 0x04e, 0x06e, 0x05e, 0x07e, 0x09e, 0x0be, 0x04f, 0x0bf, 0x0ce, 0x0de, 0x09f, 0x0af, 0x0ae, 0x08e, 0x07f, 0x05f, 0x03f, 0x01f, 0x02e},
	0x14: {0x077, 0x031, 0x027, 0x017, 0x007, 0x087, 0x0a7},
	0x16: {0x0db, 0x0dc, 0x0cc, 0x0cb, 0x0bc, 0x045, 0x044, 0x0ac, 0x0bb, 0x0ab, 0x064, 0x065},
	0x18: {0x023, 0x024, 0x014, 0x015, 0x0b6, 0x0c6, 0x0d6, 0x0c7, 0x0b7, 0x013, 0x004, 0x0b5, 0x0c5, 0x0d5, 0x0c4, 0x0b4, 0x0a4},
	0x1a: {0x00c, 0x08c, 0x01c, 0x09c, 0x09b, 0x07d, 0x07c, 0x07b, 0x09d, 0x08d, 0x08b, 0x06b, 0x05b, 0x05c, 0x05d, 0x06d, 0x06c, 0x0a5, 0x095, 0x096, 0x03d, 0x04d, 0x0a6, 0x04c, 0x01d, 0x00d},
	0xff: {0x104, 0x0f0, 0x0f1, 0x0f2, 0x0f3, 0x0f4, 0x0f5, 0x0e3, 0x0e2, 0x0f8, 0x0e8, 0x0fb, 0x0eb, 0x0fd, 0x0ed, 0x0fe, 0x0ee, 0x0ff, 0x0ef, 0x0df, 0x0f9, 0x0fa, 0x0ea, 0x0e1, 0x0e6, 0x0e7, 0x0e4, 0x0e5, 0x055, 0x010, 0x008, 0x018, 0x02f, 0x03c, 0x02c, 0x100, 0x11e, 0x101, 0x102, 0x117, 0x103, 0x105, 0x11f, 0x106, 0x107, 0x108, 0x109, 0x10a, 0x10b, 0x10c, 0x11b, 0x11c, 0x120, 0x110, 0x112, 0x111, 0x113, 0x114, 0x115, 0x10d, 0x10f, 0x119, 0x11d, 0x116, 0x121, 0x122, 0x118, 0x11a, 0x10e, 0x123, 0x124, 0x125, 0x126, 0x000, 0x003, 0x127},
}

var dungeonEntrances = map[uint8][]uint8{
	0x00: {0x02, 0x81},
	0x02: {0x03, 0x04, 0x05},
	0x04: {0x08},
	0x06: {0x09, 0x0a, 0x0b, 0x0c},
	0x08: {0x24},
	0x0a: {0x25},
	0x0c: {0x26},
	0x0e: {0x27},
	0x10: {0x28, 0x29, 0x2a, 0x2b, 0x76, 0x77, 0x78, 0x79},
	0x12: {0x2d},
	0x14: {0x33},
	0x16: {0x34},
	0x18: {0x15, 0x18, 0x19, 0x35},
	0x1a: {0x37},
	0xff: {0x00, 0x01, 0x06, 0x07, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x16, 0x17, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x2c, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x36, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f, 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f, 0x80, 0x82, 0x83, 0x84},
}

var fastRomBank uint32 = 0

var roomsWithUnreachableWarpPits map[Supertile]bool

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
	flag.IntVar(&enemyMovementFrames, "movementframes", 300, "render N frames in animated GIF of enemy movement after room load")
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

	// make data directory:
	{
		_, romFilename := filepath.Split(romPath)
		if i := strings.LastIndexByte(romFilename, '.'); i >= 0 {
			romFilename = romFilename[:i]
		}
		if hackhackhack {
			romFilename += "-HACK"
		} else {
			romFilename += "-data"
		}
		fmt.Printf("chdir `%s`\n", romFilename)
		_ = os.MkdirAll(romFilename, 0755)
		_ = os.Chdir(romFilename)
	}

	if err = e.InitEmulator(); err != nil {
		panic(err)
	}

	setupAlttp(&e)

	//RoomsWithPitDamage#_00990C [0x70]uint16
	roomsWithPitDamage = make(map[Supertile]bool, 0x128)
	for i := Supertile(0); i < 0x128; i++ {
		roomsWithPitDamage[i] = false
	}
	for i := uint32(0x00_990C); i <= uint32(0x00_997C); i += 2 {
		romaddr, _ := lorom.BusAddressToPak(i)
		st := Supertile(read16(e.ROM[:], romaddr))
		roomsWithPitDamage[st] = true
	}

	roomsWithUnreachableWarpPits = make(map[Supertile]bool, 0x128)
	roomsWithUnreachableWarpPits[Supertile(0x014)] = true
	roomsWithUnreachableWarpPits[Supertile(0x061)] = true
	roomsWithUnreachableWarpPits[Supertile(0x010)] = true
	roomsWithUnreachableWarpPits[Supertile(0x045)] = true

	const entranceCount = 0x85

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

	// new simplified reachability:
	if false {
		err = reachabilityAnalysis(&e)
		if err != nil {
			panic(err)
		}
	}

	// generate supertile animations:
	if true {
		wg := sync.WaitGroup{}
		wg.Add(0x128)

		jobs := make(chan func(), runtime.NumCPU())
		for i := 0; i < runtime.NumCPU(); i++ {
			go func() {
				for job := range jobs {
					job()
				}
			}()
		}

		for st16 := uint16(0); st16 < 0x128; st16++ {
			st16 := st16
			jobs <- func() {
				roomMovement(Supertile(st16), &e)
				wg.Done()
			}
		}

		wg.Wait()
	}

	// old floodfill analysis:
	if false {
		wg := sync.WaitGroup{}
		entranceGroups := make([]Entrance, entranceCount)
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
}

func roomMovement(st Supertile, initEmu *System) {
	var err error

	dungeonID := uint8(0xff)
	exists := false
findSupertile:
	for id, sts := range dungeonSupertiles {
		for _, s := range sts {
			if s == uint16(st) {
				dungeonID = id
				exists = true
				break findSupertile
			}
		}
	}
	if !exists {
		// skip unused supertiles:
		return
	}

	// get first entrance ID:
	eID := dungeonEntrances[dungeonID][0]

	room := &RoomState{
		Supertile: st,
		Entrance:  &Entrance{EntranceID: eID},
	}

	e := &room.e

	// have the emulator's WRAM refer to room.WRAM
	e.WRAM = &room.WRAM
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

	wram := e.WRAM[:]
	vram := e.VRAM[:]

	//e.LoggerCPU = e.Logger
	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID

	// load the entrance:
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}

	// load and draw selected supertile:
	write16(wram, 0xA0, uint16(st))
	write16(wram, 0x048E, uint16(st))
	if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
		panic(err)
	}
	//e.LoggerCPU = nil

	namePrefix := fmt.Sprintf("t%03x.d%02x", uint16(st), dungeonID)

	if enemyMovementFrames > 0 {
		isInteresting := true
		spriteDead := [16]bool{}
		spriteID := [16]uint8{}
		for j := 0; j < 16; j++ {
			spriteDead[j] = read8(wram, uint32(0x0DD0+j)) == 0
			spriteID[j] = read8(wram, uint32(0x0E20+j))
		}

		// run for N frames and render each frame into a GIF:
		sx, sy := room.Supertile.AbsTopLeft()
		fmt.Printf(
			"t%03x: abs=(%04x, %04x), bg1=(%04x,%04x), bg2=(%04x,%04x)\n",
			uint16(room.Supertile),
			sx,
			sy,
			read16(wram, 0xE0),
			read16(wram, 0xE6),
			read16(wram, 0xE2),
			read16(wram, 0xE8),
		)

		// place Link at the entrypoint:
		if true {
			// TODO: find a good place for Link to start
			//linkX, linkY := ep.Point.ToAbsCoord(st)
			//linkX, linkY := uint16(0x1000), uint16(0x1000)
			linkX, linkY := sx+8, sy+14
			//// nudge link within visible bounds:
			//if linkX&0x1FF < 0x20 {
			//	linkX += 0x20
			//}
			//if linkX&0x1FF > 0x1E0 {
			//	linkX -= 0x20
			//}
			//if linkY&0x1FF < 0x20 {
			//	linkY += 0x20
			//}
			//if linkY&0x1FF > 0x1E0 {
			//	linkY -= 0x20
			//}
			//linkY += 14
			write16(wram, 0x22, linkX)
			write16(wram, 0x20, linkY)
		}

		gifName := fmt.Sprintf("%s.move.gif", namePrefix)
		fmt.Printf("rendering %s\n", gifName)

		// first frame of enemy movement GIF:
		var lastFrame *image.Paletted
		{
			copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])

			pal, bg1p, bg2p, addColor, halfColor := room.RenderBGLayers()
			if false {
				if err := exportPNG(fmt.Sprintf("%03x.%02x.bg1.0.png", uint16(room.Supertile), room.Entrance.EntranceID), bg1p[0]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("%03x.%02x.bg1.1.png", uint16(room.Supertile), room.Entrance.EntranceID), bg1p[1]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("%03x.%02x.bg2.0.png", uint16(room.Supertile), room.Entrance.EntranceID), bg2p[0]); err != nil {
					panic(err)
				}
				if err := exportPNG(fmt.Sprintf("%03x.%02x.bg2.1.png", uint16(room.Supertile), room.Entrance.EntranceID), bg2p[1]); err != nil {
					panic(err)
				}
			}
			g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
			ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
			room.RenderSprites(g)

			// HACK:
			if hackhackhack {
				fmt.Println("oops all pipes!")
				for i := uint32(0); i < 16; i++ {
					et := read8(wram, 0x0E20+i)
					if et < 0xAE || st > 0xB1 {
						write8(wram, 0x0E20+i, uint8(i&3)+0xAE)
					}
				}
			}

			lastFrame = g
			room.EnemyMovementGIF.Image = append(room.EnemyMovementGIF.Image, g)
			room.EnemyMovementGIF.Delay = append(room.EnemyMovementGIF.Delay, 20)
			room.EnemyMovementGIF.Disposal = append(room.EnemyMovementGIF.Disposal, gif.DisposalNone)
			room.EnemyMovementGIF.LoopCount = 0
			room.EnemyMovementGIF.BackgroundIndex = 0
		}

	movement:
		for i := 0; i < enemyMovementFrames; i++ {
			//fmt.Println("FRAME")
			//e.LoggerCPU = os.Stdout
			// move camera to all four quadrants to get all enemies moving:
			// NEW: patched out sprite handling to disable off-screen check
			for j := 0; j < 1; j++ {
				// BG1H
				write16(wram, 0xE0, uint16(j&1)<<8+sx)
				// BG2H
				write16(wram, 0xE2, uint16(j&1)<<8+sx)
				// BG1V
				write16(wram, 0xE6, uint16(j&2)<<7+sy)
				// BG2V
				write16(wram, 0xE8, uint16(j&2)<<7+sy)

				if err := e.ExecAtUntil(b00RunSingleFramePC, 0, 0x200000); err != nil {
					fmt.Fprintln(os.Stderr, err)
					break movement
				}
				//e.LoggerCPU = nil

				// sanity check:
				if supertileWram := read16(wram, 0xA0); supertileWram != uint16(room.Supertile) {
					fmt.Fprintf(os.Stderr, "%s: supertile in wram does not match expected\n", namePrefix)
					break movement
				}

				// update tile sets after NMI; e.g. animated tiles:
				copy((&room.VRAMTileSet)[:], vram[0x4000:0x8000])
				//copy(tiles, wram[0x12000:0x14000])

				// check for any killed sprites:
				for j := 0; j < 16; j++ {
					if spriteDead[j] {
						continue
					}

					sDead := read8(wram, uint32(0x0DD0+j)) == 0
					if sDead {
						sID := read8(wram, uint32(0x0E20+j))
						fmt.Fprintf(os.Stderr, "%s: sprite %02x killed on frame %3d\n", namePrefix, sID, i)
						if sID == 0x9b {
							isInteresting = true
						}
						spriteDead[j] = true
					}
				}

				{
					pal, bg1p, bg2p, addColor, halfColor := room.RenderBGLayers()
					g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
					ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
					room.RenderSprites(g)

					delta := g
					dirty := false
					disposal := byte(0)
					if optimizeGIFs && room.EnemyMovementGIF.Image != nil {
						delta, dirty = generateDeltaFrame(lastFrame, g)
						//_ = exportPNG(fmt.Sprintf("%s.fr%03d.png", namePrefix, i), delta)
						disposal = gif.DisposalNone
					}

					if !dirty && room.EnemyMovementGIF.Image != nil {
						// just increment last frame's delay if nothing changed:
						room.EnemyMovementGIF.Delay[len(room.EnemyMovementGIF.Delay)-1] += 2
					} else {
						room.EnemyMovementGIF.Image = append(room.EnemyMovementGIF.Image, delta)
						room.EnemyMovementGIF.Delay = append(room.EnemyMovementGIF.Delay, 2)
						room.EnemyMovementGIF.Disposal = append(room.EnemyMovementGIF.Disposal, disposal)
					}
					lastFrame = g
				}
			}
		}

		fmt.Printf("rendered  %s\n", gifName)
		if isInteresting {
			RenderGIF(&room.EnemyMovementGIF, gifName)
			fmt.Printf("wrote     %s\n", gifName)
		}

		// reset WRAM:
		room.WRAM = room.WRAMAfterLoaded
	}

	return
}

type Hitbox struct {
	X int
	Y int
	W int
	H int
}

var hitbox [32]Hitbox

func setupAlttp(e *System) {
	var a *asm.Emitter
	var err error

	// pool Sprite_SetupHitBox
	//  .offset_x_low#_06F735
	// .offset_x_high#_06F755
	//         .width#_06F775
	//  .offset_y_low#_06F795
	// .offset_y_high#_06F7B5
	//        .height#_06F7D5
	for i := uint32(0); i < 32; i++ {
		hitbox[i].X = int(int8(e.Bus.Read8(0x06_F735 + i)))
		hitbox[i].Y = int(int8(e.Bus.Read8(0x06_F795 + i)))
		hitbox[i].W = int(int8(e.Bus.Read8(0x06_F775 + i)))
		hitbox[i].H = int(int8(e.Bus.Read8(0x06_F7D5 + i)))
	}

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
		a.Comment("JSR ClearOAMBuffer")
		// ClearOAMBuffer#_00841E
		a.JSR_abs(0x841E)
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

	{
		// patch out Sprite_PrepOAMCoord to not disable offscreen sprites.
		// Sprite_PrepOAMCoord_disable#_06E48B: INC.w $0F00,X  (INC,X = $FE)
		// to                                   STZ.w $0F00,X  (STZ,X = $9E)
		a = newEmitterAt(e, fastRomBank|0x06_E48B, true)
		a.STZ_abs_x(0x0F00)
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
