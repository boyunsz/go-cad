package render

import (
	"bytes"
	"encoding/xml"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"golang.org/x/image/font/gofont/goregular"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testFont 将内置 Go Regular 字体写入测试临时目录，返回其路径。
// 写入失败时立即终止测试；文件由 testing 的临时目录清理机制回收。
func testFont(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Go-Regular.ttf")
	if e := os.WriteFile(p, goregular.TTF, 0600); e != nil {
		t.Fatal(e)
	}
	return p
}

// TestMTextAbsoluteHeight 验证 MTEXT 绝对字高指令按图纸单位解释，而非相对倍数。
func TestMTextAbsoluteHeight(t *testing.T) {
	doc := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: `\H2;A`, TextHeight: 10, Attachment: 1}}}
	p, e := PrepareDrawing(doc, RenderOptions{Width: 512, FontPath: testFont(t)})
	if e != nil {
		t.Fatal(e)
	}
	b := p.Texts[0].Bounds
	if b.MaxY-b.MinY > 2.1 {
		t.Fatalf("absolute 2-unit text became %.4f units tall", b.MaxY-b.MinY)
	}
}

// TestFullBoundsKeepsRemoteGeometryAndClipsInfinite 验证全范围视口保留远处有限图元，并按有效范围裁切无限图元。
func TestFullBoundsKeepsRemoteGeometryAndClipsInfinite(t *testing.T) {
	doc := &drawing.Document{ModelSpace: []any{&entity.EntLine{Start: entity.Point3{}, End: entity.Point3{X: 100, Y: 1}}, &entity.EntCircle{Center: entity.Point3{X: 1000, Y: 100}, Radius: 2}, &entity.EntRay{Start: entity.Point3{}, UnitVector: entity.Point3{X: 1, Y: 1}}}}
	p, err := PrepareDrawing(doc, RenderOptions{Width: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Primitives) != 3 || p.Audit.Bounds.MaxX < 1002 || p.Audit.Bounds.MinX >= 0 || p.Audit.Bounds.MaxX > 2000 {
		t.Fatalf("view dropped finite geometry or used artificial ray extents: %+v", p.Audit.Bounds)
	}
	for _, s := range p.Primitives[2].Strokes {
		if s.X1 < p.Audit.Bounds.MinX || s.X2 > p.Audit.Bounds.MaxX || s.Y2 > p.Audit.Bounds.MaxY+1e-8 {
			t.Fatal(s)
		}
	}
}

// TestHatchHoleIslandAndSharedSVGPNG 验证填充孔洞及岛屿的奇偶规则在 SVG 与 PNG 中一致。
func TestHatchHoleIslandAndSharedSVGPNG(t *testing.T) {
	square := func(a, b float64) []entity.Point2 {
		return []entity.Point2{{X: a, Y: a}, {X: b, Y: a}, {X: b, Y: b}, {X: a, Y: b}}
	}
	h := &entity.EntHatch{SolidFill: true, Paths: []entity.HatchPath{{Points: square(0, 10)}, {Points: square(2, 8)}, {Points: square(4, 6)}}}
	p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{h}}, RenderOptions{Width: 200, Bounds: "explicit", ViewBox: &drawing.Box2{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.PNG()
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		x, y int
		dark bool
	}{{20, 20, true}, {60, 60, false}, {100, 100, true}} {
		r, _, _, _ := im.At(v.x, v.y).RGBA()
		if (r < 32768) != v.dark {
			t.Fatalf("fill hole/island %v: %d", v, r)
		}
	}
	svg, _ := p.SVG()
	if !bytes.Contains(svg, []byte(`fill-rule="evenodd"`)) {
		t.Fatal("SVG missing evenodd")
	}
}

// TestDashPhaseAcrossConnectedSegments 验证相连线段之间延续虚线相位，不在每个离散段重启。
func TestDashPhaseAcrossConnectedSegments(t *testing.T) {
	st := dashStrokes([]drawing.Stroke{{X1: 0, X2: 2}, {X1: 2, X2: 7}}, []float64{3, -2}, 0, .1)
	want := []drawing.Stroke{{X1: 0, X2: 2}, {X1: 2, X2: 3}, {X1: 5, X2: 7}}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("dash reset at joint: %+v", st)
	}
}

