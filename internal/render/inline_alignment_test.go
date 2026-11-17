package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"testing"
)

// TestInlineToleranceAlignment 验证不同字号的内联公差按中间对齐规则布置。
func TestInlineToleranceAlignment(t *testing.T) {
	d := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: `\A1;H{\H1.5;H}`, TextHeight: 2.5, Attachment: 1}}}
	p, e := PrepareDrawing(d, RenderOptions{FontPath: testFont(t)})
	if e != nil {
		t.Fatal(e)
	}
	g := p.Texts[0].Glyphs
	if len(g) != 2 || math.Abs(g[1].Matrix[5]-g[0].Matrix[5]-.5) > .001 {
		t.Fatalf("small tolerance should be centered: %+v", g)
	}
}
