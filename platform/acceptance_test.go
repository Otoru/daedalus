package platform

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// qualityUpdate rewrites platform fixtures. It is not a way to turn a red
// test green: a mismatch means the frozen map changed, and the root's rule
// in CONTRIBUTING.md applies here too.
var qualityUpdate = flag.Bool("update-platform", false, "rewrite platform golden and portrait fixtures after explicit review")

// TestFrozenPlatformGoldens compares each named seed's PlatformLayout to a
// fixture field by field. One machine does not prove the same bytes on native
// amd64 and arm64; that takes this test and these fixtures on both runners.
// The log line records the arch that actually executed.
//
// The fixture is gzip of a JSON document because a certified jump graph
// carries a witness on every edge. Gzip is only the envelope. The comparison
// is the decoded struct, and a mismatch names the first field path.
func TestFrozenPlatformGoldens(t *testing.T) {
	t.Logf("platform golden arch %s/%s", runtime.GOOS, runtime.GOARCH)
	for _, testCase := range qualityGoldenCases() {
		t.Run(testCase.name, func(t *testing.T) {
			layout := qualityGenerateCertified(t, testCase.seed)
			payload, err := qualityLayoutBytes(layout)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			roundTrip, err := qualityLayoutFromBytes(payload)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if msg := qualityLayoutDifference(layout, roundTrip); msg != "" {
				t.Fatalf("fixture codec dropped a field\n%s", msg)
			}
			if !reflect.DeepEqual(layout, roundTrip) {
				t.Fatal("fixture codec round-trips with no field path and still differs")
			}

			path := qualityGoldenPath(testCase.name)
			if *qualityUpdate {
				qualityWriteFixture(t, path, payload)
			}
			expected, err := qualityLayoutFromBytes(qualityReadFixture(t, path))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			if msg := qualityLayoutDifference(expected, layout); msg != "" {
				t.Fatalf("PlatformLayout diverged from frozen fixture\n%s\nFrozen output changed; decide consciously whether to regenerate with go test ./platform -run TestFrozenPlatformGoldens -update-platform. This is not an automatic fix.", msg)
			}
		})
	}
}

// TestPlatformGoldenCodecKeepsNonFiniteFloats is the guard on the envelope.
// DefaultProfile stores +Inf sentinels. encoding/json rejects those, so a
// codec that quietly dropped them would still hash and still compare equal
// to itself.
func TestPlatformGoldenCodecKeepsNonFiniteFloats(t *testing.T) {
	layout := qualityCertified(t)
	if !math.IsInf(layout.Config.Profile.MaxSafeFallHeight, 1) {
		t.Fatal("the fixture profile no longer carries +Inf, so this test no longer guards the codec")
	}
	payload, err := qualityLayoutBytes(layout)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := qualityLayoutFromBytes(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !math.IsInf(decoded.Config.Profile.MaxSafeFallHeight, 1) {
		t.Fatalf("MaxSafeFallHeight round-tripped as %v", decoded.Config.Profile.MaxSafeFallHeight)
	}
	if msg := qualityLayoutDifference(layout, decoded); msg != "" {
		t.Fatalf("codec round trip\n%s", msg)
	}
}

// TestPlatformGoldenDiagnosticNamesTheField checks that a one-field change is
// reported as that field. A comparator that only hashed the gzip would not
// name it.
func TestPlatformGoldenDiagnosticNamesTheField(t *testing.T) {
	expected := PlatformLayout{JumpGraph: JumpGraph{Edges: []MotionEdge{{ID: 1, Duration: 1}}}}
	actual := PlatformLayout{JumpGraph: JumpGraph{Edges: []MotionEdge{{ID: 1, Duration: 4}}}}
	diagnostic := qualityLayoutDifference(expected, actual)
	if !strings.Contains(diagnostic, "PlatformLayout.JumpGraph.Edges[0].Duration") {
		t.Fatalf("diagnostic = %q", diagnostic)
	}
	if !strings.Contains(diagnostic, "expected=1") || !strings.Contains(diagnostic, "actual=4") {
		t.Fatalf("diagnostic = %q", diagnostic)
	}
}

func qualityGoldenCases() []struct {
	name string
	seed Seed
} {
	return []struct {
		name string
		seed Seed
	}{
		{name: "gated-seed-11", seed: 11},
		{name: "gated-seed-7", seed: 7},
	}
}

func qualityGenerateCertified(t *testing.T, seed Seed) PlatformLayout {
	t.Helper()
	layout, err := Generate(context.Background(), NewM1Oracle(), qualityConfig(seed))
	if err != nil {
		t.Fatalf("generate seed %d: %v", seed, err)
	}
	if !layout.Judgement.Certified() {
		t.Fatalf("seed %d is %s/%s (%s); a golden of an uncertified map freezes a failure",
			seed, layout.Judgement.Verdict, layout.Judgement.Reason, layout.Judgement.Detail)
	}
	return layout
}

func qualityGoldenPath(name string) string {
	return filepath.Join("testdata", "golden", name+".json.gz")
}

func qualityWriteFixture(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func qualityReadFixture(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture missing; regenerate explicitly with go test ./platform -run TestFrozenPlatformGoldens -update-platform: %v", err)
	}
	return payload
}

func qualityLayoutBytes(layout PlatformLayout) ([]byte, error) {
	tree, err := qualityEncodeValue(reflect.ValueOf(layout))
	if err != nil {
		return nil, err
	}
	var raw bytes.Buffer
	encoder := json.NewEncoder(&raw)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(tree); err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw.Bytes()); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}

