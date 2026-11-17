package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"math"
	"testing"
)

// lineFor 为场景测试构造带指定句柄与坐标的 LINE 实体。
func lineFor(h uint64, x float64) *entity.EntLine {
	return &entity.EntLine{BaseEntity: entity.BaseEntity{Handle: h, TypeName: "LINE"}, Start: entity.Point3{X: x}, End: entity.Point3{X: x + 1, Y: 1}}
}

// TestSceneInstancesBasePointAndUnreferenced 验证块基点和重复插入位置正确，未引用的块定义不作为顶层图元输出。
func TestSceneInstancesBasePointAndUnreferenced(t *testing.T) {
	a, b := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 1}, Scale: entity.Point3{X: 1, Y: 1}, Position: entity.Point3{X: 10}, BlockHeader: 100}, &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 2}, Scale: entity.Point3{X: 1, Y: 1}, Position: entity.Point3{X: 30}, BlockHeader: 100}
	d := &Document{ModelSpace: []any{a, b}, Blocks: map[uint64][]any{100: {lineFor(3, 5)}, 101: {lineFor(4, 1000)}}, InternalObjs: map[uint64]*object.ObjGeneric{100: {Name: "BLOCK_HEADER", Fields: []object.ObjField{{Key: "base_pt", Val: []float64{5, 0, 0}}}}}}
	sc := BuildScene(d, .01)
	if len(sc.Primitives) != 2 {
		t.Fatal(len(sc.Primitives))
	}
	for i, x := range []float64{10, 30} {
		p := sc.Primitives[i]
		if p.Strokes[0].X1 != x || len(p.Source.Inserts) != 1 || p.Source.Inserts[0] != uint64(i+1) {
			t.Fatalf("bad instance: %+v", p)
		}
	}
}

// TestSourceDrawOrder 验证场景按源文件的绘制顺序输出图元。
func TestSourceDrawOrder(t *testing.T) {
	d := &Document{InternalObjs: map[uint64]*object.ObjGeneric{99: {Name: "SORTENTSTABLE", Fields: []object.ObjField{{"num_ents", int64(2)}, {"ents[0]", int64(1)}, {"sort_ents[0]", int64(0)}, {"ents[1]", int64(3)}, {"sort_ents[1]", int64(1)}}}}}
	out := d.OrderedEntities([]any{lineFor(1, 0), lineFor(2, 0), lineFor(3, 0)})
	for i, h := range []uint64{3, 2, 1} {
		if out[i].(entity.EntityCommon).Common().Handle != h {
			t.Fatal("wrong draw order")
		}
	}
}

// TestByBlockStyleDoesNotSquareGlobalScale 验证 ByBlock 样式继承不会重复乘入全局线型比例。
func TestByBlockStyleDoesNotSquareGlobalScale(t *testing.T) {
	d := &Document{Ltscale: 3, LayerColors: map[uint64]LayerColor{1: {Name: "0", StyleKnown: true}, 2: {Name: "drawing", StyleKnown: true, Lineweight: 7, Ltype: 50, Index: 4}}, InternalObjs: map[uint64]*object.ObjGeneric{50: {Fields: []object.ObjField{{"dash_lengths", []float64{2, -1}}}}}}
	ts := NewTessellator(d)
	parent, pc, pl := ts.resolveStyle(&entity.BaseEntity{Layer: 2, Head: &entity.CommonEntityHead{Linewt: 29, LtypeScale: 1}})
	ts.parentStyle, ts.parentColor, ts.parentLayer = parent, pc, pl
	child, c, l := ts.resolveStyle(&entity.BaseEntity{Layer: 1, Head: &entity.CommonEntityHead{Linewt: 30, LtypeFlags: 1, LtypeScale: 1}, Color: entity.EntColor{HasIndex: true, Index: 0}})
	if child.LineMM != .25 || child.Dash[0] != 6 || child.Dash[1] != -3 || c.Index != 4 || l != 2 {
		t.Fatalf("inheritance: %+v %+v %d", child, c, l)
	}
	for code, want := range map[uint16]float64{0: 0, 7: .25, 23: 2.11, 29: .25, 31: .25} {
		if LineweightMM(code) != want {
			t.Fatal(code)
		}
	}
}

