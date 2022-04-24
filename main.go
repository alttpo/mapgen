package main

import (
	"encoding/binary"
	"fmt"
	"github.com/alttpo/snes/asm"
	"github.com/alttpo/snes/mapping/lorom"
	"image"
	"io/ioutil"
	"os"
	"sync"
	"unsafe"
)

const drawOverlays = true

var (
	b02LoadUnderworldSupertilePC     uint32 = 0x02_5200
	b01LoadAndDrawRoomPC             uint32
	b01LoadAndDrawRoomSetSupertilePC uint32
	b00HandleRoomTagsPC              uint32 = 0x00_5300
	loadEntrancePC                   uint32
	setEntranceIDPC                  uint32
	loadSupertilePC                  uint32
	donePC                           uint32
)

func main() {
	var err error

	var f *os.File
	f, err = os.Open("alttp-jp.sfc")
	if err != nil {
		panic(err)
	}

	var e *System

	// create the CPU-only SNES emulator:
	e = &System{
		Logger:    os.Stdout,
		LoggerCPU: nil,
	}
	if err = e.CreateEmulator(); err != nil {
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

	var a *asm.Emitter

	// initialize game:
	e.CPU.Reset()
	//#_008029: JSR Sound_LoadIntroSongBank		// skip this
	// this is useless zeroing of memory; don't need to run it
	//#_00802C: JSR Startup_InitializeMemory
	if err = e.Exec(0x00_8029); err != nil {
		panic(err)
	}

	{
		// must execute in bank $01
		a = asm.NewEmitter(e.HWIO.Dyn[0x01_5100&0xFFFF-0x5000:], true)
		a.SetBase(0x01_5100)

		{
			b01LoadAndDrawRoomPC = a.Label("loadAndDrawRoom")
			a.REP(0x30)
			b01LoadAndDrawRoomSetSupertilePC = a.Label("loadAndDrawRoomSetSupertile") + 1
			a.LDA_imm16_w(0x0000)
			a.STA_dp(0xA0)
			a.SEP(0x30)

			// loads header and draws room
			a.Comment("Underworld_LoadRoom#_01873A")
			a.JSL(0x01_873A)

			a.Comment("Underworld_LoadCustomTileAttributes#_0FFD65")
			a.JSL(0x0F_FD65)
			a.Comment("Underworld_LoadAttributeTable#_01B8BF")
			a.JSL(0x01_B8BF)

			// then JSR Underworld_LoadHeader#_01B564 to reload the doors into $19A0[16]
			//a.BRA("jslUnderworld_LoadHeader")
			a.WDM(0xAA)
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
		a.SetBase(0x00_5000)
		a.SEP(0x30)

		a.Comment("InitializeTriforceIntro#_0CF03B: sets up initial state")
		a.JSL(0x0C_F03B)
		a.Comment("LoadDefaultTileAttributes#_0FFD2A")
		a.JSL(0x0F_FD2A)

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
		a.JSL(0x00_80B5)
		a.BRA("updateVRAM")

		loadSupertilePC = a.Label("loadSupertile")
		a.SEP(0x30)
		a.INC_abs(0x0710)
		a.Comment("Intro_InitializeDefaultGFX after JSL DecompressAnimatedUnderworldTiles")
		a.JSL(0x0C_C237)
		a.STZ_dp(0x11)
		a.Comment("LoadUnderworldSupertile")
		a.JSL(b02LoadUnderworldSupertilePC)

		a.Label("updateVRAM")
		// this code sets up the DMA transfer parameters for animated BG tiles:
		a.Comment("NMI_PrepareSprites")
		a.JSR_abs(0x85FC)
		a.Comment("NMI_DoUpdates")
		a.JSR_abs(0x89E0) // NMI_DoUpdates

		// WDM triggers an abort for values >= 10
		donePC = a.Label("done")
		a.WDM(0xAA)

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
		//a.JSL(0x00_E43A)
		a.Comment("Underworld_HandleRoomTags#_01C2FD")
		a.JSL(0x01_C2FD)

		// check if submodule changed:
		a.LDA_dp(0x11)
		a.BEQ("no_submodule")

		a.Label("continue_submodule")
		a.Comment("JSL Module_MainRouting")
		a.JSL(0x00_80B5)

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
		a.LDA_dp(0x11)
		a.BNE("continue_submodule")

		a.STZ_dp(0x11)
		a.WDM(0xAA)

		// finalize labels
		if err = a.Finalize(); err != nil {
			panic(err)
		}
		a.WriteTextTo(e.Logger)
	}

	{
		// skip over music & sfx loading since we did not implement APU registers:
		a = newEmitterAt(e, 0x02_8293, true)
		//#_028293: JSR Underworld_LoadSongBankIfNeeded
		a.JMP_abs_imm16_w(0x82BC)
		//.exit
		//#_0282BC: SEP #$20
		//#_0282BE: RTL
		a.WriteTextTo(e.Logger)
	}

	{
		// patch out RebuildHUD:
		a = newEmitterAt(e, 0x0D_FA88, true)
		//RebuildHUD_Keys:
		//	#_0DFA88: STA.l $7EF36F
		a.RTL()
		a.WriteTextTo(e.Logger)
	}

	//s.LoggerCPU = os.Stdout
	_ = loadSupertilePC

	// run the initialization code:
	if err = e.ExecAt(0x00_5000, donePC); err != nil {
		panic(err)
	}

	//RoomsWithPitDamage#_00990C [0x70]uint16
	roomsWithPitDamage := make(map[Supertile]bool, 0x128)
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
	supertiles := make(map[Supertile]*RoomState, 0x128)

	// scan underworld for certain tile types:
	if false {
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

	// iterate over entrances:
	wg := sync.WaitGroup{}
	for eID := uint8(0); eID < entranceCount; eID++ {
		fmt.Fprintf(e.Logger, "entrance $%02x\n", eID)

		// poke the entrance ID into our asm code:
		//dyn := e.HWIO.Dyn
		//e.HWIO.Reset()
		//e.HWIO.Dyn = dyn
		e.HWIO.Dyn[setEntranceIDPC-0x5000] = eID
		// load the entrance and draw the room:
		if err = e.ExecAt(loadEntrancePC, donePC); err != nil {
			panic(err)
		}

		g := &entranceGroups[eID]
		g.EntranceID = eID
		g.Supertile = Supertile(e.ReadWRAM16(0xA0))

		g.Rooms = make([]*RoomState, 0, 0x20)

		// function to create a room and track it:
		createRoom := func(st Supertile) (room *RoomState) {
			var ok bool
			if room, ok = supertiles[st]; ok {
				fmt.Printf("    reusing room %s\n", st)
				//if eID != room.EntranceID {
				//	panic(fmt.Errorf("conflicting entrances for room %s", st))
				//}
				return
			}

			fmt.Printf("    creating room %s\n", st)

			// load and draw current supertile:
			write16(e.WRAM[:], 0xA0, uint16(st))
			if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
				panic(err)
			}

			//// load and draw current supertile:
			//write16(e.HWIO.Dyn[:], b01LoadAndDrawRoomSetSupertilePC-0x01_5000, uint16(st))
			//if err = e.ExecAt(b01LoadAndDrawRoomPC, 0); err != nil {
			//	panic(err)
			//}

			room = &RoomState{
				Supertile:         st,
				Rendered:          nil,
				Hookshot:          make(map[MapCoord]byte, 0x2000),
				TilesVisitedStar0: make(map[MapCoord]empty, 0x2000),
				TilesVisitedStar1: make(map[MapCoord]empty, 0x2000),
				TilesVisitedTag0:  make(map[MapCoord]empty, 0x2000),
				TilesVisitedTag1:  make(map[MapCoord]empty, 0x2000),
			}
			room.TilesVisited = room.TilesVisitedStar0
			wram := (&room.WRAM)[:]
			tiles := (&room.Tiles)[:]

			copy(room.VRAMTileSet[:], e.VRAM[0x4000:0x8000])
			copy(wram, e.WRAM[:])
			copy(tiles, e.WRAM[0x12000:0x14000])

			// make a map full of $01 Collision and carve out reachable areas:
			for i := range room.Reachable {
				room.Reachable[i] = 0x01
			}

			g.Rooms = append(g.Rooms, room)
			supertiles[st] = room

			//ioutil.WriteFile(fmt.Sprintf("data/%03X.wram", uint16(st)), wram, 0644)
			ioutil.WriteFile(fmt.Sprintf("data/%03X.tmap", uint16(st)), tiles, 0644)

			room.WarpExitTo = Supertile(read8(wram, 0xC000))
			room.StairExitTo = [4]Supertile{
				Supertile(read8(wram, uint32(0xC001))),
				Supertile(read8(wram, uint32(0xC002))),
				Supertile(read8(wram, uint32(0xC003))),
				Supertile(read8(wram, uint32(0xC004))),
			}
			room.WarpExitLayer = MapCoord(read8(wram, uint32(0x063C))&2) << 11
			room.StairTargetLayer = [4]MapCoord{
				MapCoord(read8(wram, uint32(0x063D))&2) << 11,
				MapCoord(read8(wram, uint32(0x063E))&2) << 11,
				MapCoord(read8(wram, uint32(0x063F))&2) << 11,
				MapCoord(read8(wram, uint32(0x0640))&2) << 11,
			}

			fmt.Fprintf(e.Logger, "    TAG1 = %02x\n", read8(wram, 0xAE))
			fmt.Fprintf(e.Logger, "    TAG2 = %02x\n", read8(wram, 0xAF))
			//fmt.Fprintf(s.Logger, "    WARPTO   = %s\n", Supertile(read8(wram, 0xC000)))
			//fmt.Fprintf(s.Logger, "    STAIR0TO = %s\n", Supertile(read8(wram, 0xC001)))
			//fmt.Fprintf(s.Logger, "    STAIR1TO = %s\n", Supertile(read8(wram, 0xC002)))
			//fmt.Fprintf(s.Logger, "    STAIR2TO = %s\n", Supertile(read8(wram, 0xC003)))
			//fmt.Fprintf(s.Logger, "    STAIR3TO = %s\n", Supertile(read8(wram, 0xC004)))
			//fmt.Fprintf(s.Logger, "    DARK     = %v\n", room.IsDarkRoom())

			// process doors first:
			doors := make([]Door, 0, 16)
			for m := 0; m < 16; m++ {
				tpos := read16(wram[:], uint32(0x19A0+(m<<1)))
				// stop marker:
				if tpos == 0 {
					//fmt.Fprintf(s.Logger, "    door stop at marker\n")
					break
				}

				door := Door{
					Pos:  MapCoord(tpos >> 1),
					Type: DoorType(read16(wram[:], uint32(0x1980+(m<<1)))),
					Dir:  Direction(read16(wram[:], uint32(0x19C0+(m<<1)))),
				}
				doors = append(doors, door)

				fmt.Fprintf(e.Logger, "    door: %v\n", door)

				isDoorEdge, _, _, _ := door.Pos.IsDoorEdge()

				{
					// open up doors that are in front of interroom stairwells:
					var stair MapCoord

					switch door.Dir {
					case DirNorth:
						stair = door.Pos + 0x01
						break
					case DirSouth:
						stair = door.Pos + 0xC1
						break
					case DirEast:
						stair = door.Pos + 0x43
						break
					case DirWest:
						stair = door.Pos + 0x40
						break
					}

					v := tiles[stair]
					if v >= 0x30 && v <= 0x39 {
						tiles[door.Pos+0x41+0x00] = 0x00
						tiles[door.Pos+0x41+0x01] = 0x00
						tiles[door.Pos+0x41+0x40] = 0x00
						tiles[door.Pos+0x41+0x41] = 0x00
					}
				}

				if door.Type.IsExit() {
					lyr, row, col := door.Pos.RowCol()
					// patch up the door tiles to prevent reachability from exiting:
					for y := uint16(0); y < 4; y++ {
						for x := uint16(0); x < 4; x++ {
							t := lyr | (row+y)<<6 | (col + x)
							if tiles[t] >= 0xF0 {
								tiles[t] = 0x00
							}
						}
					}
					continue
				}

				if door.Type == 0x30 {
					// exploding wall:
					pos := int(door.Pos)
					fmt.Printf("    exploding wall %s\n", door.Pos)
					for c := 0; c < 11; c++ {
						for r := 0; r < 12; r++ {
							tiles[pos+(r<<6)-c] = 0
							tiles[pos+(r<<6)+1+c] = 0
						}
					}
					continue
				}

				if isDoorEdge {
					// blow open edge doorways:
					var (
						start        MapCoord
						tn           MapCoord
						doorTileType uint8
						doorwayTile  uint8
						adj          int
					)

					var ok bool
					lyr, _, _ := door.Pos.RowCol()

					switch door.Dir {
					case DirNorth:
						start = door.Pos + 0x81
						doorwayTile = 0x80 | uint8(lyr>>10)
						adj = 1
						break
					case DirSouth:
						start = door.Pos + 0x41
						doorwayTile = 0x80 | uint8(lyr>>10)
						adj = 1
						break
					case DirEast:
						start = door.Pos + 0x41
						doorwayTile = 0x81 | uint8(lyr>>10)
						adj = 0x40
						break
					case DirWest:
						start = door.Pos + 0x42
						doorwayTile = 0x81 | uint8(lyr>>10)
						adj = 0x40
						break
					}

					doorTileType = tiles[start]
					if doorTileType < 0xF0 {
						// don't blow this doorway; it's custom:
						continue
					}

					tn = start
					canBlow := func(v uint8) bool {
						if v == 0x01 || v == 0x00 {
							return true
						}
						if v == doorTileType {
							return true
						}
						if v >= 0x28 && v <= 0x2B {
							return true
						}
						if v == 0x10 {
							// slope?? found in sanctuary $002
							return true
						}
						return false
					}
					for i := 0; i < 12; i++ {
						v := tiles[tn]
						if canBlow(v) {
							fmt.Printf("    blow open %s\n", tn)
							tiles[tn] = doorwayTile
							fmt.Printf("    blow open %s\n", MapCoord(int(tn)+adj))
							tiles[int(tn)+adj] = doorwayTile
						} else {
							panic(fmt.Errorf("something blocking the doorway at %s: $%02x", tn, v))
							break
						}

						tn, _, ok = tn.MoveBy(door.Dir, 1)
						if !ok {
							break
						}
					}
					continue
				}

				//if !isDoorEdge
				{
					var (
						start        MapCoord
						tn           MapCoord
						doorTileType uint8
						maxCount     int
						count        int
						doorwayTile  uint8
						adj          int
					)

					var ok bool
					lyr, _, _ := door.Pos.RowCol()

					switch door.Dir {
					case DirNorth:
						start = door.Pos + 0x81
						maxCount = 12
						doorwayTile = 0x80 | uint8(lyr>>10)
						adj = 1
						break
					case DirSouth:
						start = door.Pos + 0x41
						maxCount = 12
						doorwayTile = 0x80 | uint8(lyr>>10)
						adj = 1
						break
					case DirEast:
						start = door.Pos + 0x42
						maxCount = 10
						doorwayTile = 0x81 | uint8(lyr>>10)
						adj = 0x40
						break
					case DirWest:
						start = door.Pos + 0x42
						maxCount = 10
						doorwayTile = 0x81 | uint8(lyr>>10)
						adj = 0x40
						break
					}

					var mustStop func(uint8) bool

					doorTileType = tiles[start]
					if doorTileType >= 0x80 && doorTileType <= 0x8D {
						mustStop = func(v uint8) bool {
							if v == 0x01 {
								return false
							}
							if v >= 0x28 && v <= 0x2B {
								return false
							}
							if v == doorwayTile {
								return false
							}
							if v >= 0xF0 {
								return false
							}
							return true
						}
					} else if doorTileType >= 0xF0 {
						oppositeDoorType := uint8(0)
						if doorTileType >= 0xF8 {
							oppositeDoorType = doorTileType - 8
						} else if doorTileType >= 0xF0 {
							oppositeDoorType = doorTileType + 8
						}

						mustStop = func(v uint8) bool {
							if v == 0x01 {
								return false
							}
							if v == doorwayTile {
								return false
							}
							if v == oppositeDoorType {
								return false
							}
							if v == doorTileType {
								return false
							}
							if v >= 0x28 && v <= 0x2B {
								// ledge tiles can be found in doorway in fairy cave $008:
								return false
							}
							return true
						}
					} else {
						// bad door starter tile type
						fmt.Fprintf(e.Logger, fmt.Sprintf("unrecognized door tile at %s: $%02x\n", start, doorTileType))
						continue
					}

					// check many tiles behind door for opposite door tile:
					i := 0
					tn = start
					for ; i < maxCount; i++ {
						v := tiles[tn]
						if mustStop(v) {
							break
						}
						tn, _, ok = tn.MoveBy(door.Dir, 1)
						if !ok {
							break
						}
					}
					count = i

					// blow open the doorway:
					tn = start
					for i := 0; i < count; i++ {
						v := tiles[tn]
						if mustStop(v) {
							break
						}

						//fmt.Printf("    blow open %s\n", tn)
						tiles[tn] = doorwayTile
						//fmt.Printf("    blow open %s\n", mapCoord(int(tn)+adj))
						tiles[int(tn)+adj] = doorwayTile
						tn, _, _ = tn.MoveBy(door.Dir, 1)
					}
				}
			}
			room.Doors = doors

			// find layer-swap tiles in doorways:
			swapCount := read16(wram, 0x044E)
			room.SwapLayers = make(map[MapCoord]empty, swapCount*4)
			for i := uint16(0); i < swapCount; i += 2 {
				t := MapCoord(read16(wram, uint32(0x06C0+i)))

				// mark the 2x2 tile as a layer-swap:
				room.SwapLayers[t+0x00] = empty{}
				room.SwapLayers[t+0x01] = empty{}
				room.SwapLayers[t+0x40] = empty{}
				room.SwapLayers[t+0x41] = empty{}
				// have to put it on both layers? ew
				room.SwapLayers[t|0x1000+0x00] = empty{}
				room.SwapLayers[t|0x1000+0x01] = empty{}
				room.SwapLayers[t|0x1000+0x40] = empty{}
				room.SwapLayers[t|0x1000+0x41] = empty{}
			}

			// find interroom stair objects:
			stairCount := uint32(0)
			for _, n := range []uint32{0x0438, 0x043A, 0x047E, 0x0482, 0x0480, 0x0484, 0x04A2, 0x04A6, 0x04A4, 0x04A8} {
				index := uint32(read16(wram, n))
				if index > stairCount {
					stairCount = index
				}
			}
			for i := uint32(0); i < stairCount; i += 2 {
				t := MapCoord(read16(wram, 0x06B0+i))
				room.Stairs = append(room.Stairs, t)
				fmt.Fprintf(e.Logger, "    interroom stair at %s\n", t)
			}

			for i := uint32(0); i < 0x20; i += 2 {
				pos := MapCoord(read16(wram, 0x0540+i) >> 1)
				if pos == 0 {
					break
				}
				fmt.Printf(
					"    manipulable(%s): %02x, %04x @%04x -> %04x%04x,%04x%04x\n",
					pos,
					i,
					read16(wram, 0x0500+i), // MANIPPROPS
					read16(wram, 0x0520+i), // MANIPOBJX
					read16(wram, 0x0560+i), // MANIPRTNW
					read16(wram, 0x05A0+i), // MANIPRTNE
					read16(wram, 0x0580+i), // MANIPRTSW
					read16(wram, 0x05C0+i), // MANIPRTSE
				)
			}

			for i := uint32(0); i < 6; i++ {
				gt := read16(wram, 0x06E0+i<<1)
				if gt == 0 {
					break
				}

				fmt.Printf("    chest($%04x)\n", gt)

				if gt&0x8000 != 0 {
					// locked cell door:
					t := MapCoord((gt & 0x7FFF) >> 1)
					if tiles[t] == 0x58+uint8(i) {
						tiles[t+0x00] = 0x00
						tiles[t+0x01] = 0x00
						tiles[t+0x40] = 0x00
						tiles[t+0x41] = 0x00
					}
					if tiles[t|0x1000] == 0x58+uint8(i) {
						tiles[t|0x1000+0x00] = 0x00
						tiles[t|0x1000+0x01] = 0x00
						tiles[t|0x1000+0x40] = 0x00
						tiles[t|0x1000+0x41] = 0x00
					}
				}
			}

			// clear all enemy health to see if this triggers something:
			for i := uint32(0); i < 16; i++ {
				write8(room.WRAM[:], 0x0DD0+i, 0)
			}

			room.HandleRoomTags(e)

			//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap", uint16(st)), (&room.Tiles)[:], 0644)

			return
		}

		{
			// if this is the entrance, Link should be already moved to his starting position:
			wram := (&e.WRAM)[:]
			linkX := read16(wram, 0x22)
			linkY := read16(wram, 0x20)
			linkLayer := read16(wram, 0xEE)
			g.EntryCoord = AbsToMapCoord(linkX, linkY, linkLayer)
			fmt.Fprintf(e.Logger, "  link coord = {%04x, %04x, %04x}\n", linkX, linkY, linkLayer)
		}

		createRoom(g.Supertile)

		// build a stack (LIFO) of supertile entry points to visit:
		lifo := make([]EntryPoint, 0, 0x100)
		lifo = append(lifo, EntryPoint{g.Supertile, g.EntryCoord, DirNone, ExitPoint{}})

		// process the LIFO:
		for len(lifo) != 0 {
			// pop off the stack:
			lifoEnd := len(lifo) - 1
			ep := lifo[lifoEnd]
			lifo = lifo[0:lifoEnd]

			this := ep.Supertile

			fmt.Fprintf(e.Logger, "  ep = %s\n", ep)

			// create a room by emulation:
			room := createRoom(this)

			// check if room causes pit damage vs warp:
			// RoomsWithPitDamage#_00990C [0x70]uint16
			pitDamages := roomsWithPitDamage[this]

			warpExitTo := room.WarpExitTo
			stairExitTo := &room.StairExitTo
			warpExitLayer := room.WarpExitLayer
			stairTargetLayer := &room.StairTargetLayer

			//exitSeen := make(map[Supertile]struct{}, 24)
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
				fmt.Fprintf(e.Logger, "    %s to %s\n", name, ep)
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
							fmt.Fprintf(e.Logger, "    manip(%s) %02x = %04x\n", t, v, p)
							if p == 0 {
								fmt.Fprintf(e.Logger, "    pushBlock(%s)\n", t)

								// push block flips 0x0641
								write8(room.WRAM[:], 0x0641, 0x01)
								if read8(room.WRAM[:], 0xAE)|read8(room.WRAM[:], 0xAF) != 0 {
									// handle tags if there are any after the push to see if it triggers a secret:
									room.HandleRoomTags(e)
									// TODO: properly determine which tag was activated
									room.TilesVisited = room.TilesVisitedTag0
								}
							}
							return
						}

						v16 := read16(room.Tiles[:], uint32(t))
						if v16 == 0x3A3A || v16 == 0x3B3B {
							fmt.Fprintf(e.Logger, "    star(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							room.HandleRoomTags(e)

							// swap out visited maps:
							if read8(room.WRAM[:], 0x04BC) == 0 {
								fmt.Fprintf(e.Logger, "    star0\n")
								room.TilesVisited = room.TilesVisitedStar0
								//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap0", uint16(this)), room.Tiles[:], 0644)
							} else {
								fmt.Fprintf(e.Logger, "    star1\n")
								room.TilesVisited = room.TilesVisitedStar1
								//ioutil.WriteFile(fmt.Sprintf("data/%03X.cmap1", uint16(this)), room.Tiles[:], 0644)
							}
							return
						}

						// floor or pressure switch:
						if v16 == 0x2323 || v16 == 0x2424 {
							fmt.Fprintf(e.Logger, "    switch(%s)\n", t)

							// set absolute x,y coordinates to the tile:
							x, y := t.ToAbsCoord(room.Supertile)
							write16(room.WRAM[:], 0x20, y)
							write16(room.WRAM[:], 0x22, x)
							write16(room.WRAM[:], 0xEE, (uint16(t)&0x1000)>>10)

							if room.HandleRoomTags(e) {
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
		}

		// render all supertiles found:
		if len(g.Rooms) >= 1 {
			for _, room := range g.Rooms {
				if false {
					// loadSupertile:
					copy(e.WRAM[:], room.WRAM[:])
					write16(e.WRAM[:], 0xA0, uint16(room.Supertile))
					if err = e.ExecAt(loadSupertilePC, donePC); err != nil {
						panic(err)
					}
					copy((&room.VRAMTileSet)[:], (&e.VRAM)[0x4000:0x8000])
					copy((&room.WRAM)[:], (&e.WRAM)[:])
				}

				{
					fmt.Fprintf(e.Logger, "  render %s\n", room.Supertile)

					wg.Add(1)
					go drawSupertile(&wg, room)

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
		}
	}

	wg.Wait()

	fmt.Printf("rooms := map[uint8][]uint16{\n")
	for _, g := range entranceGroups {
		sts := make([]uint16, 0, 0x100)
		for _, r := range g.Rooms {
			sts = append(sts, uint16(r.Supertile))
		}
		fmt.Printf("\t%#v: %#v,\n", g.EntranceID, sts)
	}
	fmt.Printf("}\n")

	// condense all maps into one image:
	renderAll("eg1", entranceGroups, 0x00, 0x10)
	renderAll("eg2", entranceGroups, 0x10, 0x3)
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

	Rooms      []*RoomState
	Supertiles map[Supertile]*RoomState
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
