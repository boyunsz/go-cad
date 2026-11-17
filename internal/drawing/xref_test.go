package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"math"
	"os"
	"testing"
)

// TestExternalNamespaceTransformLayerOverrideAndCycle 验证外参句柄隔离、插入变换、主图图层覆盖和循环检测，并保持源文档不变。
func TestExternalNamespaceTransformLayerOverrideAndCycle(t *testing.T) {
	local := lineFor(10, 0)
	local.Layer = 1
	ext := &Document{SourceFile: "part.dwg", ModelSpace: []any{local}, ByHandle: map[uint64]any{10: local}, LayerColors: map[uint64]LayerColor{1: {Name: "Main", StyleKnown: true, Lineweight: 8}}, InternalObjs: map[uint64]*object.ObjGeneric{}}
	ins := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 20, TypeName: "INSERT", Layer: 2}, BlockHeader: 100, Position: entity.Point3{X: 10, Y: 20}, Scale: entity.Point3{X: 2, Y: 3, Z: 1}, Rotation: math.Pi / 2}
	root := &Document{ModelSpace: []any{ins}, ByHandle: map[uint64]any{10: lineFor(10, 999), 20: ins}, LayerColors: map[uint64]LayerColor{2: {Name: "0"}, 3: {Name: "part|Main", StyleKnown: true, Lineweight: 3}}, InternalObjs: map[uint64]*object.ObjGeneric{100: {Name: "BLOCK_HEADER", Fields: []object.ObjField{{Key: "name", Val: "part"}}}}, Xrefs: map[uint64]*XrefDocument{100: {Document: ext, Path: "part.dwg"}}}
	scene := BuildPlotScene(root, .01)
	if len(scene.Primitives) != 1 {
		t.Fatal(len(scene.Primitives))
	}
	p := scene.Primitives[0]
	s := p.Strokes[0]
	if math.Abs(s.X1-10) > .00001 || math.Abs(s.Y1-20) > .00001 || math.Abs(s.X2-7) > .00001 || math.Abs(s.Y2-22) > .00001 {
		t.Fatal("xref transform changed", s)
	}
	if p.Style.LineMM != .13 || p.Source.File != "part.dwg" || p.Source.XrefPath[0].InsertHandle != 20 {
		t.Fatal("xref override/provenance missing")
	}
	if ext.LayerColors[1].Lineweight != 8 {
		t.Fatal("external source layer was mutated")
	}
	found := false
	for _, d := range scene.Diagnostics {
		if d.Source.File == "" && d.Source.Handle == 10 && d.Status == "not_instantiated" {
			found = true
		}
	}
	if !found {
		t.Fatal("external handle collision hid root disposition")
	}
	ext.ModelSpace = []any{ins}
	ext.InternalObjs = root.InternalObjs
	ext.Xrefs = root.Xrefs
	cyclic := BuildScene(root, .01)
	found = false
	for _, d := range cyclic.Diagnostics {
		found = found || d.Status == "cycle"
	}
	if !found {
		t.Fatal("external cycle not bounded")
	}
}

// TestLocalChineseXrefTemplateLayersRespectHostVisibility 验证私有主图的中文外参图层名称按 DWG 码页解码后，正确匹配宿主可见性覆盖。
// 模板辅助图层应被隐藏，外参内部的工程虚线仍应保留。
func TestLocalChineseXrefTemplateLayersRespectHostVisibility(t *testing.T) {
	b, err := os.ReadFile(testfixtures.Path(t, "visibility"))
	if err != nil {
		t.Skip("local drawing unavailable")
	}
	root, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	root.ModelSpace = nil
	root.Blocks = map[uint64][]any{}
	root.Xrefs = map[uint64]*XrefDocument{}
	for _, bh := range []uint64{0x1d08d5, 0x1d08e2} {
		name, _ := root.InternalObjs[bh].Field("name").(string)
		found := false
		for _, l := range root.LayerColors {
			if l.Name == name+"|Defpoints1" && l.Off {
				found = true
			}
		}
		if !found {
			t.Errorf("missing decoded disabled template layer %q", name+"|Defpoints1")
		}
		guide := lineFor(10, 0)
		guide.Layer = 1
		part := lineFor(11, 5)
		part.Layer = 2
		ext := &Document{SourceFile: name + ".dwg", ModelSpace: []any{guide, part}, ByHandle: map[uint64]any{10: guide, 11: part}, LayerColors: map[uint64]LayerColor{1: {Name: "Defpoints1", StyleKnown: true}, 2: {Name: "Hidden", StyleKnown: true, Ltype: 3}}, InternalObjs: map[uint64]*object.ObjGeneric{3: {Name: "LTYPE", Fields: []object.ObjField{{Key: "dash_lengths", Val: []float64{2, -1}}}}}}
		root.Xrefs[bh] = &XrefDocument{Document: ext, Path: ext.SourceFile}
		root.ModelSpace = append(root.ModelSpace, &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: bh + 1, TypeName: "INSERT", Layer: 16}, BlockHeader: bh, Scale: entity.Point3{X: 1, Y: 1, Z: 1}})
	}
	scene := BuildPlotScene(root, .01)
	guides, parts := 0, 0
	for _, p := range scene.Primitives {
		if p.Source.Handle == 10 {
			guides++
		}
		if p.Source.Handle == 11 {
			parts++
			if len(p.Style.Dash) == 0 {
				t.Error("real hidden contour lost dash style")
			}
		}
	}
	if guides != 0 || parts != 2 {
		t.Fatalf("template guides=%d (want 0), real dashed contours=%d (want 2)", guides, parts)
	}
}