func qualityLayoutFromBytes(payload []byte) (PlatformLayout, error) {
	var zero PlatformLayout
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return zero, err
	}
	decoded, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		return zero, err
	}
	if closeErr != nil {
		return zero, closeErr
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return zero, err
	}
	var layout PlatformLayout
	if err := qualityAssign(reflect.ValueOf(&layout).Elem(), tree); err != nil {
		return zero, err
	}
	return layout, nil
}

func qualityEncodeValue(value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil, nil
		}
		return qualityEncodeValue(value.Elem())
	case reflect.Struct:
		encoded := make(map[string]any, value.NumField())
		structType := value.Type()
		for field := 0; field < value.NumField(); field++ {
			child, err := qualityEncodeValue(value.Field(field))
			if err != nil {
				return nil, err
			}
			encoded[structType.Field(field).Name] = child
		}
		return encoded, nil
	case reflect.Slice:
		if value.IsNil() {
			return nil, nil
		}
		encoded := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			child, err := qualityEncodeValue(value.Index(index))
			if err != nil {
				return nil, err
			}
			encoded[index] = child
		}
		return encoded, nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return strconv.FormatFloat(number, 'f', -1, 64), nil
		}
		return number, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint(), nil
	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		return value.Bool(), nil
	default:
		return nil, fmt.Errorf("cannot freeze %s", value.Type())
	}
}

func qualityAssign(target reflect.Value, tree any) error {
	if !target.IsValid() {
		return fmt.Errorf("assign into an invalid value")
	}
	switch target.Kind() {
	case reflect.Pointer:
		return qualityAssignPointer(target, tree)
	case reflect.Struct:
		return qualityAssignStruct(target, tree)
	case reflect.Slice:
		return qualityAssignSlice(target, tree)
	case reflect.Float32, reflect.Float64:
		return qualityAssignFloat(target, tree)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return qualityAssignInt(target, tree)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return qualityAssignUint(target, tree)
	case reflect.String:
		text, ok := tree.(string)
		if !ok {
			return fmt.Errorf("string: got %T", tree)
		}
		target.SetString(text)
		return nil
	case reflect.Bool:
		bit, ok := tree.(bool)
		if !ok {
			return fmt.Errorf("bool: got %T", tree)
		}
		target.SetBool(bit)
		return nil
	default:
		return fmt.Errorf("cannot restore %s", target.Type())
	}
}

