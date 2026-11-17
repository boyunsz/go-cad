package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"golang.org/x/image/font/gofont/gomono"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestCADTextHeightUsesCapitalHeight 验证 CAD 字高按大写字母可见高度缩放，而不是直接使用字体 em 高度。
func TestCADTextHeightUsesCapitalHeight(t *testing.T) {
	d := &drawing.Document{ModelSpace: []any{&entity.EntText{Text: "H", Height: 2}}}
	p, err := PrepareDrawing(d, RenderOptions{FontPath: testFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	b := p.Texts[0].Bounds
	if math.Abs(b.MaxY-b.MinY-2) > .01 {
		t.Fatalf("CAD height 2 produced visible H height %.6f", b.MaxY-b.MinY)
	}
}

// TestCADCapitalHeightAcrossKindsAndAffineTransforms 验证不同文字类型及仿射变换下的大写字高均符合 CAD 字高约定。
func TestCADCapitalHeightAcrossKindsAndAffineTransforms(t *testing.T) {
	mono := filepath.Join(t.TempDir(), "Go-Mono.ttf")
	if e := os.WriteFile(mono, gomono.TTF, 0600); e != nil {
		t.Fatal(e)
	}
	for _, file := range []string{testFont(t), mono} {
		for _, kind := range []string{"TEXT", "ATTRIB", "MTEXT"} {
			for _, basis := range [][4]float64{{1, 0, 0, 1}, {0, 2, -3, 0}, {0, -2, 3, 0}} {
				p := drawing.Primitive{Kind: 1, Kind0: kind}
				p.Lb.Tx = &drawing.GlyphTextInfo{Raw: "H", HWorld: 2, Ux: basis[0], Uy: basis[1], Vx: basis[2], Vy: basis[3], WidthFactor: 1}
				plan, _ := layoutFaithfulText(p, newFontResolver(RenderOptions{FontPath: file}))
				got, want := plan.Bounds.MaxY-plan.Bounds.MinY, 2.0
				if basis[0] == 0 {
					got = plan.Bounds.MaxX - plan.Bounds.MinX
					want = 6
				}
				if math.Abs(got-want) > want*.01 {
					t.Fatalf("%s %s basis=%v height=%g want=%g", file, kind, basis, got, want)
				}
			}
		}
	}
}

// TestMTextWrapUsesCADHeight 验证 MTEXT 按 CAD 字高和列宽换行，不通过整体缩字挤成一行。
func TestMTextWrapUsesCADHeight(t *testing.T) {
	d := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: "H H", TextHeight: 2, RectWidth: 4, Attachment: 1}}}
	p, err := PrepareDrawing(d, RenderOptions{FontPath: testFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	g := p.Texts[0].Glyphs
	if len(g) != 3 || math.Abs(g[0].Matrix[5]-g[2].Matrix[5]) < 3 {
		t.Fatal("text was shrunk into one line instead of wrapping at the CAD width")
	}
}
