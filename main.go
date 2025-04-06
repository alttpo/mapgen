package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"roomloader/ptr"
	"roomloader/taskqueue"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/alttpo/snes"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
)

const hackhackhack = false

var (
	b02LoadUnderworldSupertilePC     uint32 = 0x02_5200
	b01LoadAndDrawRoomPC             uint32
	b01LoadAndDrawRoomSetSupertilePC uint32
	b00HandleRoomTagsPC              uint32 = 0x00_5300
	b00RunSingleFramePC              uint32 = 0x00_5400
	b02LoadOverworldTransitionPC     uint32 = 0x02_5500

	loadExitPC         uint32
	setExitSupertilePC uint32
	loadOverworldPC    uint32
	loadEntrancePC     uint32
	setEntranceIDPC    uint32
	loadSupertilePC    uint32
	runFramePC         uint32
	nmiRoutinePC       uint32
	donePC             uint32
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
	drawSpriteHitboxes       bool
	oopsAll                  int // sprite ID (0-255 valid), -1 = no replacement
	excludeSprites           []uint8
	useGammaRamp             bool
	drawBG1p0                bool
	drawBG1p1                bool
	drawBG2p0                bool
	drawBG2p1                bool
	optimizeGIFs             bool
	staticEntranceMap        bool
)

var fastRomBank uint32 = 0

