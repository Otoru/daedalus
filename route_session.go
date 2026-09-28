package daedalus

import (
	"context"
	"fmt"
)

// reservedRoute is one corridor already placed on the session occupancy.
// blocked lists every Cell this reservation marked, band and halo, so a splice
// can release exactly those Cells and no others.
type reservedRoute struct {
	connection Connection
	from       doorOpening
	to         doorOpening
	centerline []Cell
	cells      []Cell
	blocked    []Cell
	width      uint32
}

// routeSession is the reservation oracle Generator installs on ConnectionRequest.
// A successful tryCommit routes the pair and reserves its band and halo. A
// failure reserves nothing and puts the width stream back where it was, so a
// skipped candidate does not consume the draw that belongs to the next corridor.
type routeSession struct {
	ctx            context.Context
	order          CorridorOrder
	rooms          []PlacedRoom
	openings       [][]doorOpening
	occupancy      *placementOccupancy
	search         *routingSearch
	corridorWidths []CorridorWidthWeight
	widthStream    *splitMix64
	useWidths      bool
	reserved       []reservedRoute
}

type routeSnapshot struct {
	owners    []uint32
	reserved  []reservedRoute
	stream    uint64
	hasStream bool
}

func newRouteSession(
	ctx context.Context,
	width uint32,
	height uint32,
	order CorridorOrder,
	rooms []PlacedRoom,
	corridorWidths []CorridorWidthWeight,
	widthStream *splitMix64,
) (*routeSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	occupancy := newPlacementOccupancy(width, height)
	openings := make([][]doorOpening, len(rooms))
	for roomIndex, room := range rooms {
		occupancy.mark(uint32(roomIndex), room.Cells)
	}
	for roomIndex, room := range rooms {
		openings[roomIndex] = enumerateDoorOpenings(room, uint32(roomIndex), occupancy)
	}
	search := newRoutingSearch(ctx, occupancy)
	useWidths := len(corridorWidths) > 0
	if useWidths {
		if widthStream == nil {
			return nil, errGeneratorInvariant
		}
		search.clearance = newWidthClearance(occupancy)
	}
	return &routeSession{
		ctx: ctx, order: order, rooms: rooms, openings: openings,
		occupancy: occupancy, search: search, corridorWidths: corridorWidths,
		widthStream: widthStream, useWidths: useWidths,
	}, nil
}

func openRouteSession(ctx context.Context, effective effectiveConfig, rooms []PlacedRoom) (*routeSession, error) {
	var widthStream *splitMix64
	if len(effective.corridorWidths) > 0 {
		streams := newRNGStreams(effective.seed)
		widthStream = &streams.corridorWidth
	}
	return newRouteSession(
		ctx, effective.width, effective.height, effective.corridorOrder,
		rooms, effective.corridorWidths, widthStream,
	)
}

// tryCommit routes from→to. Success reserves the band and the one-Cell
// Chebyshev halo, including Cells beside a Room. Failure leaves the occupancy
// and the width stream unchanged.
func (session *routeSession) tryCommit(fromID, toID RoomID) (bool, error) {
	if session.indexOf(Connection{FromRoomID: fromID, ToRoomID: toID}) >= 0 {
		return true, nil
	}
	if err := session.ctx.Err(); err != nil {
		return false, err
	}
	fromIndex := roomIndexByID(session.rooms, fromID)
	toIndex := roomIndexByID(session.rooms, toID)
	if fromIndex == topologyNoRoomIndex || toIndex == topologyNoRoomIndex {
		return false, errTopologyUnknownRoom
	}

	streamState, streamSaved := session.streamState()
	best, routedWidth, found, err := session.routePair(fromIndex, toIndex)
	if err != nil || !found {
		session.restoreStream(streamState, streamSaved)
		if err != nil {
			return false, err
		}
		return false, nil
	}
	if !session.useWidths {
		routedWidth = 1
	}
	centerline := append([]Cell(nil), best.cells...)
	cells := centerline
	if session.useWidths {
		cells = occupyBand(best.cells, best.from.direction, best.to.direction, routedWidth)
	}
	blocked := blockCorridorHalo(session.occupancy, cells)
	session.rebuildClearance()
	session.reserved = append(session.reserved, reservedRoute{
		connection: Connection{FromRoomID: fromID, ToRoomID: toID},
		from:       best.from, to: best.to,
		centerline: centerline, cells: append([]Cell(nil), cells...),
		blocked: blocked, width: routedWidth,
	})
	return true, nil
}

func (session *routeSession) routePair(fromIndex, toIndex int) (routedConnection, uint32, bool, error) {
	if !session.useWidths {
		best, found, err := selectRoutedConnection(
			session.openings[fromIndex], session.openings[toIndex], session.order, session.search, 0,
		)
		return best, 0, found, err
	}
	drawn := drawCorridorWidth(session.corridorWidths, session.widthStream)
	return routeDegraded(
		session.openings[fromIndex], session.openings[toIndex], session.order,
		drawn, session.corridorWidths, session.search,
	)
}

