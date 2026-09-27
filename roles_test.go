package daedalus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPapéisVaziosNãoAtribuemNemConsomemSorteio(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 3, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}}
	streams := newRNGStreams(Seed(91))
	wantStreams := streams

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, nil)

	require.NoError(t, err)
	require.Len(t, roles, len(rooms))
	assert.Nil(t, roles[0])
	assert.Nil(t, roles[1])
	assert.Equal(t, wantStreams, streams)
}

func TestStartRecebeSempreRoomIDZero(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 7, 7), placedRoomAt(1, 0, 0)}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[0])
	assert.Equal(t, RoomRoleStart, *roles[0])
	assert.Nil(t, roles[1])
}

func TestBossUsaSomaPonderadaDoCaminhoNaÁrvore(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 10, 0),
		placedRoomAt(2, 1, 0),
		placedRoomAt(3, 20, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 3},
		{FromRoomID: 3, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[2])
	assert.Equal(t, RoomRoleBoss, *roles[2], "a Room 1 é mais distante em linha reta, mas a Room 2 tem caminho ponderado maior")
}

func TestPapéisUsamCentrosDasBoundingBoxes(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		{
			ID: 1, At: Cell{X: 1, Y: 0}, Shape: RoomShapeRectangle,
			Origin: Cell{X: 1, Y: 0}, Width: 19, Height: 1,
		},
		placedRoomAt(2, 9, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
	assert.Nil(t, roles[2], "a métrica por âncora escolheria a Room 2")
}

func TestPapéisDesempatamDistânciaPorRoomIDCrescente(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, -2, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	require.NotNil(t, roles[1])
	assert.Equal(t, RoomRoleBoss, *roles[1])
	assert.Nil(t, roles[2])
}

func TestTreasureEscolhePróximasMaisDistantesSemRepetir(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 3, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 2},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 1, RoomRoleTreasure)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
	assertRoleAt(t, roles, 3, RoomRoleBoss)
}

func TestTreasureEBossRespeitamAOrdemDasSolicitações(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleTreasure, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, backbone, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
	assertRoleAt(t, roles, 2, RoomRoleTreasure)
}

func TestMenosRoomsQuePapéisAtribuiOQueCouberSemDuplicar(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}
	requests := []RoomRoleRequest{
		{Role: RoomRoleStart, Count: 1},
		{Role: RoomRoleBoss, Count: 1},
		{Role: RoomRoleTreasure, Count: 8},
	}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, requests)

	require.NoError(t, err)
	assertRoleAt(t, roles, 0, RoomRoleStart)
	assertRoleAt(t, roles, 1, RoomRoleBoss)
}

func TestBossSemStartÉErroDeProgramação(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}

	roles, err := assignRoomRoles(context.Background(), rooms, []Connection{{FromRoomID: 0, ToRoomID: 1}}, []RoomRoleRequest{{Role: RoomRoleBoss, Count: 1}})

	assert.ErrorIs(t, err, errBossRoleWithoutStart)
	assert.Nil(t, roles)
}

func TestZeroAtalhosPreservaÁrvoreESaltaFaseSemSorteio(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0), placedRoomAt(2, 2, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 2}}
	streams := newRNGStreams(Seed(92))
	wantStreams := streams

	connections, err := addExtraConnections(context.Background(), rooms, backbone, 0)

	require.NoError(t, err)
	assert.Equal(t, backbone, connections)
	assertConnectionsFormTree(t, rooms, connections)
	assert.Equal(t, wantStreams, streams)
}

func TestAtalhosSãoAsPrimeirasArestasDescartadasOrdenadas(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 2, 0),
		placedRoomAt(2, 0, 2),
		placedRoomAt(3, 2, 2),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
	}

	connections, err := addExtraConnections(context.Background(), rooms, backbone, 2)

	require.NoError(t, err)
	assert.Equal(t, []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 0, ToRoomID: 2},
		{FromRoomID: 1, ToRoomID: 3},
		{FromRoomID: 2, ToRoomID: 3},
		{FromRoomID: 0, ToRoomID: 3},
	}, connections)
	assertGraphConnected(t, rooms, connections)
}

func TestAtalhosAcimaDoDisponívelAcrescentamTodosSemErro(t *testing.T) {
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0), placedRoomAt(2, 2, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}, {FromRoomID: 1, ToRoomID: 2}}

	connections, err := addExtraConnections(context.Background(), rooms, backbone, 99)

	require.NoError(t, err)
	assert.Equal(t, append(append([]Connection(nil), backbone...), Connection{FromRoomID: 0, ToRoomID: 2}), connections)
}

func TestPapéisSãoCalculadosAntesDosAtalhos(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
		placedRoomAt(3, 0, 1),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
		{FromRoomID: 2, ToRoomID: 3},
	}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1}}

	roles, connections, err := applyTopologyOptions(context.Background(), rooms, backbone, requests, 1)

	require.NoError(t, err)
	assertRoleAt(t, roles, 3, RoomRoleBoss)
	assert.Contains(t, connections, Connection{FromRoomID: 0, ToRoomID: 3}, "o atalho encurta o caminho até o Boss somente depois da atribuição")
}

func TestPapéisEAtalhosHabilitadosNãoConsomemSorteio(t *testing.T) {
	rooms := []PlacedRoom{
		placedRoomAt(0, 0, 0),
		placedRoomAt(1, 1, 0),
		placedRoomAt(2, 2, 0),
	}
	backbone := []Connection{
		{FromRoomID: 0, ToRoomID: 1},
		{FromRoomID: 1, ToRoomID: 2},
	}
	requests := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1}}
	streams := newRNGStreams(Seed(93))
	wantStreams := streams

	roles, connections, err := applyTopologyOptions(context.Background(), rooms, backbone, requests, 1)

	require.NoError(t, err)
	assertRoleAt(t, roles, 2, RoomRoleBoss)
	assert.Len(t, connections, 3)
	assert.Equal(t, wantStreams, streams)
}

func TestFasesDePapéisEAtalhosRespeitamContextCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rooms := []PlacedRoom{placedRoomAt(0, 0, 0), placedRoomAt(1, 1, 0)}
	backbone := []Connection{{FromRoomID: 0, ToRoomID: 1}}

	roles, roleErr := assignRoomRoles(ctx, rooms, backbone, []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}})
	connections, edgeErr := addExtraConnections(ctx, rooms, backbone, 1)

	assert.ErrorIs(t, roleErr, context.Canceled)
	assert.Nil(t, roles)
	assert.ErrorIs(t, edgeErr, context.Canceled)
	assert.Nil(t, connections)
}

func assertRoleAt(t *testing.T, roles []*RoomRole, index int, want RoomRole) {
	t.Helper()
	require.NotNil(t, roles[index])
	assert.Equal(t, want, *roles[index])
}

func assertGraphConnected(t *testing.T, rooms []PlacedRoom, connections []Connection) {
	t.Helper()
	visited := make([]bool, len(rooms))
	visited[0] = true
	for changed := true; changed; {
		changed = false
		for _, connection := range connections {
			from := int(connection.FromRoomID)
			to := int(connection.ToRoomID)
			if visited[from] == visited[to] {
				continue
			}
			visited[from] = true
			visited[to] = true
			changed = true
		}
	}
	for roomIndex := range rooms {
		assert.True(t, visited[roomIndex], "Room %d precisa permanecer alcançável", roomIndex)
	}
}