var roomsWithUnreachableWarpPits map[Supertile]bool

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("main: recovered from error: %v\n%s\n", err, debug.Stack())
			return
		}
	}()

	nWorkers := -1
	entranceStr := ""
	entranceBadListStr := ""
	entranceMinStr, entranceMaxStr := "", ""
	oopsAllStr, excludeSpritesStr := "", ""
	roomListStr := ""
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
	flag.BoolVar(&drawSpriteHitboxes, "hitboxes", false, "draw sprite hitboxes on eg1/eg2")
	flag.StringVar(&oopsAllStr, "oopsall", "", "replace all sprites with this ID (hex)")
	flag.StringVar(&excludeSpritesStr, "excludesprites", "", "exclude sprite IDs (comma delimited list of hex values) from -oopsall replacement")
	flag.BoolVar(&useGammaRamp, "gamma", false, "use bsnes gamma ramp")
	flag.BoolVar(&supertileGifs, "gifs", false, "render room GIFs")
	flag.BoolVar(&animateRoomDrawing, "animate", false, "render animated room drawing GIFs")
	flag.IntVar(&animateRoomDrawingDelay, "animdelay", 15, "room drawing GIF frame delay")
	flag.IntVar(&enemyMovementFrames, "movementframes", 0, "render N frames in animated GIF of enemy movement after room load")
	flag.StringVar(&roomListStr, "rooms", "", "list of room numbers (hex), comma delimited, ranges with x..y permitted")
	flag.StringVar(&entranceStr, "ent", "", "single entrance ID (hex)")
	flag.StringVar(&entranceBadListStr, "entsbad", "", "bad entrance IDs to exclude (hex, comma-delimited)")
	flag.StringVar(&entranceMinStr, "entmin", "0", "entrance ID range minimum (hex)")
	flag.StringVar(&entranceMaxStr, "entmax", "84", "entrance ID range maximum (hex)")
	flag.BoolVar(&staticEntranceMap, "static", false, "use static entrance->supertile map from JP 1.0")
	flag.IntVar(&nWorkers, "n", -1, "number of parallel workers (-1 = CPU count)")
	flag.Parse()

	if nWorkers <= 0 {
		nWorkers = runtime.NumCPU()
	}

	excludeSprites = []uint8{
		0x04,
		0x05,
		0x06,
		0x07,
		0x09,
		0x14,
		0x16,
		0x1A,
		0x1D,
		0x1E,
		0x1F,
		0x21,
		0x25,
		0x28,
		0x29,
		0x2A,
		0x2B,
		0x2C,
		0x2D,
		0x2E,
		0x2F,
		0x30,
		0x31,
		0x32,
		0x33,
		0x34,
		0x35,
		0x36,
		0x37,
		0x39,
		0x3A,
		0x3B,
		0x3C,
		0x3D,
		0x3E,
		0x3F,
		0x40,
		0x52,
		0x53,
		0x54,
		0x62,
		0x6C,
		0x72,
		0x73,
		0x74,
		0x75,
		0x76,
		0x78,
		0x7A,
		0x7B,
		0x88,
		0x89,
		0x8C,
		0x8D,
		0xA2,
		0xA3,
		0xA4,
		0xAB,
		0xAC,
		0xAD,
		0xAE,
		0xAF,
		0xB0,
		0xB1,
		0xB2,
		0xB3,
		0xB4,
		0xB5,
		0xB6,
		0xB7,
		0xB8,
		0xB9,
		0xBA,
		0xBB,
		0xBC,
		0xBD,
		0xBE,
		0xBF,
		0xC0,
		0xC1,
		0xC2,
		0xC8,
		0xCB,
		0xCC,
		0xCD,
		0xCE,
		0xD5,
		0xD6,
		0xD7,
		0xD8,
		0xD9,
		0xDA,
		0xDB,
		0xDC,
		0xDD,
		0xDE,
		0xDF,
		0xE0,
		0xE1,
		0xE2,
		0xE4,
		0xE5,
		0xE6,
		0xE7,
		0xE8, // ??
		0xE9,
		0xEA,
		0xEB,
		0xEC,
		0xED,
		0xEE,
		0xF2,
	}

	oopsAll = -1
	if oopsAllStr != "" {
		if id, err := strconv.ParseInt(oopsAllStr, 16, 8); err == nil {
			oopsAll = int(id)
		}
	}

	// parse exclusion list:
	if excludeSpritesStr != "" {
		strs := strings.Split(excludeSpritesStr, ",")
		list := make([]string, 0, 10)
		for _, s := range strs {
			// parse hex values:
			if id, err := strconv.ParseInt(s, 16, 64); err == nil {
				excludeSprites = append(excludeSprites, uint8(id))
				list = append(list, strconv.FormatInt(id, 16))
			}
		}
		fmt.Printf("excluding sprite ids: [%s]", strings.Join(list, ","))
	}

	roomList := make([]uint16, 0, 0x127)
	if roomListStr != "" {
		for roomListStr != "" {
			exprStr, remainder, found := strings.Cut(roomListStr, ",")
			if !found {
				exprStr = roomListStr
			}
			roomListStr = remainder

			// check if it's a range:
			if rangeStartStr, rangeEndStr, hasRange := strings.Cut(exprStr, ".."); hasRange {
				rs, _ := strconv.ParseUint(rangeStartStr, 16, 16)
				re, _ := strconv.ParseUint(rangeEndStr, 16, 16)
				for i := uint16(rs); i <= uint16(re); i++ {
					roomList = append(roomList, i)
				}
			} else {
				// single number:
				r, _ := strconv.ParseUint(exprStr, 16, 16)
				roomList = append(roomList, uint16(r))
			}
		}
	} else {
		for i := uint16(0); i <= 0x127; i++ {
			roomList = append(roomList, i)
		}
	}
	fmt.Printf("%+v\n", roomList)

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

		if h.DestinationCode == snes.RegionJapan {
			// JP 1.0:
			alttp = alttpJP10
			fmt.Printf("Detected JP ROM\n")
		} else if h.DestinationCode == snes.RegionNorthAmerica {
			// US:
			alttp = alttpUS
			fmt.Printf("Detected US ROM\n")
		}
	}

	if err = e.InitEmulator(); err != nil {
		panic(err)
	}

	// override "constants" with extractable pointers from code:
	alttp.ExtractPointers(&alttp, &e)

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

	// debugging WRAM reads and writes:
	if true {
		//e.OnPC = make(map[uint32]func())
		//firstRead := make(map[uint32]uint32)
		//firstWrite := make(map[uint32]uint32)
		if true {
			//e.ReadWRAM = func(offs uint32) uint8 {
			//	//if offs == 0x84 || offs == 0x85 {
			//	//	fmt.Printf("$%04X read    -> $%02X @ #_%06X\n", offs, e.WRAM[offs], e.GetPC())
			//	//}
			//
			//	if (offs >= 0x10 && offs <= 0x136) || (offs >= 0x0200 && offs < 0x2000) {
			//		if _, ok := firstRead[offs]; !ok {
			//			firstRead[offs] = e.GetPC()
			//			fmt.Printf("$%04X read    -> $%02X @ #_%06X\n", offs, e.WRAM[offs], e.GetPC())
			//		}
			//	}
			//	return e.DefaultReadWRAM(offs)
			//}
			e.WriteWRAM = func(offs uint32, value uint8) {
				if offs == 0x84 || offs == 0x85 {
					fmt.Printf(
						"$%04X <- $%02X @ #_%06X; [E2,E8] = {%04X, %04X}, [E0,E6] = {%04X, %04X}, [22,20] = {%04X, %04X}, [1A] = %02X\n",
						offs,
						value,
						e.GetPC(),
						e.Bus.Read16(0xE2),
						e.Bus.Read16(0xE8),
						e.Bus.Read16(0xE0),
						e.Bus.Read16(0xE6),
						e.Bus.Read16(0x22),
						e.Bus.Read16(0x20),
						e.Bus.Read8(0x1A),
					)
				}

				//if (offs >= 0x10 && offs <= 0x136) || (offs >= 0x0200 && offs < 0x2000) {
				//	if _, ok := firstWrite[offs]; !ok {
				//		firstWrite[offs] = e.GetPC()
				//		fmt.Printf("$%04X written <- $%02X @ #_%06X\n", offs, value, e.GetPC())
				//	}
				//}
				e.DefaultWriteWRAM(offs, value)
			}
			//e.OnPC[0x0085FC] = func() {
			//	clear(firstRead)
			//	clear(firstWrite)
			//}
		}
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
	roomsWithUnreachableWarpPits[Supertile(0x089)] = true
	roomsWithUnreachableWarpPits[Supertile(0x065)] = true

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

	// override range with single entrance ID:
	if entranceStr != "" {
		var v uint64
		v, err = strconv.ParseUint(entranceStr, 16, 8)
		if err == nil {
			entranceMin, entranceMax = uint8(v), uint8(v)
		}
	}

	// exclude entrance IDs:
	excludeEntrances := map[uint8]bool{}
	if entranceBadListStr != "" {
		strs := strings.Split(entranceBadListStr, ",")
		list := make([]string, 0, 10)
		for _, s := range strs {
			// parse hex values:
			if id, err := strconv.ParseInt(s, 16, 64); err == nil {
				excludeEntrances[uint8(id)] = true
				list = append(list, strconv.FormatInt(id, 16))
			}
		}
		fmt.Printf("excluding entrance ids: [%s]", strings.Join(list, ","))
	}

	// validate entrance range:
	if entranceMax < entranceMin {
		entranceMin, entranceMax = entranceMax, entranceMin
	}

	if entranceMin > entranceCount-1 {
		entranceMin = entranceCount - 1
	}
	if entranceMax > entranceCount-1 {
		entranceMax = entranceCount - 1
	}

	// run jobs starting from entrances:
	if true {
		roomsMap := make(map[Supertile]*RoomState, 0x128)
		roomsLock := sync.Mutex{}
		areasMap := make(map[AreaID]*Area, 0x80)
		areasLock := sync.Mutex{}

		q := taskqueue.NewQ[*ReachTask](nWorkers, 0x2000)

		wram := (*e.WRAM)[:]

		// activate all overworld overlays to open up entrances:
		for _, aid := range []uint8{
			// light world:
			0x02, 0x07,
			0x13, 0x14,
			0x18, 0x1B, 0x1E,
			0x3B,
			// dark world:
			0x40, 0x43, 0x45, 0x47,
			0x58, 0x5B, 0x5E,
			0x62,
			0x70,
			0x7B,
		} {
			e.WRAM[0xF280+uint32(aid)] |= 0x20
		}

		if false {
			// eIDmin, eIDmax := uint8(0), uint8(0x84)
			for eID := entranceMin; eID <= entranceMax; eID++ {
				if excludeEntrances[eID] {
					fmt.Printf("entrance $%02X skip\n", eID)
					continue
				}

				// skip attract mode cinematic entrances (vanilla only??):
				if eID >= 0x73 && eID <= 0x75 {
					fmt.Printf("entrance $%02X skip (assuming only used for intro/attract sequence)\n", eID)
					continue
				}

				q.SubmitTask(
					&ReachTask{
						EntranceID:      eID,
						InitialEmulator: &e,
						Rooms:           roomsMap,
						RoomsLock:       &roomsLock,
						Areas:           areasMap,
						AreasLock:       &areasLock,
					},
					ReachTaskFromEntranceWorker,
				)
			}
		}

		{
			// load module 5 to get game started:
			write8(wram, 0x10, 0x05)
			write8(wram, 0x11, 0x00)
			write8(wram, 0xB0, 0x00)
			// act like a respawn so we don't get a menu prompt:
			write8(wram, 0x010A, 0x01)
			write8(wram, 0x04AA, 0x01)

			f := 0

			// run frames until we get to underworld:
			for i := 0; i < 240; i++ {
				if err = e.ExecAt(runFramePC, donePC); err != nil {
					panic(err)
				}

				m, sm := read8(wram, 0x10), read8(wram, 0x11)
				if m == 0x07 && sm == 0x00 {
					break
				} else if m == 0x1B && sm != 0 {
					// this should exit a choice menu:
					write8(wram, 0x11, 0)
					// choice 0, Link's house:
					write8(wram, 0x1CE8, 0)
				}
			}

			// wait until link wakes up:
			for i := 0; i < 8; i++ {
				if err = e.ExecAt(runFramePC, donePC); err != nil {
					panic(err)
				}

				if read8(wram, 0x5D) == 0 {
					break
				}
			}

			if m, sm := read8(wram, 0x10), read8(wram, 0x11); m != 0x07 || sm != 0x00 {
				panic(fmt.Sprintf("did not reach module 07,00; got %02X,%02X", m, sm))
			}

			// for debugging OW transition from 2C to 1B:
			if true {
				var d deltaGifEmitter
				var bg deltaGifEmitter
				d.doNotOptimize = true
				bg.doNotOptimize = true

				f = 0

				steps := []struct {
					I       uint16
					F       int
					MandSeq *[2]uint8
					Sne     *uint8
				}{
					//// walk south until we exit Link's house:
					//{I: DirSouth.ToControllerInput(), F: 512, MandSeq: ptr.Of([2]uint8{9, 0})},
					//// now walk south a tiny bit:
					//{I: DirSouth.ToControllerInput(), F: 30},
					//// east past the bushes and hop off the ledge:
					//{I: DirEast.ToControllerInput(), F: 96},
					//// north until we cross the border:
					//{I: DirNorth.ToControllerInput(), F: 292, Sne: ptr.Of(uint8(0))},
					//// no input:
					//{I: 0, F: 128},

					// walk south until we exit Link's house:
					{I: DirSouth.ToControllerInput(), F: 512, MandSeq: ptr.Of([2]uint8{9, 0})},
					// now walk south a tiny bit:
					{I: DirSouth.ToControllerInput(), F: 120},
					// east past the bushes and hop off the ledge:
					{I: DirEast.ToControllerInput(), F: 16},
					// south until we cross the border:
					{I: DirSouth.ToControllerInput(), F: 120, Sne: ptr.Of(uint8(0))},
					// no input:
					{I: 0, F: 128},
				}

				for i := 0; i < len(steps); i++ {
					step := &steps[i]

					e.HWIO.ControllerInput[0] = step.I
					fmt.Printf("Step %d; I = %016b\n", i, step.I)
					for n := 0; n < step.F; n++ {
						fmt.Printf("FRAME %03d\n----------------------------------------------------------------\n", f)
						if err = e.ExecAt(runFramePC, donePC); err != nil {
							panic(err)
						}

						d.EmitEmulatedFrame(&e)
						g := renderVRAMFullBG(nil, &e)
						bg.EmitFrame(g)

						//os.WriteFile(
						//	fmt.Sprintf("debug.2Cto1B.%03d.wram", f),
						//	e.WRAM[:],
						//	0666,
						//)

						f++

						// exit conditions:
						if step.MandSeq != nil {
							if read8(wram, 0x10) == (*step.MandSeq)[0] {
								if read8(wram, 0x11) == (*step.MandSeq)[1] {
									break
								}
							}
						}
						if step.Sne != nil {
							if read8(wram, 0x11) != *step.Sne {
								break
							}
						}
					}
				}

				d.RenderGIF(fmt.Sprintf("debug.2Cto1B.gif"))
				bg.RenderGIF(fmt.Sprintf("debug.2Cto1B.bg2.gif"))

				return
			} else {
				var d deltaGifEmitter
				var bg deltaGifEmitter
				d.doNotOptimize = true
				bg.doNotOptimize = true

				// walk south to exit Link's house:
				e.HWIO.ControllerInput[0] = DirSouth.ToControllerInput()
				//e.Logger = os.Stderr
				for i := 0; i < 512; i++ {
					fmt.Printf("FRAME %03d\n----------------------------------------------------------------\n", f)
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}

					d.EmitEmulatedFrame(&e)
					g := renderVRAMFullBG(nil, &e)
					bg.EmitFrame(g)
					f++

					if read8(wram, 0x10) == 0x09 {
						if read8(wram, 0x11) == 0x00 {
							break
						}
					}
				}

				//$0084 <- $D6 @ #_02F06A; [E2,E8] = {08BA, 0A00}, [E0,E6] = {0876, 0A4D}, [22,20] = {0940, 0A03}, [1A] = FC
				//$0085 <- $1F @ #_02F06A; [E2,E8] = {08BA, 0A00}, [E0,E6] = {0876, 0A4D}, [22,20] = {0940, 0A03}, [1A] = FC
				e.Bus.Write16(0x84, 0x1FD6)
				e.Bus.Write16(0x86, 0x0003)

				e.Bus.Write16(0xE2, 0x08BA)
				e.Bus.Write16(0xE8, 0x0A00)

				e.Bus.Write16(0x22, 0x0940)
				e.Bus.Write16(0x20, 0x0A03)

				// north until transition:
				e.HWIO.ControllerInput[0] = DirNorth.ToControllerInput()
				for i := 0; i < 292; i++ {
					fmt.Printf("FRAME %03d\n----------------------------------------------------------------\n", f)
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}

					d.EmitEmulatedFrame(&e)
					g := renderVRAMFullBG(nil, &e)
					bg.EmitFrame(g)
					f++

					if read8(wram, 0x10) != 0x09 {
						break
					}
					if read8(wram, 0x11) != 0x00 {
						break
					}
				}

				// transition:
				e.HWIO.ControllerInput[0] = 0
				for i := 0; i < 128; i++ {
					fmt.Printf("FRAME %03d\n----------------------------------------------------------------\n", f)
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}

					d.EmitEmulatedFrame(&e)
					g := renderVRAMFullBG(nil, &e)
					bg.EmitFrame(g)
					f++
				}

				d.RenderGIF(fmt.Sprintf("north.2Cto1B.gif"))
				bg.RenderGIF(fmt.Sprintf("north.2Cto1B.bg2.gif"))
				return
			}

			{
				q.SubmitTask(
					&ReachTask{
						Mode:            ModeUnderworld,
						Rooms:           roomsMap,
						RoomsLock:       &roomsLock,
						Areas:           areasMap,
						AreasLock:       &areasLock,
						InitialEmulator: &e,
					},
					ReachTaskRoomFromCurrentStateWorker,
				)
			}
		}

		fmt.Println("wait")
		q.Wait()

		// Load all LW flute transport locations:
		fmt.Println("flute")
		*e.WRAM = areasMap[0x2C].WRAMAfterLoaded
		*e.VRAM = *areasMap[0x2C].e.VRAM
		for i := 0; i < 60; i++ {
			if err = e.ExecAt(runFramePC, donePC); err != nil {
				panic(err)
			}

			m, sm := read8(wram, 0x10), read8(wram, 0x11)
			if m == 0x09 && sm == 0x00 {
				break
			} else if m == 0x1B && sm != 0 {
				// this should exit a choice menu:
				write8(wram, 0x11, 0)
				// choice 0, Link's house:
				write8(wram, 0x1CE8, 0)
			}
		}

		for i := 0; i < 8; i++ {
			q.SubmitTask(
				&ReachTask{
					Mode:            ModeOverworld,
					Rooms:           roomsMap,
					RoomsLock:       &roomsLock,
					Areas:           areasMap,
					AreasLock:       &areasLock,
					InitialEmulator: &e,
					Transport:       uint8(i),
				},
				ReachTaskOverworldTransportWorker,
			)
		}
		fmt.Println("wait")
		q.Wait()

		for runs := 0; runs < 3; runs++ {
			// after the initial round of scans is complete, go back through the overworld and find previously
			// unreachable DW areas with a reachable counterpart in the same LW area (for mirroring):
			for dwaid, dwa := range areasMap {
				if dwaid&0x40 == 0 {
					continue
				}

				// find LW counterpart:
				lwa, ok := areasMap[dwaid&0x3F]
				if !ok {
					continue
				}

				// scan for mirrorable areas:
				mirrors := make([]OWCoord, 0, 0x100)
				for row := 0; row < dwa.Height; row++ {
					for col := 0; col < dwa.Width; col++ {
						c := RowColToOWCoord(row, col)
						// is DW tile reachable?
						if dwa.Reachable[c] != 0x01 && dwa.Reachable[c] == dwa.Tiles[c] {
							// is LW tile not reachable but could be?
							if lwa.Reachable[c] == 0x01 && lwa.isAlwaysWalkable(lwa.Tiles[c]) {
								// LW tile is unreachable; we have a mirror entry point from DW to LW:
								mirrors = append(mirrors, c)
							}
						}
					}
				}

				if len(mirrors) > 0 {
					q.SubmitTask(
						&ReachTask{
							Mode:      ModeOverworld,
							Rooms:     roomsMap,
							RoomsLock: &roomsLock,
							Areas:     areasMap,
							AreasLock: &areasLock,
							AreaID:    lwa.AreaID,
							OWWarps:   mirrors,
						},
						ReachTaskOverworldMirroredWorker,
					)
				}
			}
			q.Wait()
		}

		fmt.Println("close")
		q.Close()

		// areas := make([]*Area, 0, 0x80)
		for _, a := range areasMap {
			// a.Render()
			a.DrawOverlays()
			exportPNG(fmt.Sprintf("ow%02X.png", uint8(a.AreaID)), a.RenderedNRGBA)
		}

		rooms := make([]*RoomState, 0, 0x128)
		for _, room := range roomsMap {
			rooms = append(rooms, room)
		}

		// condense all maps into big atlas images:
		if true {
			func() {
				black := image.NewUniform(color.RGBA{0, 0, 0, 255})
				white := image.NewUniform(color.RGBA{255, 255, 255, 255})

				// entire overworld is 512x512 8px tiles:
				ow := [2]*image.NRGBA{
					image.NewNRGBA(image.Rect(0, 0, 4096, 4096)),
					image.NewNRGBA(image.Rect(0, 0, 4096, 4096)),
				}

				for aid, a := range areasMap {
					row, col := aid.RowCol()
					g := ow[(a.AreaID&0x40)>>6]
					draw.Draw(
						g,
						image.Rect(
							col*0x40*8,
							row*0x40*8,
							(col*0x40+a.Width)*8,
							(row*0x40+a.Height)*8,
						),
						a.RenderedNRGBA,
						image.Point{},
						draw.Over,
					)

					// draw area number in top-left:
					stStr := fmt.Sprintf("%02X", uint8(aid))
					arow, acol := aid.RowCol()
					stx := acol * 0x40 * 8
					sty := arow * 0x40 * 8
					(&font.Drawer{
						Dst:  g,
						Src:  black,
						Face: inconsolata.Bold8x16,
						Dot: fixed.Point26_6{
							X: fixed.I(stx + 5),
							Y: fixed.I(sty + 5 + 12),
						},
					}).DrawString(stStr)
					(&font.Drawer{
						Dst:  g,
						Src:  white,
						Face: inconsolata.Bold8x16,
						Dot: fixed.Point26_6{
							X: fixed.I(stx + 4),
							Y: fixed.I(sty + 4 + 12),
						},
					}).DrawString(stStr)
				}
				exportPNG("ow-lw.png", ow[0])
				exportPNG("ow-dw.png", ow[1])
				// TODO: special overworld
			}()
		}
		if drawEG1 {
			renderAll("eg1", rooms, 0x00, 0x10)
		}
		if drawEG2 {
			renderAll("eg2", rooms, 0x10, 0x3)
		}
		fmt.Println("done")
		return
	}

	// run generic processing jobs on all supertiles:
	if false {
		n := runtime.NumCPU()
		//n := 1

		jobs := make(chan func(), n)
		wg := sync.WaitGroup{}

		submitJob := func(job func()) {
			wg.Add(1)
			jobs <- func() {
				job()
				wg.Done()
			}
		}

		for i := 0; i < n; i++ {
			go func() {
				defer func() {
					if err := recover(); err != nil {
						fmt.Println(err)
						return
					}
				}()

				for job := range jobs {
					job()
				}
			}()
		}

		// st16min, st16max := uint16(0), uint16(0x127)
		// st16min, st16max := uint16(0x06b), uint16(0x06b)
		//st16min, st16max := uint16(0x004), uint16(0x004)
		//st16min, st16max := uint16(0x57), uint16(0x57)
		//st16min, st16max := uint16(0x12), uint16(0x12)

		// generate supertile animations:
		roomFn := roomFindReachablePitsFromEnemies
		// roomFn := renderEnemyMovementGif
		// roomFn := renderSupertile

		rooms := make([]*RoomState, 0, 0x128)
		for _, st16 := range roomList {
			st := Supertile(st16)

			var eID uint8
			if ste, ok := supertileEntrances[uint16(st)]; !ok {
				// unused supertile:
				continue
			} else {
				eID = ste[0]
			}

			room := &RoomState{
				Supertile: st,
				Entrance:  &Entrance{EntranceID: eID},
			}
			rooms = append(rooms, room)

			submitJob(func() {
				defer func() {
					if err := recover(); err != nil {
						fmt.Println(err)
						return
					}
				}()

				fmt.Printf("process room %s\n", room.Supertile)
				processRoom(room, &e, roomFn)
			})
		}

		wg.Wait()

		dbg := strings.Builder{}
		for _, room := range rooms {
			if room.HasReachablePit {
				fmt.Fprintf(&dbg, ",%s", room.Supertile)
			}
		}
		fmt.Printf("rooms with enemy-reachable pits: %s\n", dbg.String()[1:])

		// condense all maps into big atlas images:
		if drawEG1 {
			wg.Add(1)
			go func() {
				renderAll("eg1", rooms, 0x00, 0x10)
				wg.Done()
			}()
		}
		if drawEG2 {
			wg.Add(1)
			go func() {
				renderAll("eg2", rooms, 0x10, 0x3)
				wg.Done()
			}()
		}
		if drawEG1 || drawEG2 {
			wg.Wait()
		}
		return
	}

	// old simplified reachability:
	if false {
		err = reachabilityAnalysis(&e)
		if err != nil {
			panic(err)
		}
	}

	// old bad floodfill analysis:
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

		// TODO convert the above to append to a []*RoomState instead of []Entrance
		//// condense all maps into big atlas images:
		//if drawEG1 {
		//	wg.Add(1)
		//	go func() {
		//		renderAll("eg1", entranceGroups, 0x00, 0x10)
		//		wg.Done()
		//	}()
		//}
		//if drawEG2 {
		//	wg.Add(1)
		//	go func() {
		//		renderAll("eg2", entranceGroups, 0x10, 0x3)
		//		wg.Done()
		//	}()
		//}
		//if drawEG1 || drawEG2 {
		//	wg.Wait()
		//}

		var rooms []*RoomState
		if drawEG1 || drawEG2 {
			rooms = make([]*RoomState, 0, 0x128)
			for i := range entranceGroups {
				g := &entranceGroups[i]
				rooms = append(rooms, g.Rooms...)
			}
		}

		// condense all maps into big atlas images:
		if drawEG1 {
			wg.Add(1)
			go func() {
				renderAll("eg1", rooms, 0x00, 0x10)
				wg.Done()
			}()
		}
		if drawEG2 {
			wg.Add(1)
			go func() {
				renderAll("eg2", rooms, 0x10, 0x3)
				wg.Done()
			}()
		}
		if drawEG1 || drawEG2 {
			wg.Wait()
		}
	}

	fmt.Println("main exit")
}

