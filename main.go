package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/alttpo/snes"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

const hackhackhack = false

var (
	b02LoadUnderworldSupertilePC     uint32 = 0x02_5200
	b01LoadAndDrawRoomPC             uint32
	b01LoadAndDrawRoomSetSupertilePC uint32
	b00HandleRoomTagsPC              uint32 = 0x00_5300
	b00RunSingleFramePC              uint32 = 0x00_5400

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

	// generate room object PNGs:
	if true {
		capturePNG := func(i uint32) {
			pal, bg1p, bg2p, addColor, halfColor := renderBGLayers(
				e.WRAM,
				e.VRAM[0x4000:0x10000],
			)

			g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
			pal[0] = color.NRGBA{}
			ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
			if err = exportPNG(fmt.Sprintf("ro%03x.png", i), g); err != nil {
				panic(err)
			}
		}

		// load the entrance:
		if true {
			//eID := byte(0x00)
			//st := uint16(0x114)
			eID := dungeonEntrances[0x1a][0]
			st := dungeonSupertiles[0x1a][0]

			e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID
			if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
				panic(err)
			}

			// load and draw selected supertile:
			e.Bus.Write16(0xA0, st)
			e.Bus.Write16(0x048E, st)
			if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
				panic(err)
			}
		}

		for i := 0; i < 20; i++ {
			if err = e.ExecAt(runFramePC, donePC); err != nil {
				panic(err)
			}
		}

		os.WriteFile("vram.raw", e.VRAM[:], 0644)

		// ($5600 - $4000) / $20
		//emptyTile := uint16(0xB0)

		// find an empty tile in VRAM BG tiles:
		emptyTile := uint16(0)
		for i := 0; i < 0x400; i++ {
			isEmpty := true
			for j := 0; j < 0x20; j++ {
				if e.VRAM[0x4000+i<<5+j] != 0 {
					isEmpty = false
					break
				}
			}
			if !isEmpty {
				continue
			}

			emptyTile = uint16(i)
			fmt.Printf("empty = %03x\n", emptyTile)
			break
		}

		// clear tilemap:
		for o := 0x2000; o < 0x6000; o += 2 {
			e.WRAM[o+0] = byte(emptyTile & 0xFF)
			e.WRAM[o+1] = byte((emptyTile >> 8) & 0x03)
		}
		capturePNG(0xfff)

		// load tilemap pointers to $7E2000 into scratch space:
		// see RoomDraw_DrawFloors#_0189DF
		for i := 0; i < 11; i++ {
			e.WRAM[0xBF+i*3] = e.Bus.Read8(uint32(0x0186F8 + 0 + i*3))
			e.WRAM[0xC0+i*3] = e.Bus.Read8(uint32(0x0186F8 + 1 + i*3))
			e.WRAM[0xC1+i*3] = e.Bus.Read8(uint32(0x0186F8 + 2 + i*3))
		}

		// type1_subtype_1:
		for i := uint32(0); i <= 0xF7; i++ {
			routine := e.Bus.Read16(0x01_8200 + i*2)
			// if the routine starts with RTS then it doesn't do anything:
			if e.Bus.Read8(0x01_0000|uint32(routine)) == 0x60 {
				continue
			}

			fmt.Printf("%03x\n", i)
			dataOffset := e.Bus.Read16(0x01_8000 + i*2)

			a := asm.NewEmitter(e.HWIO.Dyn[0x600:], false)
			a.REP(0x30)
			// Y is the tilemap offset to write to
			a.LDY_imm16_w(0x1040)
			a.STY_dp(0x08)
			a.LDA_imm16_w(dataOffset)
			//a.TAX()
			a.LDX_imm16_w(dataOffset)
			a.JSR_abs(routine)
			a.STP()
			if err = a.Finalize(); err != nil {
				panic(err)
			}

			// clear scratch:
			for o := 0; o < 0x10; o++ {
				e.WRAM[o] = 0
			}

			// clear tilemap:
			for o := 0x2000; o < 0x6000; o += 2 {
				e.WRAM[o+0] = byte(emptyTile & 0xFF)
				e.WRAM[o+1] = byte((emptyTile >> 8) & 0x03)
			}
			e.WRAM[0xB7] = 0
			e.WRAM[0xB8] = 0

			// X size? (0..3)
			e.WRAM[0xB2] = 0
			e.WRAM[0xB3] = 0
			// Y size? (0..3)
			e.WRAM[0xB4] = 0
			e.WRAM[0xB5] = 0

			// run the object draw routine:
			//e.LoggerCPU = os.Stdout
			if err = e.ExecAt(uint32(0x01_5600), 0x015610); err != nil {
				panic(err)
			}

			capturePNG(i)
		}

		// type1_subtype_2:
		for i := uint32(0x100); i <= 0x13F; i++ {
			j := i - 0x100
			routine := e.Bus.Read16(0x01_8470 + j*2)
			// if the routine starts with RTS then it doesn't do anything:
			if e.Bus.Read8(0x01_0000|uint32(routine)) == 0x60 {
				continue
			}

			fmt.Printf("%03x\n", i)
			dataOffset := e.Bus.Read16(0x01_83F0 + j*2)

			a := asm.NewEmitter(e.HWIO.Dyn[0x600:], false)
			a.REP(0x30)
			// Y is the tilemap offset to write to
			a.LDY_imm16_w(0x1040)
			a.STY_dp(0x08)
			a.LDA_imm16_w(dataOffset)
			//a.TAX()
			a.LDX_imm16_w(dataOffset)
			a.JSR_abs(routine)
			a.STP()
			if err = a.Finalize(); err != nil {
				panic(err)
			}

			// clear scratch:
			for o := 0; o < 0x10; o++ {
				e.WRAM[o] = 0
			}

			// clear tilemap:
			for o := 0x2000; o < 0x6000; o += 2 {
				e.WRAM[o+0] = byte(emptyTile & 0xFF)
				e.WRAM[o+1] = byte((emptyTile >> 8) & 0x03)
			}
			e.WRAM[0xB7] = 0
			e.WRAM[0xB8] = 0

			// X size? (0..3)
			e.WRAM[0xB2] = 0
			e.WRAM[0xB3] = 0
			// Y size? (0..3)
			e.WRAM[0xB4] = 0
			e.WRAM[0xB5] = 0

			// run the object draw routine:
			//e.LoggerCPU = os.Stdout
			if err = e.ExecAt(uint32(0x01_5600), 0x015610); err != nil {
				panic(err)
			}

			capturePNG(i)
		}

		// type1_subtype_3:
		for i := uint32(0x200); i <= 0x27F; i++ {
			j := i - 0x200
			routine := e.Bus.Read16(0x01_85F0 + j*2)
			// if the routine starts with RTS then it doesn't do anything:
			if e.Bus.Read8(0x01_0000|uint32(routine)) == 0x60 {
				continue
			}

			fmt.Printf("%03x\n", i)
			dataOffset := e.Bus.Read16(0x01_84F0 + j*2)

			a := asm.NewEmitter(e.HWIO.Dyn[0x600:], false)
			a.REP(0x30)
			// Y is the tilemap offset to write to
			a.LDY_imm16_w(0x1040)
			a.STY_dp(0x08)
			a.LDA_imm16_w(dataOffset)
			//a.TAX()
			a.LDX_imm16_w(dataOffset)
			a.JSR_abs(routine)
			a.STP()
			if err = a.Finalize(); err != nil {
				panic(err)
			}

			// clear scratch:
			for o := 0; o < 0x10; o++ {
				e.WRAM[o] = 0
			}

			// clear tilemap:
			for o := 0x2000; o < 0x6000; o += 2 {
				e.WRAM[o+0] = byte(emptyTile & 0xFF)
				e.WRAM[o+1] = byte((emptyTile >> 8) & 0x03)
			}
			e.WRAM[0xB7] = 0
			e.WRAM[0xB8] = 0

			// X size? (0..3)
			e.WRAM[0xB2] = 0
			e.WRAM[0xB3] = 0
			// Y size? (0..3)
			e.WRAM[0xB4] = 0
			e.WRAM[0xB5] = 0

			// run the object draw routine:
			//e.LoggerCPU = os.Stdout
			if err = e.ExecAt(uint32(0x01_5600), 0x015610); err != nil {
				panic(err)
			}

			capturePNG(i)
		}
	}

	// new simplified reachability:
	if false {
		err = reachabilityAnalysis(&e)
		if err != nil {
			panic(err)
		}
	}

	// generate supertile animations:
	if false {
		jobs := make(chan func(), runtime.NumCPU())
		for i := 0; i < runtime.NumCPU(); i++ {
			go func() {
				for job := range jobs {
					job()
				}
			}()
		}

		st16min, st16max := uint16(0), uint16(0x127)
		//st16min, st16max := uint16(0x57), uint16(0x57)
		//st16min, st16max := uint16(0x12), uint16(0x12)

		wg := sync.WaitGroup{}
		for st16 := st16min; st16 <= st16max; st16++ {
			st16 := st16
			wg.Add(1)
			jobs <- func() {
				roomMovement(Supertile(st16), &e)
				wg.Done()
			}
		}

		wg.Wait()
		return
	}

	// overworld screens:
	if false {
		func(initEmu *System) {
			e := &System{
				Logger:    nil,
				LoggerCPU: nil,
			}
			if err = e.InitEmulatorFrom(initEmu); err != nil {
				panic(err)
			}

			wram := e.WRAM[:]

			a := gif.GIF{}
			var aLastFrame *image.Paletted = nil
			renderGifFrame := func() {
				pal, bg1p, bg2p, addColor, halfColor := renderOWBGLayers(
					e.WRAM,
					(*(*[0x1000]uint16)(unsafe.Pointer(&e.VRAM[0x0000])))[:],
					(*(*[0x1000]uint16)(unsafe.Pointer(&e.VRAM[0x2000])))[:],
					e.VRAM[0x4000:0x8000],
				)
				g := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
				ComposeToPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
				renderSpriteLabels(g, e.WRAM[:], Supertile(read16(e.WRAM[:], 0xA0)))

				dirty := true
				var delta *image.Paletted
				if aLastFrame != nil {
					delta, dirty = generateDeltaFrame(aLastFrame, g)
				} else {
					delta = g
				}
				aLastFrame = g

				if dirty {
					a.Image = append(a.Image, delta)
					a.Delay = append(a.Delay, 2)
					a.Disposal = append(a.Disposal, gif.DisposalNone)
				} else {
					a.Delay[len(a.Delay)-1] += 2
				}
			}

			frameTrace := bytes.Buffer{}
			f := 0

			if false {
				fmt.Println("module 05")
				write8(e.WRAM[:], 0x10, 0x05)
				write8(e.WRAM[:], 0x11, 0x00)
				write8(e.WRAM[:], 0xB0, 0x00)
				if err = e.ExecAt(runFramePC, donePC); err != nil {
					panic(err)
				}
				renderGifFrame()
			}

			if false {
				// load sanctuary entrance:
				fmt.Println("load sanctuary entrance")
				//e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = 0x02
				e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = 0x00
				if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
					panic(err)
				}
				f++
				fmt.Println(f)
				renderGifFrame()
			}

			if false {
				fmt.Println("run 2 frames")
				//e.Logger = os.Stdout
				//e.LoggerCPU = os.Stdout
				for n := 0; n < 30; n++ {
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}
					f++
					fmt.Println(f)
					renderGifFrame()
				}

				RenderGIF(&a, "test.gif")
				return
			}

			if true {
				// emulate until module=7,submodule=0:
				fmt.Println("wait until module 7")
				for {
					if read8(wram, 0x10) == 0x7 && read8(wram, 0x11) == 0 {
						break
					}
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}
					f++
					fmt.Println(f)
					renderGifFrame()
				}
			}

			if true {
				// now immediately exit sanctuary to go to overworld:
				//                            BYsSudlr AXLRvvvv
				e.HWIO.ControllerInput[0] = 0b00000100_00000000
				// emulate until module=9,submodule=0:
				fmt.Println("hold DOWN until module 9")
				for {
					//if read8(wram, 0x10) != 0x7 /* && read8(wram, 0x11) == 0*/ {
					//	// dump last frame's CPU trace:
					//	frameTrace.WriteTo(os.Stdout)
					//	break
					//}
					if read8(wram, 0x10) == 0x9 && read8(wram, 0x11) == 0 {
						frameTrace.WriteTo(os.Stdout)
						break
					}
					if f&63 == 63 {
						RenderGIF(&a, "test.gif")
					}
					frameTrace.Reset()
					e.Logger = &frameTrace
					e.LoggerCPU = &frameTrace
					//e.LoggerCPU = os.Stdout
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}
					e.Logger = nil
					e.LoggerCPU = nil
					f++
					fmt.Println(f)
					renderGifFrame()
				}
			}

			if true {
				fmt.Println("release DOWN for 300 frames")
				e.HWIO.ControllerInput[0] = 0b00000000_00000000
				for n := 0; n < 300; n++ {
					//e.LoggerCPU = os.Stdout
					if err = e.ExecAt(runFramePC, donePC); err != nil {
						panic(err)
					}
					f++
					fmt.Println(f)
					renderGifFrame()
				}
			}

			pal, bg1p, bg2p, addColor, halfColor := renderOWBGLayers(
				e.WRAM,
				(*(*[0x1000]uint16)(unsafe.Pointer(&e.VRAM[0x0000])))[:],
				(*(*[0x1000]uint16)(unsafe.Pointer(&e.VRAM[0x2000])))[:],
				e.VRAM[0x4000:0x8000],
			)
			g := image.NewNRGBA(image.Rect(0, 0, 512, 512))
			ComposeToNonPaletted(g, pal, bg1p, bg2p, addColor, halfColor)
			renderSpriteLabels(g, e.WRAM[:], Supertile(read16(e.WRAM[:], 0xA0)))

			_ = exportPNG("test.png", g)

			RenderGIF(&a, "test.gif")
		}(&e)
		return
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

	var eID uint8
	if ste, ok := supertileEntrances[uint16(st)]; ok {
		eID = ste[0]
	} else {
		// unused supertile:
		return
	}

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

	dungeonID := read8(wram, 0x040C)

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
					//renderOAMSprites(g, e.WRAM, e.VRAM, e.OAM, 0, 0)
					renderSpriteLabels(g, e.WRAM[:], st)

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

