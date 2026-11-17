package drawing

import (
	"bytes"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"math"
	"os"
	"testing"
)

// TestSpatialDepthSurvivesNestedProjection 验证嵌套空间变换保留 Z 分量直到最终投影。
func TestSpatialDepthSurvivesNestedProjection(t *testing.T) {
	// Two opposite tilts must undo each other, including the intermediate Z.
	a := SpatialXform([12]float64{1, 0, 0, 0, 0, 0, -1, 0, 0, 1, 0, 0})
	b := SpatialXform([12]float64{1, 0, 0, 0, 0, 0, 1, 0, 0, -1, 0, 0})
	q := a.Compose(b).Project(entity.Point3{X: 2, Y: 7, Z: 11})
	if q.X != 2 || q.Y != 7 {
		t.Fatal(q)
	}
	ins := &entity.EntInsert{Scale: entity.Point3{X: 1, Y: 1, Z: 2}, Extrusion: entity.Point3{X: 1}}
	p := insertSpatial(ins).Project(entity.Point3{Z: 3})
	if math.Abs(p.X-6) > 1e-10 {
		t.Fatal(p)
	}
}

// TestFitSplineNaturalC2AndBoundedFlattening 验证自然边界拟合样条的 C2 连续性及离散误差界。
func TestFitSplineNaturalC2AndBoundedFlattening(t *testing.T) {
	e := &entity.EntSpline{Scenario: 2, Degree: 3, FitPoints: []entity.Point3{{X: 0, Y: 0}, {X: 1, Y: 2}, {X: 3, Y: -1}, {X: 4, Y: 0}}}
	g := fitSplineGeometry(e, IdentityXform(), .001)
	if len(g.Segments) != 3 {
		t.Fatal(g)
	}
	for i, s := range g.Segments {
		if !s.Cubic || s.P0.X != e.FitPoints[i].X || s.P1.Y != e.FitPoints[i+1].Y {
			t.Fatal(s)
		}
		st := cubicStrokes(s, .001)
		for k := 0; k <= 1000; k++ {
			p := s.CubicPoint(float64(k) / 1000)
			d := math.Inf(1)
			for _, v := range st {
				d = math.Min(d, pointLineDistance(p, entity.Point2{X: v.X1, Y: v.Y1}, entity.Point2{X: v.X2, Y: v.Y2}))
			}
			if d > .00101 {
				t.Fatalf("flatten error %g", d)
			}
		}
	}
	// Natural boundary has zero second derivative.
	s := g.Segments[0]
	if math.Abs(s.P0.X-2*s.C1.X+s.C2.X) > 1e-10 || math.Abs(s.P0.Y-2*s.C1.Y+s.C2.Y) > 1e-10 {
		t.Fatal("non-natural start")
	}
	for i := 0; i < 2; i++ {
		a, b := g.Segments[i], g.Segments[i+1]
		h0 := math.Hypot(a.P1.X-a.P0.X, a.P1.Y-a.P0.Y)
		h1 := math.Hypot(b.P1.X-b.P0.X, b.P1.Y-b.P0.Y)
		for _, v := range [][6]float64{{a.P1.X, a.C2.X, a.C1.X, b.P0.X, b.C1.X, b.C2.X}, {a.P1.Y, a.C2.Y, a.C1.Y, b.P0.Y, b.C1.Y, b.C2.Y}} {
			if math.Abs((v[0]-v[1])/h0-(v[4]-v[3])/h1) > 1e-10 || math.Abs((v[0]-2*v[1]+v[2])/(h0*h0)-(v[3]-2*v[4]+v[5])/(h1*h1)) > 1e-10 {
				t.Fatal("C1/C2 discontinuity")
			}
		}
	}
}

// TestFitSplineStoredTangentDirection 验证拟合样条遵守源记录保存的端点切线方向。
func TestFitSplineStoredTangentDirection(t *testing.T) {
	e := &entity.EntSpline{FitPoints: []entity.Point3{{}, {X: 2, Y: 1}, {X: 3}}}
	e.SetFitTangents(entity.Point3{Y: 1}, entity.Point3{X: 1})
	g := fitSplineGeometry(e, IdentityXform(), .001)
	if math.Abs(g.Segments[0].C1.X) > 1e-12 || g.Segments[0].C1.Y <= 0 || math.Abs(g.Segments[1].C2.Y) > 1e-12 {
		t.Fatal(g)
	}
}