func processRoom(room *RoomState, initEmu *System, roomFn func(room *RoomState)) {
	var err error

	eID := room.Entrance.EntranceID
	e := room.e

	// have the emulator's WRAM refer to room.WRAM
	e.WRAM = &room.WRAM
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

	wram := e.WRAM[:]

	//e.LoggerCPU = e.Logger
	// poke the entrance ID into our asm code:
	e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID

	// load the entrance:
	if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
		panic(err)
	}

	// load and draw selected supertile:
	write16(wram, 0xA0, uint16(room.Supertile))
	write16(wram, 0x048E, uint16(room.Supertile))
	//e.Logger = os.Stdout
	//e.LoggerCPU = os.Stdout
	if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
		panic(err)
	}
	room.WRAMAfterLoaded = room.WRAM

	copy((&room.VRAMTileSet)[:], e.VRAM[0x4000:0x8000])

	roomFn(room)
}

func renderEnemyMovementGif(room *RoomState) {
	if enemyMovementFrames <= 0 {
		return
	}

	e := room.e
	st := room.Supertile
	wram := e.WRAM[:]
	vram := e.VRAM[:]

	dungeonID := read8(wram, 0x040C)

	namePrefix := fmt.Sprintf("t%03x.d%02x", uint16(st), dungeonID)
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
		// room.RenderSprites(g)

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

		// lastFrame = g
		// room.EnemyMovementGIF.Image = append(room.EnemyMovementGIF.Image, g)
		// room.EnemyMovementGIF.Delay = append(room.EnemyMovementGIF.Delay, 20)
		// room.EnemyMovementGIF.Disposal = append(room.EnemyMovementGIF.Disposal, gif.DisposalNone)
		room.EnemyMovementGIF.LoopCount = 0
		room.EnemyMovementGIF.BackgroundIndex = 0
	}

	roomX, roomY := room.Supertile.AbsTopLeft()

	var pal color.Palette
	var bg1p, bg2p [2]*image.Paletted
	var obj [4]*image.Paletted

