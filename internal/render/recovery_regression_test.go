package render

import (
	"bytes"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestClippedBoundsExcludeOutsideObjects 验证视口外图元不会因无效交集扩大最终取景范围。
func TestClippedBoundsExcludeOutsideObjects(t *testing.T) {
	b := emptyBounds()
	extendBox(&b, drawing.Box2{MinX: 10, MaxX: 5, MinY: 0, MaxY: 10})
	if !b.Invalid() {
		t.Fatal("inverted intersection expanded viewport", b)
	}
}

// TestViewportClipsSVGAndRaster 验证纸张视口裁切同时作用于 SVG 和栅格输出。
func TestViewportClipsSVGAndRaster(t *testing.T) {
	clip := &drawing.Box2{MinX: 4, MinY: 4, MaxX: 6, MaxY: 6}
	p := &PreparedDrawing{Doc: &drawing.Document{}, Options: RenderOptions{Background: color.RGBA{255, 255, 255, 255}}, Audit: RenderAudit{Width: 100, Height: 100, Bounds: drawing.Box2{MaxX: 10, MaxY: 10}}, Primitives: []drawing.Primitive{{Kind: 0, Clip: clip, Strokes: []drawing.Stroke{{X1: 0, Y1: 5, X2: 10, Y2: 5}}}}}
	b, e := p.PNG()
	if e != nil {
		t.Fatal(e)
	}
	im, e := png.Decode(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		x    int
		dark bool
	}{{20, false}, {50, true}, {80, false}} {
		r, _, _, _ := im.At(v.x, 50).RGBA()
		if (r < 32768) != v.dark {
			t.Fatal(v, r)
		}
	}
	svg, _ := p.SVG()
	if !bytes.Contains(svg, []byte(`clip-path="url(#vp0)"`)) {
		t.Fatal("SVG viewport not clipped")
	}
}

// TestTextWordsTabsAndLineSpacing 验证单词边界、制表和行距处理，避免替代字体度量拆散完整单词。
func TestTextWordsTabsAndLineSpacing(t *testing.T) {
	f := testFont(t)
	d := &drawing.Document{ModelSpace: []any{&entity.EntMText{Text: "INJECTION", TextHeight: 1, RectWidth: 3, Attachment: 1, LineFactor: 1}}}
	p, e := PrepareDrawing(d, RenderOptions{Width: 200, FontPath: f})
	if e != nil {
		t.Fatal(e)
	}
	gs := p.Texts[0].Glyphs
	if len(gs) != 9 {
		t.Fatal(len(gs))
	}
	for _, g := range gs {
		if g.Matrix[5] != gs[0].Matrix[5] {
			t.Fatal("split Latin word to fit replacement metrics")
		}
	}
	d.ModelSpace[0].(*entity.EntMText).Text = "A\tB\nC"
	d.ModelSpace[0].(*entity.EntMText).RectWidth = 0
	p, e = PrepareDrawing(d, RenderOptions{Width: 200, FontPath: f})
	if e != nil {
		t.Fatal(e)
	}
	gs = p.Texts[0].Glyphs
	if len(gs) != 3 || gs[1].Matrix[4] <= gs[0].Matrix[4] || math.Abs(gs[0].Matrix[5]-gs[2].Matrix[5]-5.0/3) > 1e-10 {
		t.Fatal(gs)
	}
}

// TestExplicitFontMap 验证显式字体映射被采用并记录相应诊断。
func TestExplicitFontMap(t *testing.T) {
	path := testFont(t)
	r := newFontResolver(RenderOptions{FontMap: map[string]string{"romand.shx": path}})
	f, actual, _ := r.choose("romand.shx")
	if f == nil || actual != path {
		t.Fatal(actual)
	}
}

// TestRasterResourceIsEmbeddedAndClipped 验证外部位图被嵌入输出，并保留垂直方向和源裁切范围。
func TestRasterResourceIsEmbeddedAndClipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})
	src.Set(0, 1, color.RGBA{0, 0, 255, 255})
	src.Set(1, 0, color.RGBA{0, 255, 0, 255})
	src.Set(1, 1, color.White)
	var buf bytes.Buffer
	png.Encode(&buf, src)
	os.WriteFile(path, buf.Bytes(), 0600)
	e := &entity.EntImage{ImageDef: 100, ImageSize: entity.Point2{X: 2, Y: 2}, Uvec: entity.Point3{X: 1}, Vvec: entity.Point3{Y: 1}}
	r := &drawing.RasterGeometry{Image: e, Path: `C:\old\test.png`, Transform: drawing.IdentityXform()}
	im, err := prepareImage(r, RenderOptions{ResourceDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if im.Matrix[0] != 1 || im.Matrix[3] != -1 || im.Matrix[4] != -.5 || im.Matrix[5] != 1.5 {
		t.Fatal(im.Matrix)
	}
	p := &PreparedDrawing{Doc: &drawing.Document{}, Options: RenderOptions{Background: color.RGBA{255, 255, 255, 255}}, Audit: RenderAudit{Width: 100, Height: 100, Bounds: im.Bounds}, Primitives: []drawing.Primitive{{Kind: 4}}, Images: map[int]*preparedImage{0: im}}
	svg, err := p.SVG()
	if err != nil || !bytes.Contains(svg, []byte("data:image/png;base64,")) {
		t.Fatal(err)
	}
	pngData, err := p.PNG()
	if err != nil {
		t.Fatal(err)
	}
	out, _ := png.Decode(bytes.NewReader(pngData))
	r0, g0, b0, _ := out.At(10, 10).RGBA()
	if r0 <= g0 || r0 <= b0 {
		t.Fatal("image vertical orientation changed")
	}
	e.Clipping = true
	e.ClipBoundaryType = 1
	e.ClipVerts = []entity.Point2{{X: -.5, Y: -.5}, {X: .5, Y: 1.5}}
	im, err = prepareImage(r, RenderOptions{ResourceDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, a := im.Pixels.At(1, 0).RGBA()
	if a != 0 {
		t.Fatal("source image clipping ignored")
	}
}