// rewire releases one reserved edge and reserves two replacements. Any failure
// restores the released edge, the Cells it owned, and the width stream.
func (session *routeSession) rewire(remove, first, second Connection) (bool, error) {
	snapshot := session.snapshot()
	if !session.drop(remove) {
		return false, fmt.Errorf("%w: splice released an edge that was not reserved", errGeneratorInvariant)
	}
	ok, err := session.tryCommit(first.FromRoomID, first.ToRoomID)
	if err != nil || !ok {
		session.restore(snapshot)
		return false, err
	}
	ok, err = session.tryCommit(second.FromRoomID, second.ToRoomID)
	if err != nil || !ok {
		session.restore(snapshot)
		return false, err
	}
	return true, nil
}

// ensureCommitted routes every edge that the Connector returned without
// reserving. An edge that does not fit is ErrUnroutableEdge: the Connector
// never negotiated it away.
func (session *routeSession) ensureCommitted(connections []Connection) error {
	for _, connection := range connections {
		if session.indexOf(connection) >= 0 {
			continue
		}
		ok, err := session.tryCommit(connection.FromRoomID, connection.ToRoomID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf(
				"%w: RoomID %d to RoomID %d",
				ErrUnroutableEdge, connection.FromRoomID, connection.ToRoomID,
			)
		}
	}
	return nil
}

func (session *routeSession) materialize(connections []Connection) ([]Corridor, []Door, error) {
	if err := session.ctx.Err(); err != nil {
		return nil, nil, err
	}
	corridors := make([]Corridor, 0, len(connections))
	doors := make([]Door, 0, len(connections)*2)
	for connectionIndex, connection := range connections {
		record, ok := session.lookup(connection)
		if !ok {
			return nil, nil, fmt.Errorf(
				"%w: reserved route missing for RoomID %d to RoomID %d",
				errGeneratorInvariant, connection.FromRoomID, connection.ToRoomID,
			)
		}
		fromOpening := record.from
		toOpening := record.to
		if connection.FromRoomID != record.connection.FromRoomID {
			fromOpening, toOpening = toOpening, fromOpening
		}
		corridorID := CorridorID(connectionIndex)
		fromDoorID := getOrCreateSpannedDoor(&doors, fromOpening, record.width, corridorID)
		toDoorID := getOrCreateSpannedDoor(&doors, toOpening, record.width, corridorID)
		corridors = append(corridors, Corridor{
			ID: corridorID, FromRoomID: connection.FromRoomID, ToRoomID: connection.ToRoomID,
			FromDoorID: fromDoorID, ToDoorID: toDoorID,
			Centerline: append([]Cell(nil), record.centerline...),
			Cells:      append([]Cell(nil), record.cells...),
		})
	}
	return corridors, doors, nil
}

func (session *routeSession) lookup(target Connection) (reservedRoute, bool) {
	index := session.indexOf(target)
	if index < 0 {
		return reservedRoute{}, false
	}
	return session.reserved[index], true
}

func (session *routeSession) indexOf(target Connection) int {
	for index, record := range session.reserved {
		if connectionJoins(record.connection, target.FromRoomID, target.ToRoomID) {
			return index
		}
	}
	return -1
}

func (session *routeSession) drop(target Connection) bool {
	index := session.indexOf(target)
	if index < 0 {
		return false
	}
	session.unmark(session.reserved[index].blocked)
	session.reserved = append(session.reserved[:index], session.reserved[index+1:]...)
	session.rebuildClearance()
	return true
}

func (session *routeSession) unmark(cells []Cell) {
	for _, cell := range cells {
		index, ok := session.occupancy.index(cell)
		if !ok {
			continue
		}
		session.occupancy.owners[index] = 0
	}
}

func (session *routeSession) rebuildClearance() {
	if session.search.clearance != nil {
		session.search.clearance = newWidthClearance(session.occupancy)
	}
}

func (session *routeSession) streamState() (uint64, bool) {
	if session.widthStream == nil {
		return 0, false
	}
	return session.widthStream.state, true
}

func (session *routeSession) restoreStream(state uint64, saved bool) {
	if !saved {
		return
	}
	session.widthStream.state = state
}

func (session *routeSession) snapshot() routeSnapshot {
	snap := routeSnapshot{
		owners:   append([]uint32(nil), session.occupancy.owners...),
		reserved: append([]reservedRoute(nil), session.reserved...),
	}
	snap.stream, snap.hasStream = session.streamState()
	return snap
}

func (session *routeSession) restore(snap routeSnapshot) {
	copy(session.occupancy.owners, snap.owners)
	session.reserved = snap.reserved
	session.restoreStream(snap.stream, snap.hasStream)
	session.rebuildClearance()
}
