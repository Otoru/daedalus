package daedalus

// V1 product limits. MaxCells is 65536, so a Grid stops at 256×256. MaxRooms is
// 256. MaxFootprintCells is 4096. They apply equally to the SDK and the gRPC
// service: a Config exceeding Width, Height, the Cell product, MaxRooms, or
// one Room's footprint fails with ErrLimitExceeded before any allocation,
// generation, or RNG consumption and is never silently truncated.
const (
	// MaxCells is the maximum Width × Height product of a Grid: 65,536 Cells,
	// corresponding to a Grid of up to 256 × 256.
	MaxCells = 65536
	// MaxRooms is the maximum number of Rooms per Layout: 256. It protects
	// on-demand generation latency; it is not a request to fill the Grid.
	MaxRooms = 256
	// MaxFootprintCells is the maximum number of Cells occupied by one Room.
	MaxFootprintCells = 4096
)

// Config is the complete input to a dungeon-generation request. It exists only
// as a Go value passed to the SDK or as a protobuf message in a gRPC request;
// there is no configuration file, human-authored JSON/YAML, or preset.
//
// Width, Height, and Seed are required and have no safe defaults. The other
// fields have defaults documented per field; the zero Config is not valid. An
// invalid Config fails with ErrInvalidConfig (or ErrLimitExceeded when it
// exceeds product limits) before any generation.
type Config struct {
	// Width is the Grid width in Cells: range 1..256, with Width × Height ≤
	// MaxCells. Required.
	Width uint32
	// Height is the Grid height in Cells: range 1..256, with Width × Height ≤
	// MaxCells. Required.
	Height uint32
	// CellSize is one Cell's size in caller-defined units: finite and > 0.
	// Default 1.0. It is copied only to Layout; no algorithm converts it.
	CellSize float64
	// Seed is the source of the request's random streams. Required; every uint64
	// value is accepted, and zero does not mean random.
	Seed Seed
	// MinDistance is the minimum Euclidean distance, in Cells, between Room
	// centers: finite and ≥ 1.0. Default 6.0.
	MinDistance float64
	// MaxAttempts is the maximum number of Poisson candidates per active point:
	// range 1..1024. Default 30.
	MaxAttempts uint32
	// MaxRooms is the maximum number of accepted Rooms: range 1..MaxRooms
	// (product limit). Default 256. Placement stops when it is reached.
	MaxRooms uint32
	// CorridorOrder selects the preferred L-route bend. Default
	// CorridorOrderXThenY (the zero value).
	CorridorOrder CorridorOrder
	// ExtraEdgeCount is the maximum number of discarded short edges reintroduced
	// after the backbone: range 0..MaxRooms×(MaxRooms-1)/2. Default 0, which
	// disables cycles and guarantees that the graph is a tree.
	ExtraEdgeCount uint32
	// MaxRoomEdges is the largest number of Corridors that may reach one Room.
	// Zero means unlimited, and the Room's geometry is then the only ceiling.
	// It is a ceiling, never a target: the openings actually used are the
	// smaller of this value and the number the perimeter can host. A non-zero
	// value below 2 is ErrInvalidConfig, because one Corridor per Room spans
	// exactly two Rooms and cannot connect a larger floor. Shortcuts requested
	// by ExtraEdgeCount spend the same budget.
	MaxRoomEdges uint32
	// RoomRoleRequests lists declarative thematic-role requests, at most one per
	// RoomRole. The empty default disables thematic Rooms and leaves Role absent
	// from every Room.
	RoomRoleRequests []RoomRoleRequest
	// DensityRegions lists Grid rectangles that replace MinDistance with a local
	// distance. Regions must be non-empty, contained in the Grid, and have no
	// overlapping Cells. The empty default uses uniform MinDistance and disables
	// biomes.
	DensityRegions []DensityRegion
	// RoomGeometry defines Room geometry. Nil requests the default dynamic
	// profile. Each shape carries its own width and height: Rectangle, L, T,
	// and Cross are 3..9 on both axes, and Circle is 5..9 on both axes, each
	// axis clamped with min(profile minimum, grid side)..min(profile maximum,
	// grid side). MaxFootprintCells is 81, MinRoomGap is 1, and the weights
	// are Rectangle 4, L 2, T 2, Cross 1, Circle 2. A shape whose clamped
	// range has no legal mask is omitted. Rectangle, including the 1×1 mask
	// on a 1×1 Grid, always remains.
	RoomGeometry *RoomGeometry
	// CorridorGeometry defines the width distribution for Corridors. Nil means
	// every Corridor is one Cell wide and the width stream is never consumed,
	// which keeps the Layout identical to the historical one-Cell routes.
	CorridorGeometry *CorridorGeometry
	// PlantCatalog is the optional asset-metadata catalog, or nil when absent; in
	// that case PlantID and Tags remain empty in the Layout.
	PlantCatalog *PlantCatalog
}

