package main

import (
	"fmt"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"os"
	"slices"
	"unsafe"
)

func reachabilityAnalysis(initEmu *System) (err error) {
	e := &System{}
	if err = e.InitEmulatorFrom(initEmu); err != nil {
		panic(err)
	}

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
	fmt.Printf("roomTable: %06x\n", roomHeaderTableLong)

	roomHeaderPointers := [0x140]uint16{}
	for i := uint16(0); i < 0x140; i++ {
		roomHeaderPointers[i] = e.Bus.Read16(roomHeaderTableLong + uint32(i)<<1)
		fmt.Printf("room[%03x]: %04x\n", i, roomHeaderPointers[i])
	}

	// sort pointers in ascending order:
	pointersSorted := [0x140]uint16{}
	copy(pointersSorted[:], roomHeaderPointers[:])
	slices.Sort(pointersSorted[:])

	// make a map of which room pointer owns which bytes in the table (lowest address "wins"):
	addrOwner := map[uint16]uint16{}
	for i := uint16(0); i < 0x140; i++ {
		for j := uint16(0); j < 14; j++ {
			p := pointersSorted[i] + j
			addrOwner[p] = pointersSorted[i]
		}
	}

	roomHeaders := [0x140][14]uint8{}
	for i := uint16(0); i < 0x140; i++ {
		for j := uint16(0); j < 14; j++ {
			p := roomHeaderPointers[i] + j
			if addrOwner[p] == roomHeaderPointers[i] {
				roomHeaders[i][j] = e.Bus.Read8(roomHeaderTableLong&0xff_0000 | uint32(p))
			}
		}
	}

	for i := uint16(0); i < 0x128; i++ {
		fmt.Printf("[%03x]: %#v\n", i, roomHeaders[i])
	}

	wram := (e.WRAM)[:]

	type Dungeon struct {
		DungeonID         uint8
		Entrances         []uint8
		Supertiles        []Supertile
		ContainsSupertile map[uint16]struct{}

		Stack []Supertile
	}
	dungeons := map[uint8]*Dungeon{}
	supertileEntrances := map[uint16][]uint8{}

	// create an all-encompassing EG map:
	all := image.NewNRGBA(image.Rect(0, 0, 16*512, 19*512))
	// clear the image and remove alpha layer
	draw.Draw(
		all,
		all.Bounds(),
		image.NewUniform(color.NRGBA{0, 0, 0, 255}),
		image.Point{},
		draw.Src)

	// entrances...
	eIDmin, eIDmax := uint8(0), uint8(0x84)
	//eIDmin, eIDmax := uint8(0x7b), uint8(0x7b)
	for eID := eIDmin; eID <= eIDmax; eID++ {
		// skip attract mode cinematic entrances:
		if eID >= 0x73 && eID <= 0x75 {
			continue
		}

		// poke the entrance ID into our asm code:
		e.HWIO.Dyn[setEntranceIDPC&0xffff-0x5000] = eID

		// load the entrance:
		if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
			panic(err)
		}
		entranceLoadedWRAM := *e.WRAM

		// read dungeon ID:
		dungeonID := read8(wram, 0x040C)

		// fetch or create dungeon record:
		dungeon, ok := dungeons[dungeonID]
		if !ok {
			dungeon = &Dungeon{
				DungeonID:         dungeonID,
				Entrances:         nil,
				Supertiles:        nil,
				ContainsSupertile: make(map[uint16]struct{}),
				Stack:             make([]Supertile, 0, 40),
			}
			dungeons[dungeonID] = dungeon
		}
		dungeon.Entrances = append(dungeons[dungeonID].Entrances, eID)

		// read initial entrance supertile:
		st := read16(wram, 0xA0)
		dungeon.Stack = append(dungeon.Stack, Supertile(st))

		if len(dungeon.Stack) > 0 {
			fmt.Printf("dungeon %02x\n", dungeonID)
		}

		for len(dungeon.Stack) > 0 {
			// pop off the stack:
			lifoEnd := len(dungeon.Stack) - 1
			st := dungeon.Stack[lifoEnd]
			dungeon.Stack = dungeon.Stack[0:lifoEnd]

			// skip if already scanned:
			st16 := uint16(st)
			if _, ok := dungeon.ContainsSupertile[st16]; ok {
				continue
			}

			fmt.Printf("  scan supertile %03x\n", st16)
			fmt.Printf("    header: %#v\n", roomHeaders[st16])
			dungeon.ContainsSupertile[st16] = struct{}{}
			dungeon.Supertiles = append(dungeon.Supertiles, st)

			{
				ste := supertileEntrances[st16]
				ste = append(ste, eID)
				supertileEntrances[st16] = ste
			}

			// load the supertile into emulator memory:
			*e.WRAM = entranceLoadedWRAM

			// set supertile:
			write16(wram, 0xA0, uint16(st))
			write16(wram, 0x048E, uint16(st))

			//e.Logger = os.Stdout
			//e.LoggerCPU = e.Logger
			if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
				return
			}
			//e.LoggerCPU = nil

			tiles := wram[0x12000:0x14000]
			if true {
				// export tilemaps:
				_ = os.WriteFile(fmt.Sprintf("t%03x.til.map", st16), tiles, 0644)
				bg1wram := (*(*[0x2000]uint8)(unsafe.Pointer(&wram[0x2000])))[:]
				_ = os.WriteFile(fmt.Sprintf("t%03x.bg1.map", st16), bg1wram, 0644)
				bg2wram := (*(*[0x2000]uint8)(unsafe.Pointer(&wram[0x4000])))[:]
				_ = os.WriteFile(fmt.Sprintf("t%03x.bg2.map", st16), bg2wram, 0644)
			}

			// render to EG map:
			{
				sy := (st16 & 0x1F0) << 5
				sx := (st16 & 0x00F) << 9
				pal, bg1p, bg2p, addColor, halfColor := renderBGLayers(e.WRAM, e.VRAM[0x4000:0x8000])
				ComposeToNonPaletted(
					all.SubImage(image.Rect(
						int(sx),
						int(sy),
						int(sx+512),
						int(sy+512),
					)).(draw.Image),
					pal,
					bg1p,
					bg2p,
					addColor,
					halfColor,
				)

				if drawBGLayerPNGs {
					// render bg1 png, bg2 png:
					blankFrame := newBlankFrame()
					{
						bg1 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
						ComposeToPaletted(bg1, pal, bg1p, [2]*image.Paletted{blankFrame, blankFrame}, addColor, halfColor)
						_ = exportPNG(fmt.Sprintf("t%03x.bg1.png", st16), bg1)
					}
					{
						bg2 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
						ComposeToPaletted(bg2, pal, [2]*image.Paletted{blankFrame, blankFrame}, bg2p, addColor, halfColor)
						_ = exportPNG(fmt.Sprintf("t%03x.bg2.png", st16), bg2)
					}
				}
			}

			exitType := [5]string{"warp", "stair0", "stair1", "stair2", "stair3"}
			exitST := [5]Supertile{
				Supertile(uint16(read8(wram, 0xC000)) | st16&0x100),
				Supertile(uint16(read8(wram, 0xC001)) | st16&0x100),
				Supertile(uint16(read8(wram, 0xC002)) | st16&0x100),
				Supertile(uint16(read8(wram, 0xC003)) | st16&0x100),
				Supertile(uint16(read8(wram, 0xC004)) | st16&0x100),
			}
			exitUsed := [5]bool{}
			if false {
				// follow supertile links to WARP, STAIR0, STAIR1, STAIR2, STAIR3:
				for j := 9; j < 14; j++ {
					dest := uint16(roomHeaders[st16][j]) | st16&0x100
					if dest == 0 {
						continue
					}

					// make sure we haven't already scanned the supertile:
					if _, ok := dungeon.ContainsSupertile[dest]; ok {
						continue
					}

					// add it to the stack to be scanned for links:
					fmt.Printf("    %s -> %v\n", exitType[j-9], Supertile(dest))
					dungeon.Stack = append(dungeon.Stack, Supertile(dest))
				}
			}

			pitDamages := roomsWithPitDamage[st]

			// check doors to neighboring supertiles:
			for m := 0; m < 16; m++ {
				tpos := read16(wram, uint32(0x19A0+(m<<1)))
				// stop marker:
				if tpos == 0 {
					//fmt.Printf("  door stop at marker\n")
					break
				}

				door := Door{
					Pos:  MapCoord(tpos >> 1),
					Type: DoorType(read16(wram, uint32(0x1980+(m<<1)))),
					Dir:  Direction(read16(wram, uint32(0x19C0+(m<<1)))),
				}
				doorTile := tiles[door.Pos+0x41]

				fmt.Printf("    door: %v; t = %02x\n", door, doorTile)

				// check if the door is on the edge:
				isDoorEdge, edgeDir, _, _ := door.Pos.IsDoorEdge()
				if !isDoorEdge {
					continue
				}

				// skip dungeon exits to overworld:
				if door.Type.IsExit() {
					continue
				}

				// don't traverse over dungeon-exit doors:
				if doorTile == 0x8e {
					// north/south dungeon exit:
					continue
				}

				// skip over stairwells behind doors on north edges:
				if door.Dir == DirNorth {
					stairTile := tiles[door.Pos+0x01]
					if stairTile >= 0x30 && stairTile <= 0x37 {
						continue
					}
				}

				// TODO: JSL
				// HandleEdgeTransitionMovementEast#_02B572
				// HandleEdgeTransitionMovementWest#_02B611
				// HandleEdgeTransitionMovementSouth#_02B6B2
				// HandleEdgeTransitionMovementNorth#_02B754

				if doorTile == 0x89 {
					// east/west exit:
					if door.Dir == DirWest && !exitUsed[3] {
						fmt.Printf("    door-transport: %v -> %v\n", door.Dir, exitST[3])
						dungeon.Stack = append(dungeon.Stack, exitST[3])
						exitUsed[3] = true
					} else if door.Dir == DirEast && !exitUsed[4] {
						fmt.Printf("    door-transport: %v -> %v\n", door.Dir, exitST[4])
						dungeon.Stack = append(dungeon.Stack, exitST[4])
						exitUsed[4] = true
					}
					continue
				}

				// attempt to move through the door to the neighboring supertile:
				var dest Supertile
				var dir Direction
				var ok bool
				if dest, dir, ok = st.MoveBy(edgeDir); !ok {
					continue
				}

				// make sure we haven't already scanned the neighboring supertile:
				dest16 := uint16(dest)
				if _, ok = dungeon.ContainsSupertile[dest16]; ok {
					continue
				}

				// push neighbor onto supertile stack:
				fmt.Printf("    door: %v -> %v\n", dir, dest)
				dungeon.Stack = append(dungeon.Stack, dest)
			}

			// check tilemap for special exits, e.g. stairs, pits, bombable floors:
			{
				for ta := uint16(0); ta < 0x2000; ta++ {
					t := tiles[ta]
					if !exitUsed[0] {
						if !pitDamages && !roomsWithUnreachableWarpPits[st] {
							// pit 0x20, bombable floor 0x62:
							if t == 0x20 || t == 0x62 {
								fmt.Printf("    pit: %v\n", exitST[0])
								dungeon.Stack = append(dungeon.Stack, exitST[0])
								exitUsed[0] = true
							}
						} else if t == 0x4B {
							// warp tile:
							fmt.Printf("    warp: %v\n", exitST[0])
							dungeon.Stack = append(dungeon.Stack, exitST[0])
							exitUsed[0] = true
						}
					}

					if t >= 0x30 && t <= 0x33 {
						ex := t - 0x30
						// stairs:
						if !exitUsed[1+ex] {
							fmt.Printf("    stairs%d: %v\n", ex, exitST[1+ex])
							dungeon.Stack = append(dungeon.Stack, exitST[1+ex])
							exitUsed[1+ex] = true
						}
					}

					if t >= 0x34 && t <= 0x37 {
						ex := t - 0x34
						// stairs:
						if !exitUsed[1+ex] {
							fmt.Printf("    stairs%d: %v\n", ex, exitST[1+ex])
							dungeon.Stack = append(dungeon.Stack, exitST[1+ex])
							exitUsed[1+ex] = true
						}
					}

					if !exitUsed[1] {
						// 38 - Straight inter-room stairs north/down edge
						// 39 - Straight inter-room stairs south/up edge
						if t == 0x38 || t == 0x39 {
							// stair0:
							fmt.Printf("    stairs0: %v\n", exitST[1])
							dungeon.Stack = append(dungeon.Stack, exitST[1])
							exitUsed[1] = true
						}
					}
				}

				// HACK: IP boss exit:
				if st16 == 0x0CE {
					fmt.Printf("    pit (IP): %v\n", exitST[0])
					dungeon.Stack = append(dungeon.Stack, exitST[0])
					exitUsed[0] = true
				}
			}

			// check open pathways on edges:
			{
				// open paths are 00s enclosed by 02s or 04s on either side of the path

				// south edge:
				if findWalkwayHoriz(tiles, 0x0fc0) || findWalkwayHoriz(tiles, 0x1fc0) {
					if dest, dir, ok := st.MoveBy(DirSouth); ok {
						// push neighbor onto supertile stack:
						fmt.Printf("    edge: %v -> %v\n", dir, dest)
						dungeon.Stack = append(dungeon.Stack, dest)
					}
				}

				// north edge:
				if findWalkwayHoriz(tiles, 0x0000) || findWalkwayHoriz(tiles, 0x1000) {
					if dest, dir, ok := st.MoveBy(DirNorth); ok {
						// push neighbor onto supertile stack:
						fmt.Printf("    edge: %v -> %v\n", dir, dest)
						dungeon.Stack = append(dungeon.Stack, dest)
					}
				}

				// west edge:
				if findWalkwayVert(tiles, 0x0000) || findWalkwayVert(tiles, 0x1000) {
					if dest, dir, ok := st.MoveBy(DirWest); ok {
						// push neighbor onto supertile stack:
						fmt.Printf("    edge: %v -> %v\n", dir, dest)
						dungeon.Stack = append(dungeon.Stack, dest)
					}
				}

				// east edge:
				if findWalkwayVert(tiles, 0x003f) || findWalkwayVert(tiles, 0x103f) {
					if dest, dir, ok := st.MoveBy(DirEast); ok {
						// push neighbor onto supertile stack:
						fmt.Printf("    edge: %v -> %v\n", dir, dest)
						dungeon.Stack = append(dungeon.Stack, dest)
					}
				}
			}
		}

		fmt.Printf("  supertiles: %#v\n", dungeon.Supertiles)
	}

	// emit full lookup of dungeonId -> []Supertile
	{
		ids := make([]uint8, 0, len(dungeons))
		for dungeonID := range dungeons {
			ids = append(ids, dungeonID)
		}
		slices.Sort(ids)

		// analyze dungeons for overlapping supertiles:
		for _, aid := range ids {
			for _, st := range dungeons[aid].Supertiles {
				for _, did := range ids {
					if aid == did {
						continue
					}
					// first two dungeons are allowed to conflict:
					if aid == 0x00 && did == 0x02 {
						continue
					}
					if aid == 0x02 && did == 0x00 {
						continue
					}

					for _, dst := range dungeons[did].Supertiles {
						if dst == st {
							fmt.Printf(
								"WARN: overlapping supertile 0x%03x between dungeons 0x%02x and 0x%02x\n",
								uint16(st),
								aid,
								did,
							)
						}
					}
				}
			}
		}

		fmt.Print("\nvar dungeonSupertiles = map[uint8][]uint16{\n")
		for _, dungeonID := range ids {
			fmt.Printf("\t0x%02x: {", dungeonID)
			sts := dungeons[dungeonID].Supertiles
			for i, st := range sts {
				fmt.Printf("0x%03x", uint16(st))
				if i < len(sts)-1 {
					fmt.Print(", ")
				}
			}
			fmt.Print("},\n")
		}
		fmt.Print("}\n")

		fmt.Print("\nvar dungeonEntrances = map[uint8][]uint8{\n")
		for _, dungeonID := range ids {
			fmt.Printf("\t0x%02x: {", dungeonID)
			sts := dungeons[dungeonID].Entrances
			for i, st := range sts {
				fmt.Printf("0x%02x", uint16(st))
				if i < len(sts)-1 {
					fmt.Print(", ")
				}
			}
			fmt.Print("},\n")
		}
		fmt.Print("}\n")

		fmt.Print("\nvar supertileEntrances = map[uint16][]uint8{\n")
		for st16 := uint16(0); st16 < 0x128; st16++ {
			ste, ok := supertileEntrances[st16]
			if !ok {
				continue
			}
			fmt.Printf("\t0x%03x: {", st16)
			for i, e := range ste {
				fmt.Printf("0x%02x", uint16(e))
				if i < len(ste)-1 {
					fmt.Print(", ")
				}
			}
			fmt.Print("},\n")
		}
		fmt.Print("}\n")

		//fmt.Printf("%#+v\n", dungeons)
	}

	if drawNumbers {
		black := image.NewUniform(color.RGBA{0, 0, 0, 255})
		white := image.NewUniform(color.RGBA{255, 255, 255, 255})

		for st := 0; st < 0x128; st++ {
			row := st / 0x10
			col := st % 0x10

			stx := col * 512
			sty := row * 512

			// draw supertile number in top-left:
			var stStr string
			if st < 0x100 {
				stStr = fmt.Sprintf("%02X", st)
			} else {
				stStr = fmt.Sprintf("%03X", st)
			}
			(&font.Drawer{
				Dst:  all,
				Src:  black,
				Face: inconsolata.Bold8x16,
				Dot:  fixed.Point26_6{fixed.I(stx + 5), fixed.I(sty + 5 + 12)},
			}).DrawString(stStr)
			(&font.Drawer{
				Dst:  all,
				Src:  white,
				Face: inconsolata.Bold8x16,
				Dot:  fixed.Point26_6{fixed.I(stx + 4), fixed.I(sty + 4 + 12)},
			}).DrawString(stStr)
		}
	}

	if err = exportPNG(fmt.Sprintf("%s.png", "eg"), all); err != nil {
		panic(err)
	}
	return
}