// TestFitSplineDuplicatePointsAndSubdivisionLimit 验证重复拟合点被处理，细分触顶时记录限制且保留曲线终点。
func TestFitSplineDuplicatePointsAndSubdivisionLimit(t *testing.T) {
	pts := make([]entity.Point3, 10000)
	pts = append(pts, entity.Point3{X: 1, Y: 1})
	g := fitSplineGeometry(&entity.EntSpline{FitPoints: pts}, IdentityXform(), .01)
	if len(g.Segments) != 1 || len(g.Warnings) != 2 {
		t.Fatal("duplicate-point run was not reduced", len(g.Segments), g.Warnings)
	}
	g = &Geometry{Segments: []CurveSegment{{Cubic: true, P1: entity.Point2{X: 1}, C1: entity.Point2{Y: 1}, C2: entity.Point2{X: 1, Y: 1}}}}
	strokes := g.Strokes(1e-30)
	if len(strokes) > 65536 || len(g.Warnings) == 0 || strokes[len(strokes)-1].X2 != 1 {
		t.Fatal("subdivision limit not reported or curve endpoint lost")
	}
}

// TestLayoutOwnershipViewportAndFrozenLayer 验证布局归属和视口冻结层，并拒绝把透视视口静默视为正交视口。
func TestLayoutOwnershipViewportAndFrozenLayer(t *testing.T) {
	vp := &entity.EntViewport{BaseEntity: entity.BaseEntity{Handle: 21, Mode: 1}, Width: 10, Height: 10, ViewSize: 20, ViewCtr: entity.Point2{X: 100}, ViewDir: entity.Point3{Z: 1}, FrozenLayers: []uint64{8}}
	d := &Document{Blocks: map[uint64][]any{}, ByHandle: map[uint64]any{20: &entity.EntViewport{BaseEntity: entity.BaseEntity{Handle: 20, Mode: 1}}, 21: vp, 22: lineFor(22, 1)}, InternalObjs: map[uint64]*object.ObjGeneric{1: {Name: "BLOCK_CONTROL", Handles: []uint64{2, 10}}, 10: {Name: "BLOCK_HEADER", Handles: []uint64{0, 19, 20}, Fields: []object.ObjField{{Key: "name", Val: "*Paper_Space"}}}, 30: {Name: "LAYOUT", Handles: []uint64{10, 20}, Fields: []object.ObjField{{Key: "layout_name", Val: "Sheet"}}}}}
	d.ByHandle[22].(*entity.EntLine).Mode = 1
	shown, hidden := lineFor(40, 100), lineFor(41, 100)
	hidden.Layer = 8
	d.ModelSpace = []any{shown, hidden}
	sc, err := BuildLayoutScene(d, "Sheet", .01)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Primitives) != 2 {
		t.Fatal(len(sc.Primitives))
	}
	p := sc.Primitives[0]
	if p.Strokes[0].X1 != 0 || p.Clip == nil || p.Source.Inserts[0] != 21 {
		t.Fatal(p)
	}
	if len(d.LayoutEntities(10)) != 3 {
		t.Fatal("lost current paper records")
	}
	if _, err = BuildLayoutScene(d, "unknown", .01); err == nil {
		t.Fatal("silently selected layout")
	}
	vp.StatusFlag = 1
	sc, _ = BuildLayoutScene(d, "Sheet", .01)
	if len(sc.Primitives) != 1 {
		t.Fatal("perspective silently treated as orthographic")
	}
}