func qualityAssignPointer(target reflect.Value, tree any) error {
	if tree == nil {
		target.SetZero()
		return nil
	}
	child := reflect.New(target.Type().Elem())
	if err := qualityAssign(child.Elem(), tree); err != nil {
		return err
	}
	target.Set(child)
	return nil
}

func qualityAssignStruct(target reflect.Value, tree any) error {
	object, ok := tree.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: got %T", target.Type(), tree)
	}
	structType := target.Type()
	for field := 0; field < target.NumField(); field++ {
		name := structType.Field(field).Name
		child, ok := object[name]
		if !ok {
			return fmt.Errorf("%s.%s missing from fixture", target.Type(), name)
		}
		if err := qualityAssign(target.Field(field), child); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func qualityAssignSlice(target reflect.Value, tree any) error {
	if tree == nil {
		target.SetZero()
		return nil
	}
	items, ok := tree.([]any)
	if !ok {
		return fmt.Errorf("%s: got %T", target.Type(), tree)
	}
	slice := reflect.MakeSlice(target.Type(), len(items), len(items))
	for index := range items {
		if err := qualityAssign(slice.Index(index), items[index]); err != nil {
			return fmt.Errorf("[%d]: %w", index, err)
		}
	}
	target.Set(slice)
	return nil
}

func qualityAssignFloat(target reflect.Value, tree any) error {
	number, err := qualityParseFloat(tree)
	if err != nil {
		return err
	}
	if target.OverflowFloat(number) {
		return fmt.Errorf("%s overflows", target.Type())
	}
	target.SetFloat(number)
	return nil
}

func qualityAssignInt(target reflect.Value, tree any) error {
	number, err := qualityParseInt(tree)
	if err != nil {
		return err
	}
	if target.OverflowInt(number) {
		return fmt.Errorf("%s overflows", target.Type())
	}
	target.SetInt(number)
	return nil
}

func qualityAssignUint(target reflect.Value, tree any) error {
	number, err := qualityParseUint(tree)
	if err != nil {
		return err
	}
	if target.OverflowUint(number) {
		return fmt.Errorf("%s overflows", target.Type())
	}
	target.SetUint(number)
	return nil
}

func qualityParseFloat(tree any) (float64, error) {
	switch number := tree.(type) {
	case json.Number:
		return number.Float64()
	case float64:
		return number, nil
	case string:
		return strconv.ParseFloat(number, 64)
	default:
		return 0, fmt.Errorf("float: got %T", tree)
	}
}

func qualityParseInt(tree any) (int64, error) {
	switch number := tree.(type) {
	case json.Number:
		return number.Int64()
	case int64:
		return number, nil
	case int:
		return int64(number), nil
	default:
		return 0, fmt.Errorf("int: got %T", tree)
	}
}

func qualityParseUint(tree any) (uint64, error) {
	switch number := tree.(type) {
	case json.Number:
		return strconv.ParseUint(number.String(), 10, 64)
	case uint64:
		return number, nil
	case int64:
		if number < 0 {
			return 0, fmt.Errorf("uint: negative %d", number)
		}
		return uint64(number), nil
	case int:
		if number < 0 {
			return 0, fmt.Errorf("uint: negative %d", number)
		}
		return uint64(number), nil
	default:
		return 0, fmt.Errorf("uint: got %T", tree)
	}
}

func qualityLayoutDifference(expected, actual PlatformLayout) string {
	path, want, got, found := qualityFirstDifference("PlatformLayout", reflect.ValueOf(expected), reflect.ValueOf(actual))
	if !found {
		return ""
	}
	return fmt.Sprintf("first divergent path: %s\nscalar expected=%v actual=%v", path, want, got)
}

func qualityFirstDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if !expected.IsValid() || !actual.IsValid() {
		return path, qualityScalar(expected), qualityScalar(actual), expected.IsValid() != actual.IsValid()
	}
	if expected.Type() != actual.Type() {
		return path + ".type", expected.Type(), actual.Type(), true
	}
	switch expected.Kind() {
	case reflect.Pointer:
		return qualityPointerDifference(path, expected, actual)
	case reflect.Struct:
		return qualityStructDifference(path, expected, actual)
	case reflect.Slice:
		return qualitySliceDifference(path, expected, actual)
	default:
		if !reflect.DeepEqual(qualityInterface(expected), qualityInterface(actual)) {
			return path, qualityScalar(expected), qualityScalar(actual), true
		}
		return "", nil, nil, false
	}
}

func qualityPointerDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if expected.IsNil() != actual.IsNil() {
		return path, qualityScalar(expected), qualityScalar(actual), true
	}
	if expected.IsNil() {
		return "", nil, nil, false
	}
	return qualityFirstDifference(path, expected.Elem(), actual.Elem())
}

func qualityStructDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	for field := 0; field < expected.NumField(); field++ {
		fieldPath := path + "." + expected.Type().Field(field).Name
		if differing, want, got, found := qualityFirstDifference(fieldPath, expected.Field(field), actual.Field(field)); found {
			return differing, want, got, true
		}
	}
	return "", nil, nil, false
}

func qualitySliceDifference(path string, expected, actual reflect.Value) (string, any, any, bool) {
	if expected.IsNil() != actual.IsNil() {
		return path + ".nil", expected.IsNil(), actual.IsNil(), true
	}
	shared := expected.Len()
	if actual.Len() < shared {
		shared = actual.Len()
	}
	for index := 0; index < shared; index++ {
		itemPath := fmt.Sprintf("%s[%d]", path, index)
		if differing, want, got, found := qualityFirstDifference(itemPath, expected.Index(index), actual.Index(index)); found {
			return differing, want, got, true
		}
	}
	if expected.Len() != actual.Len() {
		return path + ".len", expected.Len(), actual.Len(), true
	}
	return "", nil, nil, false
}

func qualityInterface(value reflect.Value) any {
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
}

func qualityScalar(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.CanInterface() {
		return value.Interface()
	}
	return value.String()
}

// qualityPortraitFixturePath freezes one digest per configuration. The digest
// is not the layout: qualityPortraitBytes documents what it leaves out, and
// TestPlatformPortraitOmitsDocumentedFields checks that list against the bytes.
const qualityPortraitFixturePath = "testdata/portrait/portrait.json"

type qualityPortraitResult struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// TestPlatformPortraitHashes freezes a digest per seed. A matching hash does
// not mean the layouts are equal; the golden is what compares every field.
func TestPlatformPortraitHashes(t *testing.T) {
	cases := qualityPortraitCases()
	actual := make([]qualityPortraitResult, len(cases))
	var expected []qualityPortraitResult
	if !*qualityUpdate {
		expected = qualityReadPortrait(t)
		if len(expected) != len(actual) {
			t.Fatalf("portrait fixture has %d cases, this test has %d; review the change, then regenerate explicitly with go test ./platform -run TestPlatformPortraitHashes -update-platform", len(expected), len(actual))
		}
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var layout PlatformLayout
			if testCase.ungated {
				layout = qualityGenerateUngated(t, testCase.seed)
			} else {
				layout = qualityGenerateCertified(t, testCase.seed)
			}
			actual[index] = qualityPortraitResult{Name: testCase.name, Hash: qualityPortraitHash(layout)}
			if !*qualityUpdate && expected[index] != actual[index] {
				t.Errorf("portrait case changed: expected name=%q hash=%q, got name=%q hash=%q. Frozen output changed; decide consciously whether to regenerate with go test ./platform -run TestPlatformPortraitHashes -update-platform. This is not an automatic fix.",
					expected[index].Name, expected[index].Hash, actual[index].Name, actual[index].Hash)
			}
		})
	}
	if *qualityUpdate {
		qualityWritePortrait(t, actual)
	}
}

