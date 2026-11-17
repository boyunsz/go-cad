package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"math"
	"testing"
)

// TestLeaderArrowUsesSourceBlockAndFlag 验证引线箭头使用源图块的几何、尺寸和方向，并遵守箭头关闭标志。
func TestLeaderArrowUsesSourceBlockAndFlag(t *testing.T) {
	leader := &entity.EntLeader{BaseEntity: entity.BaseEntity{Handle: 7, TypeName: "LEADER"}, Points: []entity.Point3{{X: 10, Y: 10}, {X: 10, Y: 5}}, ArrowheadOn: true, Dimstyle: 80}
	arrow := &entity.EntSolid{BaseEntity: entity.BaseEntity{Handle: 9, TypeName: "SOLID"}, P1: entity.Point2{}, P2: entity.Point2{X: -1, Y: .25}, P3: entity.Point2{X: -1, Y: -.25}, P4: entity.Point2{X: -1, Y: -.25}}
	d := &Document{ModelSpace: []any{leader}, Blocks: map[uint64][]any{90: {arrow}}, InternalObjs: map[uint64]*object.ObjGeneric{80: {Fields: []object.ObjField{{Key: "DIMLDRBLK", Val: uint64(90)}, {Key: "DIMASZ", Val: 2.0}, {Key: "DIMSCALE", Val: 1.0}}}}}
	sc := BuildScene(d, .01)
	if len(sc.Primitives) != 2 {
		t.Fatalf("leader + arrow expected: %d", len(sc.Primitives))
	}
	p := sc.Primitives[1]
	if p.Kind != 2 || len(p.Source.Inserts) != 1 || p.Source.Inserts[0] != 7 {
		t.Fatal("arrow fill/provenance lost")
	}
	pts := p.Fill.Loops[0]
	if math.Abs(pts[0].X-10) > .00001 || math.Abs(pts[0].Y-10) > .00001 || math.Abs(pts[1].Y-8) > .00001 {
		t.Fatal("arrow not transformed with source size/direction", pts)
	}
	leader.ArrowheadOn = false
	if got := len(BuildScene(d, .01).Primitives); got != 1 {
		t.Fatal("disabled arrow rendered", got)
	}
}