// TestFaithfulXMLGlyphsColorsAndRepeatability 验证 SVG XML 有效、字形和颜色正确，并且重复输出保持确定性。
func TestFaithfulXMLGlyphsColorsAndRepeatability(t *testing.T) {
	doc := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: `\C1;A{\H2x;B}\P<>&"`, TextHeight: 2, RectWidth: 10, Attachment: 1}}}
	p, err := PrepareDrawing(doc, RenderOptions{Width: 400, FontPath: testFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.SVG()
	b, _ := p.SVG()
	if !bytes.Equal(a, b) {
		t.Fatal("nondeterministic SVG")
	}
	if bytes.Contains(a, []byte("NaN")) || !bytes.Contains(a, []byte(`fill="#ff0000"`)) || bytes.Contains(a, []byte("<text ")) {
		t.Fatal("outline/color failure")
	}
	dec := xml.NewDecoder(bytes.NewReader(a))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	first := p.Texts[0].Glyphs[0]
	if first.Rune != 'A' || first.Size != 2 || p.Texts[0].Glyphs[1].Size != 4 {
		t.Fatalf("run size: %+v", first)
	}
	p.Options.TextMode = "text"
	txt, _ := p.SVG()
	if !bytes.Contains(txt, []byte(`<text `)) {
		t.Fatal("editable text unavailable")
	}
}

// TestFaithfulInvalidOptions 验证 faithful 渲染对无效参数返回错误。
func TestFaithfulInvalidOptions(t *testing.T) {
	for _, o := range []RenderOptions{{Width: -1}, {Width: 17000}, {Bounds: "auto"}, {Bounds: "explicit"}, {TextMode: "bad"}, {CurveTolerance: math.NaN()}, {UnitsPerMM: -1}, {Bounds: "explicit", ViewBox: &drawing.Box2{MaxX: math.Inf(1), MaxY: 1}}, {Width: 16384, Bounds: "explicit", ViewBox: &drawing.Box2{MaxX: 1, MaxY: 100}}} {
		if _, err := PrepareDrawing(&drawing.Document{}, o); err == nil {
			t.Fatalf("accepted invalid option %+v", o)
		}
	}
}

// TestPatternHatchClipsHoleBeforeBlockTransform 验证图案线先按局部孔洞裁切，再应用块变换。
func TestPatternHatchClipsHoleBeforeBlockTransform(t *testing.T) {
	outer := []entity.Point2{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	hole := []entity.Point2{{3, 3}, {7, 3}, {7, 7}, {3, 7}}
	h := &entity.EntHatch{Paths: []entity.HatchPath{{Points: outer}, {Points: hole}}, Deflines: []entity.HatchDefLine{{Offset: entity.Point2{Y: 1}, Pt0: entity.Point2{Y: .5}}}}
	f := &drawing.FillGeometry{Hatch: h, Transform: drawing.MatrixXform([6]float64{0, 1, -1, 0, 20, 0})}
	st, err := hatchStrokes(f, .01)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 14 {
		t.Fatal(len(st))
	}
	for _, s := range st {
		x, y := (s.Y1+s.Y2)/2, 20-(s.X1+s.X2)/2
		if x > 3 && x < 7 && y > 3 && y < 7 {
			t.Fatal("pattern crossed hole", s)
		}
	}
}

// TestTextFitAndFractionDivider 验证文字拟合使用对齐端点，并正确布置堆叠分数及分隔线。
func TestTextFitAndFractionDivider(t *testing.T) {
	f := testFont(t)
	doc := &drawing.Document{ModelSpace: []any{&entity.EntText{Text: "AA", Height: 2, Insertion: entity.Point3{X: 4, Y: 5}, AlignPt: &entity.Point2{X: 14, Y: 5}, HAlign: 5}}}
	p, err := PrepareDrawing(doc, RenderOptions{Width: 500, FontPath: f})
	if err != nil {
		t.Fatal(err)
	}
	plan := p.Texts[0]
	if math.Abs(plan.Glyphs[0].Matrix[4]-4) > 1e-8 || math.Abs(plan.Glyphs[1].Matrix[4]-9) > 1e-8 {
		t.Fatal("fit endpoints not used", plan.Glyphs)
	}
	doc.ModelSpace = []any{&entity.EntMText{Text: `\S1/2;`, TextHeight: 2, Attachment: 1}}
	p, err = PrepareDrawing(doc, RenderOptions{Width: 200, FontPath: f})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Texts[0].Decor) != 1 || len(p.Texts[0].Glyphs) != 2 {
		t.Fatal("stacked fraction layout missing")
	}
}

// TestNonFiniteGeometryIsReportedWithoutInvalidSVG 验证非有限几何产生诊断，且不会把无效坐标写入 SVG。
func TestNonFiniteGeometryIsReportedWithoutInvalidSVG(t *testing.T) {
	p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{&entity.EntCircle{Radius: math.NaN()}}}, RenderOptions{Width: 100})
	if err != nil {
		t.Fatal(err)
	}
	svg, err := p.SVG()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Primitives) != 0 || len(p.Audit.Diagnostics) != 1 || p.Audit.Diagnostics[0].Status != "invalid_geometry" || bytes.Contains(svg, []byte("NaN")) {
		t.Fatal("invalid geometry silently emitted")
	}
}

// TestRequestedOutlinePreservesArrowAndPattern 验证轮廓显示保留箭头和图案填充，公开显示覆盖且不修改源 HATCH。
func TestRequestedOutlinePreservesArrowAndPattern(t *testing.T) {
	square := []entity.Point2{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	h := &entity.EntHatch{SolidFill: true, Paths: []entity.HatchPath{{Points: square}}}
	solid := &entity.EntSolid{P1: entity.Point2{20, 0}, P2: entity.Point2{21, 0}, P3: entity.Point2{20, 1}, P4: entity.Point2{20, 1}}
	patterned := &entity.EntHatch{Paths: []entity.HatchPath{{Points: square}}, Deflines: []entity.HatchDefLine{{Offset: entity.Point2{Y: 1}}}}
	p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{h, solid, patterned}}, RenderOptions{Width: 400, FillMode: "outline"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Primitives) != 3 || p.Primitives[0].Kind != 0 || len(p.Primitives[0].Strokes) != 4 || p.Primitives[1].Kind != 2 || len(p.Primitives[2].Strokes) < 8 {
		t.Fatal("outline mode changed arrow/pattern behavior")
	}
	found := false
	for _, d := range p.Audit.Diagnostics {
		if d.Status == "display_override" {
			found = true
		}
	}
	if !found {
		t.Fatal("display override not disclosed")
	}
	if !h.SolidFill {
		t.Fatal("source hatch mutated")
	}
}