// TestPlatformPortraitOmitsDocumentedFields is the portrait's honesty check.
// Mutating a field the digest claims to ignore must leave the hash alone, and
// mutating a cell it claims to cover must move the hash. Otherwise the list
// in qualityPortraitBytes is fiction, the same way a hash of an empty buffer
// would be.
func TestPlatformPortraitOmitsDocumentedFields(t *testing.T) {
	layout := qualityCertified(t)
	original := qualityPortraitHash(layout)
	if original == qualityPortraitHash(PlatformLayout{}) {
		t.Fatal("the portrait of a certified map matches the portrait of the zero layout")
	}

	witness := layout.JumpGraph.Edges[0].Witness
	savedEnd := witness.Phases[0].End.X
	witness.Phases[0].End.X = savedEnd + 3
	if qualityPortraitHash(layout) != original {
		t.Fatal("changing a witness phase changed the portrait; the digest claims to omit witnesses")
	}
	witness.Phases[0].End.X = savedEnd

	savedDetail := layout.Judgement.Detail
	layout.Judgement.Detail = savedDetail + " omitted"
	if qualityPortraitHash(layout) != original {
		t.Fatal("changing Judgement.Detail changed the portrait; the digest claims to omit it")
	}
	layout.Judgement.Detail = savedDetail

	savedAttempts := layout.Attempts
	layout.Attempts = savedAttempts + 9
	if qualityPortraitHash(layout) != original {
		t.Fatal("changing Attempts changed the portrait; the digest claims to omit synthesis provenance")
	}
	layout.Attempts = savedAttempts

	savedExists := layout.Audit.Route.Exists
	layout.Audit.Route.Exists = !savedExists
	if qualityPortraitHash(layout) != original {
		t.Fatal("changing Audit.Route.Exists changed the portrait; the digest claims to omit the audit")
	}
	layout.Audit.Route.Exists = savedExists

	room := &layout.Plane.Rooms[0]
	savedTerrain := room.Grid.Terrain
	room.Grid.Terrain = &TerrainLayer{Palette: []TerrainDefinition{{ID: "omitted", EntryCost: 1}}}
	if qualityPortraitHash(layout) != original {
		t.Fatal("adding Grid.Terrain changed the portrait; the digest claims to omit terrain, as the root portrait omits it")
	}
	room.Grid.Terrain = savedTerrain

	if len(room.Grid.Cells) == 0 {
		t.Fatal("room 0 has no cells, so the portrait cannot be shown to cover them")
	}
	room.Grid.Cells[0] = CellKindHazard
	if qualityPortraitHash(layout) == original {
		t.Fatal("changing a covered cell did not change the portrait")
	}
}

func qualityPortraitCases() []struct {
	name    string
	seed    Seed
	ungated bool
} {
	return []struct {
		name    string
		seed    Seed
		ungated bool
	}{
		{name: "gated-seed-11", seed: 11},
		{name: "gated-seed-7", seed: 7},
		{name: "ungated-seed-11", seed: 11, ungated: true},
	}
}

func qualityGenerateUngated(t *testing.T, seed Seed) PlatformLayout {
	t.Helper()
	config := qualityConfig(seed)
	config.Progression = ProgressionPlan{}
	layout, err := Generate(context.Background(), NewM1Oracle(), config)
	if err != nil {
		t.Fatalf("generate ungated seed %d: %v", seed, err)
	}
	if !layout.Judgement.Certified() {
		t.Fatalf("ungated seed %d is %s/%s (%s)", seed, layout.Judgement.Verdict, layout.Judgement.Reason, layout.Judgement.Detail)
	}
	return layout
}

func qualityPortraitHash(layout PlatformLayout) string {
	digest := sha256.Sum256(qualityPortraitBytes(layout))
	return hex.EncodeToString(digest[:])
}