// RoomGeometry defines area, spacing, and per-shape dimension ranges for Rooms
// accepted by a request. Width and height belong to each RoomShapeWeight.
type RoomGeometry struct {
	// MaxFootprintCells limits the Cells occupied by a single Room.
	MaxFootprintCells uint32
	// MinRoomGap is the minimum number of empty layers between footprints,
	// measured by Chebyshev distance between occupied Cells.
	MinRoomGap uint32
	// Shapes lists positive weights by shape, with no duplicate shapes. Each
	// entry carries the width and height ranges that apply to that shape.
	Shapes []RoomShapeWeight
}

// DimensionRange is an inclusive span of bounding-box sizes, in Cells.
// Max 0, or Min greater than Max, is not a span. The zero value is not the
// meaning of an omitted wire range; that omission is filled with
// DefaultDimensionRanges before validation sees the shape.
type DimensionRange struct {
	// Min is the smallest permitted size in Cells, inclusive.
	Min uint32
	// Max is the largest permitted size in Cells, inclusive.
	Max uint32
}

// CorridorGeometry defines the width distribution for Corridors.
// A nil pointer means every Corridor is one Cell wide.
type CorridorGeometry struct {
	// Widths lists positive weights by corridor width, with no duplicate widths.
	// The list must be non-empty when CorridorGeometry itself is non-nil.
	Widths []CorridorWidthWeight
}

// CorridorWidthWeight associates a corridor width, in Cells, with a positive
// selection weight. Width is 1..64. Weight is 1..2^32-1.
type CorridorWidthWeight struct {
	// Width is the corridor width in Cells, from 1 to 64 inclusive.
	Width uint32
	// Weight is the positive relative weight used for width selection.
	Weight uint32
}

// RoomShapeWeight associates a Shape with a positive selection weight and the
// width and height spans legal for that shape.
type RoomShapeWeight struct {
	// Shape is one of the canonical Room shapes.
	Shape RoomShape
	// Weight is the positive relative weight used for shape selection.
	Weight uint32
	// Width is the inclusive bounding-box width span, in Cells.
	Width DimensionRange
	// Height is the inclusive bounding-box height span, in Cells.
	Height DimensionRange
}

// RoomRoleRequest is a declarative request to assign a RoomRole and the Plant
// tags required for Rooms receiving it.
type RoomRoleRequest struct {
	// Role is the requested thematic role: Start, Boss, or Treasure.
	Role RoomRole
	// Count is the number of Rooms receiving the role: 1 for Start and Boss;
	// 0..MaxRooms for Treasure. Boss requires a Start request in the same Config
	// because distance without an origin is undefined.
	Count uint32
	// RequiredTags lists non-empty, non-duplicate UTF-8 tags that the selected
	// Plant must contain. It restricts catalog selection but does not change the
	// topological Room choice.
	RequiredTags []string
}

// DensityRegion is a Grid rectangle that replaces Config.MinDistance with a
// local distance, allowing denser or sparser biomes. The rectangle is
// half-open: Min is inclusive and Max is exclusive, so the region covers X in
// [Min.X, Max.X) and Y in [Min.Y, Max.Y). Thus Max may equal the Grid
// dimension, a strictly greater Max is invalid, and a single-Cell region is
// written with Max = Min + (1,1). The Placer's membership test uses exactly
// this convention.
type DensityRegion struct {
	// Min is the inclusive minimum corner of the rectangle, in Cells.
	Min Cell
	// Max is the exclusive maximum corner of the rectangle, in Cells. It must be
	// strictly greater than Min on both axes and within the Grid.
	Max Cell
	// MinDistance is the local minimum Euclidean distance in Cells: finite and
	// ≥ 1.0.
	MinDistance float64
}

// PlantCatalog is the engine-agnostic asset-metadata catalog. It stores only
// data: resolving PlantID to a scene, prefab, tile, or mesh belongs to the
// calling game. When present in Config, both lists must be non-empty with
// unique IDs.
type PlantCatalog struct {
	// Rooms lists Plants available for Rooms; non-empty when the catalog is present.
	Rooms []RoomPlant
	// Corridors lists Plants available for Corridors; non-empty when the catalog
	// is present.
	Corridors []CorridorPlant
}

// RoomPlant describes metadata for a catalog Room Plant.
type RoomPlant struct {
	// ID is the opaque asset identifier: required, non-empty UTF-8, and unique
	// within the catalog.
	ID PlantID
	// Tags lists non-empty, non-duplicate UTF-8 tags; it may be empty.
	Tags []string
	// Weight is the relative weighted-selection weight: range 1..2^32-1.
	Weight uint32
	// DoorDirections declares the opening Directions supported by the asset:
	// non-empty, with no duplicates. A RoomPlant is compatible with a Room only
	// when DoorDirections is a superset of the Directions used by that Room's Doors.
	DoorDirections []Direction
}

// CorridorPlant describes metadata for a catalog Corridor Plant. It declares
// no local geometry because that derives from Corridor.Cells.
type CorridorPlant struct {
	// ID is the opaque asset identifier: required, non-empty UTF-8, and unique
	// within the catalog.
	ID PlantID
	// Tags lists non-empty, non-duplicate UTF-8 tags; it may be empty.
	Tags []string
	// Weight is the relative weighted-selection weight: range 1..2^32-1.
	Weight uint32
}