movement:
	for i := 0; i < enemyMovementFrames; i++ {
		frameWram := *e.WRAM

		g := image.NewPaletted(image.Rect(0, 0, 512, 512), nil)
		for j := 0; j < 4; j++ {
			obj[j] = image.NewPaletted(image.Rect(0, 0, 512, 512), nil)
		}
		var ppu PPURegs

		//fmt.Println("FRAME")
		//e.LoggerCPU = os.Stdout
		// move camera to all four quadrants to get all enemies moving:
		// NEW: patched out sprite handling to disable off-screen check
		for j := 0; j < 4; j++ {
			bgX := uint16(j&1)<<8 + sx
			bgY := uint16(j&2)<<7 + sy + uint16((j&2)^2)<<3
			// fmt.Printf("%d) %04X, %04X\n", j, bgX, bgY)

			// reset WRAM to frame state:
			*e.WRAM = frameWram

			// BG1H
			write16(wram, 0xE0, bgX)
			// BG2H
			write16(wram, 0xE2, bgX)
			// BG1V
			write16(wram, 0xE6, bgY)
			// BG2V
			write16(wram, 0xE8, bgY)

			// make Link invisible:
			write8(wram, 0x4B, 0x0C)
			// make Link invincible:
			write8(wram, 0x037B, 0x01)
			// normal Link state:
			write8(wram, 0x5D, 0x00)
			// put Link in center of room:
			write16(wram, 0x22, sx+0x100)
			write16(wram, 0x20, sy+0x100)
			write16(wram, 0x24, 0xFFFF)

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

			// render sprites:
			{
				// fmt.Printf("%v: %02X\n", room.Supertile, e.WRAM[0x1A])

				// drawOutlineBox(
				// 	g,
				// 	&image.Uniform{color.White},
				// 	int(bgX)-int(roomX),
				// 	int(bgY)-int(roomY),
				// 	256,
				// 	240)

				if true {
					renderOAMSpritesPrioritizedPalettedFromWRAM(obj, e, int(bgX), int(bgY), int(roomX), int(roomY))
				} else {
					renderSpriteLabels(g, e.WRAM[:], st)
				}
			}
		}

		{
			pal, bg1p, bg2p, _, _ = room.RenderBGLayers()

			g.Palette = pal
			obj[0].Palette = pal
			obj[1].Palette = pal
			obj[2].Palette = pal
			obj[3].Palette = pal

			// capture PPU regs to be written:
			if false {
				// from PPU $21xx writes: (doesn't work without NMI handler executing)
				ppu = PPURegs{
					TM:       e.HWIO.PPU.Regs[0x2C],
					TS:       e.HWIO.PPU.Regs[0x2D],
					CGWSEL:   e.HWIO.PPU.Regs[0x30],
					CGADDSUB: e.HWIO.PPU.Regs[0x31],
				}
			} else {
				// from WRAM:
				ppu = PPURegs{
					TM:       e.WRAM[0x1C],
					TS:       e.WRAM[0x1D],
					CGWSEL:   e.WRAM[0x99],
					CGADDSUB: e.WRAM[0x9A],
				}
			}

			if i == 0 && ppu.UsesColorMath() {
				fmt.Printf(
					"%v: uses colormath, CGADDSUB=%08b, CGWSEL=%08b, TM=%08b, TS=%08b\n",
					room.Supertile,
					ppu.CGADDSUB,
					ppu.CGWSEL,
					ppu.TM,
					ppu.TS,
				)
			}
		}

		// compose all layers to single image:
		// fmt.Printf("%v: TM=%08b,TS=%08b,WS=%08b,AS=%08b\n", room.Supertile, ppu.TM, ppu.TS, ppu.CGWSEL, ppu.CGADDSUB)
		ComposePrioritizedToPaletted(g, pal, bg1p, bg2p, obj, ppu)

		// render frame to gif:
		{
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

	fmt.Printf("rendered  %s\n", gifName)
	if isInteresting {
		RenderGIF(&room.EnemyMovementGIF, gifName)
		fmt.Printf("wrote     %s\n", gifName)
	}

	// reset WRAM:
	room.WRAM = room.WRAMAfterLoaded
}

func renderSpriteLabels(g draw.Image, wram []byte, st Supertile) {
	//black := image.NewUniform(color.RGBA{0, 0, 0, 255})
	yellow := image.NewUniform(color.RGBA{255, 255, 0, 255})
	red := image.NewUniform(color.RGBA{255, 48, 48, 255})

	// draw sprites:
	for i := uint32(0); i < 16; i++ {
		clr := yellow

		// Initial AI state on load:
		//initialAIState := read8(room.WRAMAfterLoaded[:], 0x0DD0+i)
		//if initialAIState == 0 {
		//	// nothing was ever here:
		//	continue
		//}

		// determine if in bounds:
		yl, yh := read8(wram, 0x0D00+i), read8(wram, 0x0D20+i)
		xl, xh := read8(wram, 0x0D10+i), read8(wram, 0x0D30+i)
		y := uint16(yl) | uint16(yh)<<8
		x := uint16(xl) | uint16(xh)<<8
		if !st.IsAbsInBounds(x, y) {
			continue
		}

		// AI state:
		st := read8(wram, 0x0DD0+i)
		// enemy type:
		et := read8(wram, 0x0E20+i)

		var lx, ly int
		if true {
			lx = int(x) & 0x1FF
			ly = int(y) & 0x1FF
		} else {
			coord := AbsToMapCoord(x, y, 0)
			_, row, col := coord.RowCol()
			lx = int(col << 3)
			ly = int(row << 3)
		}

		//fmt.Printf(
		//	"%02x @ abs(%04x, %04x) -> map(%04x, %04x)\n",
		//	et,
		//	x,
		//	y,
		//	col,
		//	row,
		//)

		hb := hitbox[read8(wram, 0x0F60+i)&0x1F]

		if st == 0 {
			// dead:
			clr = red
		}

		drawOutlineBox(g, clr, lx+hb.X, ly+hb.Y, hb.W, hb.H)

		// colored number label:
		drawShadowedString(g, clr, fixed.Point26_6{X: fixed.I(lx), Y: fixed.I(ly + 12)}, fmt.Sprintf("%02X", et))
	}

	// draw Link:
	{
		x := read16(wram, 0x22)
		y := read16(wram, 0x20)
		var lx, ly int
		if true {
			lx = int(x) & 0x1FF
			ly = int(y) & 0x1FF
		} else {
			coord := AbsToMapCoord(x, y, 0)
			_, row, col := coord.RowCol()
			lx = int(col << 3)
			ly = int(row << 3)
		}

		green := image.NewUniform(color.RGBA{0, 255, 0, 255})
		drawOutlineBox(g, green, lx, ly, 16, 16)
		drawShadowedString(g, green, fixed.Point26_6{X: fixed.I(lx), Y: fixed.I(ly + 12)}, "LK")
	}
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
		hitbox[i].X = int(int8(e.Bus.Read8(alttp.SpriteHitBox_OffsetXLow + i)))
		hitbox[i].Y = int(int8(e.Bus.Read8(alttp.SpriteHitBox_OffsetYLow + i)))
		hitbox[i].W = int(int8(e.Bus.Read8(alttp.SpriteHitBox_Width + i)))
		hitbox[i].H = int(int8(e.Bus.Read8(alttp.SpriteHitBox_Height + i)))
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
	b02LoadOverworldTransitionPC |= fastRomBank

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
			a.JSL(fastRomBank | alttp.Underworld_LoadRoom)

			a.Comment("Underworld_LoadCustomTileAttributes#_0FFD65")
			a.JSL(fastRomBank | alttp.Underworld_LoadCustomTileAttributes)
			a.Comment("Underworld_LoadAttributeTable#_01B8BF")
			a.JSL(fastRomBank | alttp.Underworld_LoadAttributeTable)

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
		a.JSR_abs(uint16(alttp.Underworld_LoadEntrance_DoPotsBlocksTorches & 0xFFFF))
		a.Comment("Module06_UnderworldLoad after JSR Underworld_LoadEntrance")
		a.JMP_abs_imm16_w(uint16(alttp.Module06_UnderworldLoad_after_JSR_Underworld_LoadEntrance & 0xFFFF))
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

		a.Comment("LoadDefaultTileTypes#_0FFD2A")
		a.JSL(fastRomBank | alttp.LoadDefaultTileTypes)
		a.Comment("Intro_InitializeDefaultGFX#_0CC208")
		a.JSL(alttp.Intro_InitializeDefaultGFX)

		a.Comment("Intro_CreateTextPointers#_028022")
		a.JSL(fastRomBank | alttp.Intro_CreateTextPointers)
		if alttp.DecompressFontGFX != 0 {
			a.Comment("DecompressFontGFX#_0EF572")
			a.JSL(fastRomBank | alttp.DecompressFontGFX)
		}
		a.Comment("LoadItemGFXIntoWRAM4BPPBuffer#_00D271")
		a.JSL(fastRomBank | alttp.LoadItemGFXIntoWRAM4BPPBuffer)

		// initialize SRAM save file:
		a.REP(0x10)
		a.LDX_imm16_w(0)
		a.SEP(0x10)
		a.Comment("InitializeSaveFile#_0CDB3E")
		a.JSL(alttp.InitializeSaveFile)

		// this initializes some important DMA transfer source addresses to eliminate garbage transfers to VRAM[0]
		a.Comment("CopySaveToWRAM#_0CCEB2")
		a.JSL(alttp.CopySaveToWRAM)

		// general world state:
		a.Comment("disable rain")
		a.LDA_imm8_b(0x02)
		a.STA_long(0x7EF3C5)

		// a.Comment("no bed cutscene")
		// a.LDA_imm8_b(0x10)
		// a.STA_long(0x7EF3C6)

		// non-zero mirroring to skip message prompt on file load:
		// a.STA_long(0x7EC011)

		// STOP:
		a.STP()

		// loads a dungeon given an entrance ID:
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

		// run a frame:
		runFramePC = a.Label("mainRouting")
		a.SEP(0x30)
		// increment frame counter for proper animations:
		a.INC_dp(0x1A)
		a.Comment("JSR ClearOAMBuffer")
		// ClearOAMBuffer#_00841E
		a.JSR_abs(uint16(alttp.ClearOAMBuffer & 0xFFFF))
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | alttp.Module_MainRouting)
		a.BRA("updateVRAM")

		loadSupertilePC = a.Label("loadSupertile")
		a.SEP(0x30)
		a.INC_abs(0x0710)
		a.Comment("Intro_InitializeDefaultGFX after JSL DecompressAnimatedUnderworldTiles")
		a.JSL(fastRomBank | alttp.Intro_InitializeDefaultGFX_after_JSL_DecompressAnimatedUnderworldTiles)
		a.STZ_dp(0x11)
		a.Comment("LoadUnderworldSupertile")
		a.JSL(b02LoadUnderworldSupertilePC)
		a.STZ_dp(0x11)

		a.Label("updateVRAM")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(uint16(alttp.NMI_PrepareSprites & 0xFFFF))

		// real NMI starts here:
		nmiRoutinePC = a.Label("NMIRoutine")
		a.Comment("prepare for PPU writes")
		a.LDA_imm8_b(0x80)
		a.STA_abs(0x2100) // INIDISP
		a.STZ_abs(0x420C) // HDMAEN
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(uint16(alttp.NMI_DoUpdates & 0xFFFF))
		a.Comment("NMI_ReadJoypads")
		a.JSR_abs(uint16(alttp.NMI_ReadJoypads & 0xFFFF))

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
		a.JSL(fastRomBank | alttp.Underworld_HandleRoomTags)

		// check if submodule changed:
		a.LDA_dp(0x11)
		a.BEQ("no_submodule")

		a.Label("continue_submodule")
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | alttp.Module_MainRouting)

		a.Label("no_submodule")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(uint16(alttp.NMI_PrepareSprites & 0xFFFF))

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
		a.JSR_abs(uint16(alttp.NMI_DoUpdates & 0xFFFF))
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
		// emit into our custom $00:5400 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b00RunSingleFramePC&0xFFFF-0x5000:], true)
		a.SetBase(b00RunSingleFramePC)

		a.SEP(0x30)

		//a.Label("continue_submodule")
		// increment frame counter for proper animations:
		a.INC_dp(0x1A)
		a.Comment("JSR ClearOAMBuffer")
		// ClearOAMBuffer#_00841E
		a.JSR_abs(uint16(alttp.ClearOAMBuffer & 0xFFFF))
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | alttp.Module_MainRouting)

		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(uint16(alttp.NMI_PrepareSprites & 0xFFFF))

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
		a.JSR_abs(uint16(alttp.NMI_DoUpdates & 0xFFFF)) // NMI_DoUpdates
		a.Comment("NMI_ReadJoypads")
		a.JSR_abs(uint16(alttp.NMI_ReadJoypads & 0xFFFF))
		//a.PLB()
		//a.PLD()

		a.STP()

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	// this routine loads an overworld area from edge transition:
	{
		// emit into our custom routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b02LoadOverworldTransitionPC&0xFFFF-0x5000:], true)
		a.SetBase(b02LoadOverworldTransitionPC)

		// make sure we're in the right module, submodule:
		a.SEP(0x30)
		a.LDA_imm8_b(0x09)
		a.STA_abs(0x010C)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.INC_dp(0x11)
		a.STZ_dp(0xB0)

		// NOTE: we cannot simply JMP to dont_fade_song_b because it has a PLA and RTS.

		// $0410 and $0416 must contain direction as single bit in lower 4 bits
		// $0418 and $069C must contain transition direction as enum value

		a.STZ_abs(0x0696)
		a.STZ_abs(0x0698)
		a.STZ_abs(0x0126)

		// Overworld_LoadGFXAndScreenSize#_02AA07
		a.JSR_abs(uint16(alttp.Overworld_LoadGFXAndScreenSize & 0xFFFF))
		// OverworldHandleTransitions.change_palettes#_02A9F3
		a.JSR_abs(uint16(alttp.OverworldHandleTransitions_change_palettes & 0xFFFF))
		a.STP()

		// next, caller must execute MainRouting until submodule goes back to #$00.

		a.WriteTextTo(e.Logger)
	}

	{
		// skip over music & sfx loading since we did not implement APU registers:
		a = newEmitterAt(e, fastRomBank|alttp.Patch_JSR_Underworld_LoadSongBankIfNeeded, true)
		// TODO: verify content before patching
		//#_028293: JSR Underworld_LoadSongBankIfNeeded
		a.JMP_abs_imm16_w(uint16(alttp.Patch_SEP_20_RTL & 0xFFFF))
		//.exit
		//#_0282BC: SEP #$20
		//#_0282BE: RTL
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out RebuildHUD:
		a = newEmitterAt(e, fastRomBank|alttp.Patch_RebuildHUD_Keys, true)
		// TODO: verify content before patching
		//RebuildHUD_Keys:
		//	#_0DFA88: STA.l $7EF36F
		a.RTL()
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out Sprite_PrepOAMCoord to not disable offscreen sprites.
		// Sprite_PrepOAMCoord_disable#_06E48B: INC.w $0F00,X  (INC,X = $FE)
		// to                                   STZ.w $0F00,X  (STZ,X = $9E)
		a = newEmitterAt(e, fastRomBank|alttp.Patch_Sprite_PrepOAMCoord, true)
		// TODO: verify content before patching
		a.STZ_abs_x(0x0F00)
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out LoadSongBank#_008888
		a = newEmitterAt(e, fastRomBank|alttp.Patch_LoadSongBank, true)
		a.RTS()
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out Sprite_ShowMessageUnconditional#_05E219 to prevent messages from interrupting us:
		a = newEmitterAt(e, fastRomBank|alttp.Sprite_ShowMessageUnconditional, true)
		a.RTL()
		a.WriteTextTo(e.Logger)
	}

	//e.LoggerCPU = os.Stdout
	_ = loadSupertilePC

	{
		// run the initialization code:
		//e.LoggerCPU = os.Stdout
		if err = e.ExecAt(0x00_5000, donePC); err != nil {
			panic(err)
		}
		//e.LoggerCPU = nil

		write16(e.WRAM[:], 0x0ADC, 0xA680)
		write16(e.WRAM[:], 0xC00D, 0x0001)
	}

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
