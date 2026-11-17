package render

import (
	"bytes"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"testing"
)

// TestPlotPreservesPreviewAndFiltersNonPlot 验证打印模式过滤不可打印图层，同时保持预览行为及设计几何。
func TestPlotPreservesPreviewAndFiltersNonPlot(t *testing.T) {
	line := func(h, l uint64) *entity.EntLine {
		return &entity.EntLine{BaseEntity: entity.BaseEntity{Handle: h, Layer: l, TypeName: "LINE"}, End: entity.Point3{X: 10, Y: float64(h)}}
	}
	d := &drawing.Document{LayerColors: map[uint64]drawing.LayerColor{1: {Name: "Defpoints"}, 2: {Name: "Guide", NoPlot: true, PlotKnown: true}, 3: {Name: "Print"}}, ModelSpace: []any{line(1, 1), line(2, 2), line(3, 3)}}
	a, e := PrepareDrawing(d, RenderOptions{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := PrepareDrawing(d, RenderOptions{Plot: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(a.Primitives) != 3 || len(b.Primitives) != 1 || b.Primitives[0].Source.Handle != 3 {
		t.Fatal("print filter removed design geometry or changed preview")
	}
}

// TestLocalPlottedMainFrame 验证私有主图的外框、箭头、线宽和辅助图层过滤，并检查输出仍为矢量。
func TestLocalPlottedMainFrame(t *testing.T) {
	b, e := os.ReadFile(testfixtures.Path(t, "drawing-b"))
	if os.IsNotExist(e) {
		t.Skip("local sample unavailable")
	}
	if e != nil {
		t.Fatal(e)
	}
	d, e := drawing.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	p, e := PrepareDrawing(d, RenderOptions{Plot: true, Layout: testfixtures.Value(t, "drawing-b.layout"), FillMode: "outline", Monochrome: true})
	if e != nil {
		t.Fatal(e)
	}
	visible := map[uint64]bool{}
	leaderArrow := 0
	widths := map[float64]int{}
	for _, v := range p.Primitives {
		if v.Kind == 2 {
			for _, h := range v.Source.Inserts {
				if h == 0x2BB52B {
					leaderArrow++
				}
			}
		}
		visible[v.Source.Handle] = true
		if v.Kind == 0 {
			widths[v.Style.LineMM]++
		}
	}
	if leaderArrow != 1 {
		t.Errorf("section D source LEADER arrow missing: %d", leaderArrow)
	}
	for _, h := range []uint64{0x1D0DED, 0x2291D6, 0x1D0DEB} {
		if visible[h] {
			t.Errorf("non-plot helper %X visible", h)
		}
	}
	for _, h := range []uint64{0x1D0DEA, 0x1D0DEC} {
		if !visible[h] {
			t.Errorf("source frame %X lost", h)
		}
	}
	if len(widths) < 4 {
		t.Errorf("lineweights collapsed: %v", widths)
	}
	s, e := p.SVG()
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(s, []byte("<image")) {
		t.Fatal("main drawing unexpectedly rasterized")
	}
}
