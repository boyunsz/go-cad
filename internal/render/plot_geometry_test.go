package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"strings"
	"testing"
)

// TestClosedFrameAndPolylineDashPhase 验证闭合外框的 SVG 接缝及 PLINEGEN 对原顶点虚线相位的控制。
func TestClosedFrameAndPolylineDashPhase(t *testing.T) {
	for _, flags := range []uint16{512, 768} {
		d := &drawing.Document{ModelSpace: []any{&entity.EntLwPolyline{Flags: flags, Vertices: []entity.Point2{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 8}, {X: 0, Y: 8}}}}}
		p, err := PrepareDrawing(d, RenderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		path := p.geometryPath(p.Primitives[0].Geometry)
		if !strings.HasSuffix(path, "Z") {
			t.Fatalf("closed CAD frame must close its SVG join: %s", path)
		}
		dashed := p.geometryPathWithDash(p.Primitives[0].Geometry, true)
		moves := strings.Count(dashed, "M")
		want := 4
		if flags&256 != 0 {
			want = 1
		}
		if moves != want {
			t.Fatalf("PLINEGEN flags=%d moves=%d want=%d: %s", flags, moves, want, dashed)
		}
	}
}

// TestPaperStrokeIndependentOfRasterWidth 验证改变光栅输出宽度不会改变纸张空间线宽。
func TestPaperStrokeIndependentOfRasterWidth(t *testing.T) {
	e := &entity.EntLine{BaseEntity: entity.BaseEntity{Head: &entity.CommonEntityHead{Linewt: 8}}, End: entity.Point3{X: 20}}
	d := &drawing.Document{ModelSpace: []any{e}}
	for _, w := range []int{512, 4096} {
		p, err := PrepareDrawing(d, RenderOptions{Width: w})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.strokeWidth(p.Primitives[0]); got != .3 {
			t.Fatalf("width=%d changed physical lineweight to %g", w, got)
		}
	}
}