func renderOAMSprites(g *image.Paletted, wram *WRAMArray, vram *VRAMArray, oam *OAMArray, qx int, qy int) {
	for i := 0; i < 128; i++ {
		//fmt.Printf("[%02X,0]: %02X\n", i, oam[i<<2+0])
		//fmt.Printf("[%02X,1]: %02X\n", i, oam[i<<2+1])
		//fmt.Printf("[%02X,2]: %02X\n", i, oam[i<<2+2])
		//fmt.Printf("[%02X,3]: %02X\n", i, oam[i<<2+3])
		bits := oam[512+(i>>3)] >> ((i & 3) << 1) & 3
		//fmt.Printf("[%02X,4]: %02X\n", i, bits)

		x := int(oam[i<<2+0]) | ((int(bits) & 1) << 8)
		y := int(oam[i<<2+1])
		t := int(oam[i<<2+2])
		tn := int(oam[i<<2+3]) & 1
		fv := oam[i<<2+3]&0x80 != 0
		fh := oam[i<<2+3]&0x40 != 0
		pri := oam[i<<2+3] & 0x30 >> 4
		pal := oam[i<<2+3] & 0xE >> 1

		if x >= 256 {
			x -= 512
		}
		if y >= 0xF0 {
			y -= 0x100
		}

		//ta := room.e.HWIO.PPU.ObjTilemapAddress + uint32(t)*0x20
		//ta += uint32(tn) * room.e.HWIO.PPU.ObjNamespaceSeparation
		//ta &= 0xFFFF
		//room.e.VRAM[ta]
		drawShadowedString(
			g,
			image.White,
			fixed.Point26_6{X: fixed.I(qx + x), Y: fixed.I(qy + y + 12)},
			fmt.Sprintf("%03X", t|tn<<8),
		)
		_, _, _, _ = fv, fh, pri, pal
	}
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

		a.Comment("LoadDefaultTileTypes#_0FFD2A")
		a.JSL(fastRomBank | 0x0F_FD2A)
		a.Comment("Intro_InitializeDefaultGFX#_0CC208")
		a.JSL(0x0C_C208)
		//a.Comment("LoadDefaultGraphics#_00E310")
		//a.JSL(fastRomBank | 0x00_E310)
		//a.Comment("InitializeTilesets#_00E1DB")
		//a.JSL(fastRomBank | 0x00_E1DB)
		//a.LDY_imm8_b(0x5D)
		//a.Comment("DecompressAnimatedUnderworldTiles#_00D377")
		//a.JSL(fastRomBank | 0x00_D377)

		a.Comment("Intro_CreateTextPointers#_028022")
		a.JSL(fastRomBank | 0x02_8022)
		a.Comment("DecompressFontGFX#_0EF572")
		a.JSL(fastRomBank | 0x0E_F572)
		a.Comment("LoadItemGFXIntoWRAM4BPPBuffer#_00D271")
		a.JSL(fastRomBank | 0x00_D271)

		// initialize SRAM save file:
		a.REP(0x10)
		a.LDX_imm16_w(0)
		a.SEP(0x10)
		a.Comment("InitializeSaveFile#_0CDB3E")
		a.JSL(0x0C_DB3E)

		// this initializes some important DMA transfer source addresses to eliminate garbage transfers to VRAM[0]
		a.Comment("CopySaveToWRAM#_0CCEB2")
		a.JSL(0x0C_CEB2)

		// general world state:
		a.Comment("disable rain")
		a.LDA_imm8_b(0x02)
		a.STA_long(0x7EF3C5)

		a.Comment("no bed cutscene")
		a.LDA_imm8_b(0x10)
		a.STA_long(0x7EF3C6)

		// non-zero mirroring to skip message prompt on file load:
		a.STA_long(0x7EC011)

		//a.BRA("mainRouting")
		a.STP()
		//a.BRA("loadEntrance")

		// load an overworld from an underworld exit:
		loadExitPC = a.Label("loadExit")
		a.REP(0x30)
		setExitSupertilePC = a.Label("setExitSupertile") + 1
		// set underworld supertile ID we're exiting from:
		a.LDA_imm16_w(0x0012)
		a.STA_dp(0xA0)

		// transition to overworld:
		loadOverworldPC = a.Label("loadOverworld")
		a.SEP(0x30)
		a.LDA_imm8_b(0x08)
		a.STA_abs(0x010C)
		a.STA_dp(0x10)
		a.STZ_dp(0x11)
		a.STZ_dp(0xB0)
		// DeleteCertainAncillaeStopDashing#_028A0E
		a.Comment("DeleteCertainAncillaeStopDashing")
		// Ancilla_TerminateSelectInteractives#_09AC57
		a.JSL(0x09_AC57)
		a.LDA_abs(0x0372)
		a.BEQ("mainRouting")

		a.STZ_dp(0x4D)
		a.STZ_dp(0x46)

		a.LDA_imm8_b(0xFF)
		a.STA_dp(0x29)
		a.STA_dp(0xC7)

		a.STZ_dp(0x3D)
		a.STZ_dp(0x5E)

		a.STZ_abs(0x032B)
		a.STZ_abs(0x0372)

		//#_028A2B: LDA.b #$00 ; LINKSTATE 00
		a.LDA_imm8_b(0x00)
		//#_028A2D: STA.b $5D
		a.STA_dp(0x5D)
		a.BRA("mainRouting")

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
		a.Comment("JSR ClearOAMBuffer")
		// ClearOAMBuffer#_00841E
		a.JSR_abs(0x841E)
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

		// real NMI starts here:
		nmiRoutinePC = a.Label("NMIRoutine")
		a.Comment("prepare for PPU writes")
		a.LDA_imm8_b(0x80)
		a.STA_abs(0x2100) // INIDISP
		a.STZ_abs(0x420C) // HDMAEN
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0)
		a.Comment("NMI_ReadJoypads")
		a.JSR_abs(0x83D1)

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
		// emit into our custom $00:5400 routine:
		a = asm.NewEmitter(e.HWIO.Dyn[b00RunSingleFramePC&0xFFFF-0x5000:], true)
		a.SetBase(b00RunSingleFramePC)

		a.SEP(0x30)

		//a.Label("continue_submodule")
		a.Comment("JSR ClearOAMBuffer")
		// ClearOAMBuffer#_00841E
		a.JSR_abs(0x841E)
		a.Comment("JSL Module_MainRouting")
		a.JSL(fastRomBank | 0x00_80B5)

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

	{
		// patch out LoadSongBank#_008888
		a = newEmitterAt(e, fastRomBank|0x00_8888, true)
		a.RTS()
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
