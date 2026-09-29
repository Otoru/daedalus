package gating

import daedalus "github.com/Otoru/daedalus"

const MaxGates = daedalus.MaxRooms - 1

const (
	maxSimpleCorridors = uint64(daedalus.MaxRooms) * uint64(daedalus.MaxRooms-1) / 2
	maxDoors           = 2 * maxSimpleCorridors
)
