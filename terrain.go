package daedalus

import (
	"fmt"
	"unicode/utf8"
)

const (
	// MaxTerrainKinds is the number of palette entries addressable by one
	// non-zero index byte.
	MaxTerrainKinds = 255
	// MaxTerrainPaletteBytes bounds the aggregate UTF-8 bytes in one palette.
	MaxTerrainPaletteBytes = MaxCells
)

// TerrainID is an open, caller-defined terrain identifier. Daedalus does not
// assign meaning to an ID; EntryCost and Transparent are the only generic
// properties carried by the standalone layer.
type TerrainID string

// TerrainDefinition describes one caller-defined terrain. EntryCost is zero
// for an impassable terrain and 1..255 for an enterable terrain. Transparency
// is independent of entry cost: glass may be impassable and transparent, and
// smoke may be passable and opaque.
type TerrainDefinition struct {
	ID          TerrainID
	EntryCost   uint8
	Transparent bool
}

// TerrainLayer is a compact, palette-indexed overlay in Grid cell order.
// Indices are row-major: index y*Grid.Width+x is zero for no terrain and
// otherwise addresses Palette[index-1]. A cell has one terrain at most; a
// composite ID is the deliberate escape hatch for effects such as a submerged
// trap. Empty base cells are not made floor by this overlay.
type TerrainLayer struct {
	// Palette is strictly sorted by the byte order of TerrainDefinition.ID.
	// Every declared definition remains in the palette, even if unused.
	Palette []TerrainDefinition
	// Indices contains exactly Grid.Width*Grid.Height bytes when the layer is
	// present. Missing and out-of-range data is treated as no terrain by lookup,
	// but rejected by Validate.
	Indices []byte
}

// TerrainAt returns the definition selected at at. Lookup fails closed: nil,
// index zero, coordinates outside Grid, a short index slice, and an out of
// range palette index all mean no terrain.
func (grid Grid) TerrainAt(at Cell) (*TerrainDefinition, bool) {
	if grid.Terrain == nil {
		return nil, false
	}
	if at.X < 0 || at.Y < 0 || uint32(at.X) >= grid.Width || uint32(at.Y) >= grid.Height {
		return nil, false
	}
	row := uint64(at.Y) * uint64(grid.Width)
	index := row + uint64(at.X)
	if index >= uint64(len(grid.Terrain.Indices)) {
		return nil, false
	}
	paletteIndex := grid.Terrain.Indices[index]
	if paletteIndex == 0 {
		return nil, false
	}
	paletteOffset := int(paletteIndex) - 1
	if paletteOffset >= len(grid.Terrain.Palette) {
		return nil, false
	}
	return &grid.Terrain.Palette[paletteOffset], true
}

// TerrainIDAt returns the selected terrain ID at at, or false when the cell
// has no valid terrain selection.
func (grid Grid) TerrainIDAt(at Cell) (TerrainID, bool) {
	definition, ok := grid.TerrainAt(at)
	if !ok {
		return "", false
	}
	return definition.ID, true
}

// DefinitionAt returns the palette definition selected by a wire index. Zero
// means no terrain and invalid indices fail closed.
func (layer TerrainLayer) DefinitionAt(index byte) (*TerrainDefinition, bool) {
	if index == 0 {
		return nil, false
	}
	offset := int(index) - 1
	if offset >= len(layer.Palette) {
		return nil, false
	}
	return &layer.Palette[offset], true
}

// Clone returns a detached layer. Nil remains nil, while both palette and
// index backing arrays are copied so writes cannot cross the clone boundary.
func (layer *TerrainLayer) Clone() *TerrainLayer {
	if layer == nil {
		return nil
	}
	clone := &TerrainLayer{
		Palette: make([]TerrainDefinition, len(layer.Palette)),
		Indices: make([]byte, len(layer.Indices)),
	}
	copy(clone.Palette, layer.Palette)
	copy(clone.Indices, layer.Indices)
	if layer.Palette == nil {
		clone.Palette = nil
	}
	if layer.Indices == nil {
		clone.Indices = nil
	}
	return clone
}

// ValidateTerrain validates the optional Grid layer. Nil is the compatibility
// default and is valid; a present layer must describe this Grid exactly.
func (grid Grid) ValidateTerrain() error {
	if grid.Terrain == nil {
		return nil
	}
	return grid.Terrain.Validate(grid.Width, grid.Height)
}

// ValidateForGrid is the explicit form of validating a layer against a Grid.
func (layer *TerrainLayer) ValidateForGrid(grid Grid) error {
	return layer.Validate(grid.Width, grid.Height)
}

// Validate checks the standalone layer before a consumer uses it. Palette
// order is part of the representation contract, so this method does not sort
// or copy input. A nil layer is valid; an empty present layer is not another
// spelling of nil.
func (layer *TerrainLayer) Validate(width, height uint32) error {
	if layer == nil {
		return nil
	}
	if width == 0 || height == 0 {
		return terrainError("dimensions %dx%d are zero", width, height)
	}
	product := uint64(width) * uint64(height)
	if product > uint64(MaxCells) {
		return fmt.Errorf("%w: TerrainLayer has %d cells, above %d", ErrLimitExceeded, product, MaxCells)
	}
	if len(layer.Palette) == 0 {
		return terrainError("palette is empty")
	}
	if len(layer.Palette) > MaxTerrainKinds {
		return fmt.Errorf("%w: palette has %d definitions, above %d", ErrLimitExceeded, len(layer.Palette), MaxTerrainKinds)
	}
	var paletteBytes uint64
	for index, definition := range layer.Palette {
		if definition.ID == "" {
			return terrainError("palette definition %d has an empty ID", index)
		}
		if !utf8.ValidString(string(definition.ID)) {
			return terrainError("palette definition %d has invalid UTF-8", index)
		}
		if index > 0 && layer.Palette[index-1].ID >= definition.ID {
			return terrainError("palette is not strictly sorted at index %d", index)
		}
		paletteBytes += uint64(len(string(definition.ID)))
		if paletteBytes > uint64(MaxTerrainPaletteBytes) {
			return fmt.Errorf("%w: palette IDs use %d bytes, above %d", ErrLimitExceeded, paletteBytes, MaxTerrainPaletteBytes)
		}
	}
	if uint64(len(layer.Indices)) != product {
		return terrainError("index length %d disagrees with grid cell count %d", len(layer.Indices), product)
	}
	for index, paletteIndex := range layer.Indices {
		if int(paletteIndex) > len(layer.Palette) {
			return terrainError("index %d selects palette entry %d, palette length is %d", index, paletteIndex, len(layer.Palette))
		}
	}
	return nil
}

func terrainError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidTerrain, fmt.Sprintf(format, args...))
}