// TestHiddenLayerAndExpansionCycleAreReported 验证隐藏图层和块引用循环不会绘制，并产生对应诊断。
func TestHiddenLayerAndExpansionCycleAreReported(t *testing.T) {
	d := &Document{LayerColors: map[uint64]LayerColor{10: {Off: true}}, Blocks: map[uint64][]any{}}
	l := lineFor(1, 0)
	l.Layer = 10
	ins := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 2}, Scale: entity.Point3{X: 1, Y: 1}, BlockHeader: 20}
	d.ModelSpace = []any{l, ins}
	d.Blocks[20] = []any{ins}
	sc := BuildScene(d, .01)
	if len(sc.Primitives) != 0 {
		t.Fatal("hidden/cyclic objects rendered")
	}
	status := map[string]bool{}
	for _, d := range sc.Diagnostics {
		status[d.Status] = true
	}
	if !status["layer_off"] || !status["cycle"] {
		t.Fatal(status)
	}
}

// TestAffineEllipseBoundsAndTolerance 验证仿射椭圆的包围盒包含解析极值，离散曲线满足测试容差。
func TestAffineEllipseBoundsAndTolerance(t *testing.T) {
	s := arcSegment(entity.Point2{X: 2, Y: -4}, entity.Point2{X: 7}, entity.Point2{Y: 2}, .2, 5.9, MatrixXform([6]float64{-2, .4, .8, 1, 10, 20}))
	g := &Geometry{Segments: []CurveSegment{s}}
	b := g.Bounds()
	st := g.Strokes(.005)
	worst := 0.0
	for i := 0; i <= 10000; i++ {
		q := s.Point(s.Start + s.Sweep*float64(i)/10000)
		if q.X < b.MinX-1e-10 || q.X > b.MaxX+1e-10 || q.Y < b.MinY-1e-10 || q.Y > b.MaxY+1e-10 {
			t.Fatal("missed extremum")
		}
		nearest := math.Inf(1)
		for _, line := range st {
			nearest = math.Min(nearest, pointLineDistance(q, entity.Point2{X: line.X1, Y: line.Y1}, entity.Point2{X: line.X2, Y: line.Y2}))
		}
		worst = math.Max(worst, nearest)
	}
	if worst > .005 {
		t.Fatalf("curve error=%g", worst)
	}
	t.Logf("10001 samples: max affine ellipse chord error %.8g (<0.005)", worst)
}

// TestHatchRotatedEllipseUsesBothAxisComponents 验证旋转椭圆填充边界同时使用两个轴的分量。
func TestHatchRotatedEllipseUsesBothAxisComponents(t *testing.T) {
	ts := NewTessellator(&Document{})
	ts.Tolerance = .001
	pts, _ := ts.HatchBoundary(entity.HatchPath{Segs: []entity.HatchSeg{{CurveType: 3, Endpoint: entity.Point2{X: 3, Y: 4}, Ratio: .5, StartAng: 0, EndAng: 2 * math.Pi, Ccw: true}}}, IdentityXform())
	if math.Hypot(pts[0].X-3, pts[0].Y-4) > 1e-10 {
		t.Fatal(pts[0])
	}
	maxx := 0.0
	for _, p := range pts {
		maxx = math.Max(maxx, p.X)
	}
	if math.Abs(maxx-math.Sqrt(13)) > .002 {
		t.Fatal(maxx)
	}
}

// TestInsertOCSProjectionIncludesElevation 验证 INSERT 的对象坐标系投影包含标高贡献。
func TestInsertOCSProjectionIncludesElevation(t *testing.T) {
	ts := NewTessellator(&Document{})
	ts.Faithful = true
	in := &entity.EntInsert{Position: entity.Point3{X: 2, Y: 3, Z: 5}, Scale: entity.Point3{X: 2, Y: 3, Z: 1}, Extrusion: entity.Point3{X: .8, Z: .6}}
	q := ts.blockTransform(in, IdentityXform()).Apply(entity.Point2{X: 1, Y: 2})
	// Independent 3D calculation: Ax=(0,1,0), Ay=(-.6,0,.8), Az=(.8,0,.6).
	// OCS point after scale+translation is (4,9,5), so WCS XY=(-1.4,4).
	if math.Abs(q.X+1.4) > 1e-9 || math.Abs(q.Y-4) > 1e-9 {
		t.Fatalf("OCS projection got %+v, expected {-1.4 4}", q)
	}
}