func findWalkwayHoriz(tiles []uint8, offs uint16) (edgeFound bool) {
	edgeFound = false

	for x := uint16(0); x < 0x3F; {
		// bottom edge:
		t := tiles[offs+x]
		if t == 0x02 || t == 0x04 {
			x++
			t = tiles[offs+x]
			if t == 0 {
				// scan until see the same boundary tile:
				x++
				for x < 0x40 {
					t := tiles[offs+x]
					x++
					if t == 0x02 || t == 0x04 {
						edgeFound = true
						return
					} else if t != 0 {
						break
					}
				}
			}
		} else {
			x++
		}
	}

	return
}

func findWalkwayVert(tiles []uint8, offs uint16) (edgeFound bool) {
	edgeFound = false

	for y := uint16(0); y < 0x3F; {
		// bottom edge:
		t := tiles[offs+y<<6]
		if t == 0x02 || t == 0x04 {
			y++
			t = tiles[offs+y<<6]
			if t == 0 {
				// scan until see the same boundary tile:
				y++
				for y < 0x40 {
					t := tiles[offs+y<<6]
					y++
					if t == 0x02 || t == 0x04 {
						edgeFound = true
						return
					} else if t != 0 {
						break
					}
				}
			}
		} else {
			y++
		}
	}

	return
}