// TestRecoveryLocalPaperAndNullImageReferences 验证私有样本的布局与 DXF 往返保留源句柄，空图片引用不会被错误关联。
func TestRecoveryLocalPaperAndNullImageReferences(t *testing.T) {
	data, err := os.ReadFile(testfixtures.Path(t, "drawing-a"))
	if os.IsNotExist(err) {
		t.Skip("local drawing unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Layouts()) != 3 || len(d.LayoutEntities(0x2B7AD)) != 32 || len(d.LayoutEntities(0x5381)) != 10 {
		t.Fatal(d.Layouts())
	}
	for _, layout := range d.Layouts() {
		if layout.Model && (layout.Entities == 0 || layout.Entities != len(d.SelectedModelSpace())) {
			t.Fatal("model layout inventory disagrees with selected model", layout)
		}
	}
	for _, h := range []uint64{0x2B7B6, 0x2BA56} {
		v := d.ByHandle[h].(*entity.EntViewport)
		if len(v.FrozenLayers) != 4 || v.HandleError != "" {
			t.Fatalf("viewport %X %+v", h, v)
		}
	}
	for _, h := range []uint64{0x1D1612, 0x1D1853, 0x1D1989} {
		v := d.ByHandle[h].(*entity.EntImage)
		if v.ImageDef != 0 {
			t.Fatal("invented image association")
		}
	}
	var buf bytes.Buffer
	if err = WriteDXF(d, &buf); err != nil {
		t.Fatal(err)
	}
	round, err := ParseDXF(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string]int{}
	for h, e := range d.ByHandle {
		if round.ByHandle[h] == nil {
			missing[entity.EntityBase(e).TypeName]++
		}
	}
	if len(missing) > 0 {
		t.Fatalf("roundtrip dropped source handles: %v", missing)
	}
	for _, h := range []uint64{0x1D1612, 0x2B7B6} {
		if round.ByHandle[h] == nil {
			t.Fatalf("lost %X", h)
		}
	}
}

// TestDXFFitTangentsWeightsAndSpace 验证 DXF 保留拟合切线、权重与纸张空间归属。
func TestDXFFitTangentsWeightsAndSpace(t *testing.T) {
	fit := &entity.EntSpline{BaseEntity: entity.BaseEntity{Handle: 12, TypeName: "SPLINE", Mode: 2}, Degree: 3, FitTolerance: .002, KnotTolerance: .003, CtrlTolerance: .004, FitPoints: []entity.Point3{{}, {X: 1, Y: 2}, {X: 3}}}
	fit.SetFitTangents(entity.Point3{X: 2}, entity.Point3{Y: 3})
	paper := lineFor(15, 10)
	paper.Mode = 1
	d := &Document{ModelSpace: []any{fit}, PspaceSpace: []any{paper}, ByHandle: map[uint64]any{12: fit, 15: paper}, Blocks: map[uint64][]any{}}
	var buf bytes.Buffer
	if err := WriteDXF(d, &buf); err != nil {
		t.Fatal(err)
	}
	r, err := ParseDXF(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	s := r.ByHandle[12].(*entity.EntSpline)
	a, b := s.FitTangents()
	if a.X != 2 || b.Y != 3 || s.FitTolerance != .002 || s.KnotTolerance != .003 || s.CtrlTolerance != .004 {
		t.Fatal(s, a, b)
	}
	if r.ByHandle[15].(entity.EntityCommon).Common().Mode != 1 || len(BuildScene(r, .01).Primitives) != 1 {
		t.Fatal("paper entity lost or leaked into model")
	}
}

// TestAttributeDefinitionsAndInstancesDoNotOverlap 验证属性实例不会与属性定义的默认文字重复绘制。
func TestAttributeDefinitionsAndInstancesDoNotOverlap(t *testing.T) {
	def := &entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 10, Owner: 100, TypeName: "ATTDEF"}, Tag: "SIZE", Text: "default", Height: 1}
	value := &entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 11, Owner: 20, TypeName: "ATTRIB"}, Tag: "SIZE", Text: "143-00-135", Height: 1}
	recovered := &entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 12, Owner: 20, TypeName: "ATTRIB"}, Tag: "BRAND", Text: "MM6", Height: 1}
	constant := &entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 13, Owner: 100, TypeName: "ATTDEF", Extra: map[string]any{"flags": int64(2)}}, Tag: "UNIT", Text: "mm", Height: 1}
	hidden := &entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 14, Owner: 20, TypeName: "ATTRIB", Extra: map[string]any{"flags": int64(1)}}, Tag: "SECRET", Text: "hidden", Height: 1}
	ins := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 20}, BlockHeader: 100, Scale: entity.Point3{X: 1, Y: 1, Z: 1}, Attribs: []uint64{11, 11}}
	d := &Document{ModelSpace: []any{ins}, Blocks: map[uint64][]any{100: {def, constant}, 20: {value, recovered, hidden}}, Attribs: map[uint64]*entity.EntAttrib{11: value, 12: recovered, 14: hidden}}
	sc := BuildScene(d, .01)
	if len(sc.Primitives) != 3 {
		t.Fatal(len(sc.Primitives))
	}
	texts := map[string]int{}
	for _, p := range sc.Primitives {
		texts[p.Lb.Text]++
	}
	if texts["default"] != 0 || texts["143-00-135"] != 1 || texts["MM6"] != 1 || texts["mm"] != 1 || texts["hidden"] != 0 {
		t.Fatal(texts)
	}
}