// qualityPortraitBytes is a readable digest of a platform layout.
//
// A matching hash is not equivalence. The bytes omit, on purpose:
//
//   - Grid.Terrain, palette and indices. The root portraitBytes does the same
//     with Terrain, PlantID and Tags: the hash staying put does not prove
//     those fields (portrait_test.go).
//   - Witness phases, accelerations, commands and ControlRate. The golden
//     compares those; this digest does not.
//   - Judgement.Detail and Judgement.Budget.
//   - MacroAudit. The four reports are derived. Reading them here would let a
//     stale report keep the hash, or a rewritten report move it, without the
//     map changing.
//   - Synthesis provenance: Rhythm, Placements, Dropped, Attempts and
//     AttemptSeeds. Canonical leaves the same record out.
//   - MotionNode.Resources.
//   - SurfaceInterval.Headroom and Hazard.
func qualityPortraitBytes(layout PlatformLayout) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "seed=%d plane=%dx%d spawn=%d@%d,%d goal=%d@%d,%d verdict=%s reason=%s model=%s profile=%s\n",
		layout.Config.Seed, layout.Plane.Width, layout.Plane.Height,
		layout.Plane.Spawn.Room, layout.Plane.Spawn.At.X, layout.Plane.Spawn.At.Y,
		layout.Plane.Goal.Room, layout.Plane.Goal.At.X, layout.Plane.Goal.At.Y,
		layout.Judgement.Verdict, layout.Judgement.Reason, layout.Judgement.Model, layout.Judgement.ProfileVersion)
	for _, step := range layout.Plan.Steps {
		fmt.Fprintf(&b, "step %s grants=%s\n", step.Name, step.Grants)
	}
	for _, grant := range layout.Grants {
		fmt.Fprintf(&b, "grant step=%d room=%d set=%s\n", grant.Step, grant.Room, grant.Grants)
	}
	for _, room := range layout.Plane.Rooms {
		fmt.Fprintf(&b, "room %d origin=%d,%d grid=%dx%d\n", room.ID, room.Origin.X, room.Origin.Y, room.Grid.Width, room.Grid.Height)
		for _, cell := range room.Grid.Cells {
			fmt.Fprintf(&b, "%d,", int(cell))
		}
		b.WriteByte('\n')
		for _, transition := range room.Transitions {
			fmt.Fprintf(&b, "  T %d side=%s index=%d offset=%d extent=%d exit=%d to=%d out=%s in=%s\n",
				transition.ID, transition.Side, transition.Index, transition.Offset, transition.Extent, transition.Exit, transition.To,
				qualityTraversalText(transition.Outbound), qualityTraversalText(transition.Inbound))
		}
	}
	for _, surface := range layout.JumpGraph.Surfaces {
		fmt.Fprintf(&b, "S %d room=%d kind=%s at=%s extent=%s\n", surface.ID, surface.Room, surface.Kind, qualityFloat(surface.At), surface.Extent)
	}
	for _, node := range layout.JumpGraph.Nodes {
		fmt.Fprintf(&b, "N %d surface=%d interval=%d height=%s footing=%s vel=%s mode=%s\n",
			node.ID, node.Surface, node.Interval, qualityFloat(node.Height), node.Footing, node.Velocity, node.Mode)
	}
	for _, edge := range layout.JumpGraph.Edges {
		fmt.Fprintf(&b, "E %d %d->%d %s requires=%s dur=%s\n", edge.ID, edge.From, edge.To, edge.Kind, edge.Requires, qualityFloat(edge.Duration))
	}
	for _, edge := range layout.RoomGraph.Edges {
		fmt.Fprintf(&b, "R %d %d->%d %s requires=%s\n", edge.ID, edge.From, edge.To, edge.Kind, edge.Requires)
	}
	return b.Bytes()
}

func qualityTraversalText(traversal *Traversal) string {
	if traversal == nil {
		return "nil"
	}
	return traversal.Requires.String()
}

func qualityFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func qualityReadPortrait(t *testing.T) []qualityPortraitResult {
	t.Helper()
	contents, err := os.ReadFile(qualityPortraitFixturePath)
	if err != nil {
		t.Fatalf("fixture missing; regenerate explicitly with go test ./platform -run TestPlatformPortraitHashes -update-platform: %v", err)
	}
	var fixture []qualityPortraitResult
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatalf("portrait fixture: %v", err)
	}
	return fixture
}

func qualityWritePortrait(t *testing.T, fixture []qualityPortraitResult) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(qualityPortraitFixturePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	contents, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("encode portrait: %v", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(qualityPortraitFixturePath, contents, 0o644); err != nil {
		t.Fatalf("write portrait: %v", err)
	}
}
