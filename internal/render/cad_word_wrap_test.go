package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"os"
	"testing"
)

// TestLocalSourceISOToleranceDoesNotWrapTrailingBearing 验证标准编号末尾文字不会因字形侧边度量而错误换行。
func TestLocalSourceISOToleranceDoesNotWrapTrailingBearing(t *testing.T) {
	const file = "/System/Library/Fonts/Supplemental/Verdana.ttf"
	if _, err := os.Stat(file); err != nil {
		t.Skip("local source font unavailable")
	}
	// The real source MTEXT has 1.35 height, H0.8519x, 9.10805843 width.
	// Its stored visible extents are 9.02055684; the final character advance
	// includes blank right bearing and must not force another line.
	d := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: `{\H0.8519x;ISO 2768-1}`, TextHeight: 1.35, RectWidth: 9.10805843041535, Attachment: 1}}}
	p, e := PrepareDrawing(d, RenderOptions{FontPath: file})
	if e != nil {
		t.Fatal(e)
	}
	g := p.Texts[0].Glyphs
	if len(g) != 10 {
		t.Fatal(len(g))
	}
	if g[0].Matrix[5] != g[9].Matrix[5] {
		d.ModelSpace[0].(*entity.EntMText).RectWidth = 100
		wide, _ := PrepareDrawing(d, RenderOptions{FontPath: file})
		t.Logf("unwrapped bounds: %+v glyph first/last=%+v / %+v", wide.Texts[0].Bounds, wide.Texts[0].Glyphs[0].Matrix, wide.Texts[0].Glyphs[9].Matrix)
		t.Fatal("ISO standard number unexpectedly wrapped")
	}
}
