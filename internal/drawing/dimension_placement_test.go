package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"testing"
)

// TestDimensionClonePositionWithinInsert 验证块内标注副本在嵌套插入变换后保留各自的偏移位置。
func TestDimensionClonePositionWithinInsert(t *testing.T) {
	// The same cached dimension picture can serve multiple clones. Its
	// insertion offset belongs inside the containing INSERT transform.
	line := lineFor(5, -100)
	dim := &entity.EntDimension{BaseEntity: entity.BaseEntity{Handle: 3, TypeName: "DIM_LINEAR"}, AnonymousBlock: 200, HasInsertPoint: true, InsertPoint: entity.Point3{X: 110, Y: -20}}
	clone := *dim
	clone.Handle = 4
	clone.InsertPoint.X = 120
	ins := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 1, TypeName: "INSERT"}, BlockHeader: 100, Position: entity.Point3{X: 1000, Y: 2000}, Scale: entity.Point3{X: 2, Y: 3, Z: 1}}
	d := &Document{ModelSpace: []any{ins}, Blocks: map[uint64][]any{100: {dim, &clone}, 200: {line}}}
	sc := BuildScene(d, .01)
	if len(sc.Primitives) != 2 {
		t.Fatal("clones not expanded", len(sc.Primitives))
	}
	for i, want := range []float64{1020, 1040} {
		s := sc.Primitives[i].Strokes[0]
		if s.X1 != want || s.Y1 != 1940 || s.X2 != want+2 || s.Y2 != 1943 {
			t.Fatalf("clone %d placed at %+v; want (%g,1940)-(%g,1943)", i, s, want, want+2)
		}
	}
}
