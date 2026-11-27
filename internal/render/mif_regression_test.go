package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"math"
	"os"
	"strings"
	"testing"
)

// TestMIFEscapesInRenderedTextAndTolerance 验证文字、属性及 MTEXT 中的 MIF 公差符号进入正确的字形布局。
func TestMIFEscapesInRenderedTextAndTolerance(t *testing.T) {
	for _, ent := range []any{
		&entity.EntText{Text: `8.9\M+5A1C00.1`, Height: 2},
		&entity.EntAttrib{Text: `8.9\M+5A1C00.1`, Height: 2},
		&entity.EntMText{Text: `\A1;8.9{\H1.500000;\M+5A1C00.1}`, TextHeight: 2, Attachment: 1},
	} {
		p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{ent}}, RenderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var actual strings.Builder
		for _, plan := range p.Texts {
			for _, g := range plan.Glyphs {
				actual.WriteRune(g.Rune)
			}
		}
		if actual.String() != "8.9±0.1" {
			t.Errorf("%T rendered %q, want 8.9±0.1", ent, actual.String())
		}
	}
}

// TestLocalChineseNotesDoNotOverlap 验证私有样本中文说明的行数和包围盒，避免说明与相邻标题重叠。
func TestLocalChineseNotesDoNotOverlap(t *testing.T) {
	b, err := os.ReadFile(testfixtures.Path(t, "notes"))
	if err != nil {
		t.Skip("local drawing unavailable")
	}
	doc, err := drawing.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PrepareDrawing(doc, RenderOptions{Layout: testfixtures.Value(t, "notes.layout"), Plot: true, Monochrome: true, FillMode: "outline", FontMap: map[string]string{"PMingLiU": "/System/Library/Fonts/Supplemental/Songti.ttc#STSong"}})
	if err != nil {
		t.Fatal(err)
	}
	boxes := map[uint64]drawing.Box2{}
	for i, v := range p.Primitives {
		if v.Source.Handle != 0x2f3207 && v.Source.Handle != 0x2f314b && v.Source.Handle != 0x2f314d {
			continue
		}
		plan := p.Texts[i]
		boxes[v.Source.Handle] = plan.Bounds
		rows := map[int64]bool{}
		for _, g := range plan.Glyphs {
			rows[int64(math.Round(g.Matrix[5]*1e5))] = true
		}
		t.Logf("note %X bounds=%+v rows=%d", v.Source.Handle, plan.Bounds, len(rows))
		if v.Source.Handle == 0x2f3207 && len(rows) != 2 {
			t.Errorf("customer note wrapped into %d lines, want source/PDF two lines", len(rows))
		}
	}
	if len(boxes) != 3 {
		t.Fatalf("missing note instances: %v", boxes)
	}
	for _, h := range []uint64{0x2f314b, 0x2f314d} {
		if boxes[0x2f3207].MinY <= boxes[h].MaxY {
			t.Errorf("customer note overlaps subheading %X: %+v vs %+v", h, boxes[0x2f3207], boxes[h])
		}
	}
}

// TestMTextBlankParagraphsRetainAttachmentSpacing 验证首尾空段落仍参与 MTEXT 附着点和行距计算。
func TestMTextBlankParagraphsRetainAttachmentSpacing(t *testing.T) {
	for _, tc := range []struct {
		attachment uint16
		raw        string
		shift      float64
	}{
		{1, `\PA`, -10.0 / 3}, {1, `\P\PA`, -20.0 / 3},
		{4, `\PA`, -5.0 / 3}, {4, `A\P`, 5.0 / 3},
		{7, `A\P`, 10.0 / 3}, {7, `A\P\P`, 20.0 / 3},
	} {
		bounds := func(raw string) drawing.Box2 {
			p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{&entity.EntMText{Text: raw, TextHeight: 2, Attachment: tc.attachment}}}, RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, plan := range p.Texts {
				return plan.Bounds
			}
			t.Fatal("no text")
			return drawing.Box2{}
		}
		a, b := bounds("A"), bounds(tc.raw)
		if math.Abs(b.MinY-a.MinY-tc.shift) > 1e-6 {
			t.Errorf("attachment=%d text=%q shift=%g want %g", tc.attachment, tc.raw, b.MinY-a.MinY, tc.shift)
		}
	}
}

// TestMTextInlineWidthOverridesStyleAndRelativeWidthMultiplies 验证绝对宽度指令覆盖 STYLE 宽度，相对宽度指令按比例相乘。
func TestMTextInlineWidthOverridesStyleAndRelativeWidthMultiplies(t *testing.T) {
	width := func(raw string, style float64) float64 {
		p := drawing.Primitive{Kind0: "MTEXT", Lb: drawing.Label{Tx: &drawing.GlyphTextInfo{Raw: raw, HWorld: 2, WidthFactor: style, Ux: 1, Vy: 1, Attachment: 1}}}
		plan, _ := layoutFaithfulText(p, newFontResolver(RenderOptions{}))
		return plan.Bounds.MaxX - plan.Bounds.MinX
	}
	base := width("ABC", 1)
	for _, tc := range []struct {
		raw  string
		want float64
	}{{"ABC", .9}, {`\W.8;ABC`, .8}, {`\W.8x;ABC`, .72}} {
		got := width(tc.raw, .9) / base
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%q width ratio=%g want %g", tc.raw, got, tc.want)
		}
	}
}

// TestLayerNameMIFEscapesDecodedInSVGAndSheetTexts 验证图层名中的 MIF 转义在 SVG 和文本导出中均被正确解码。
func TestLayerNameMIFEscapesDecodedInSVGAndSheetTexts(t *testing.T) {
	raw := `\M+5A5EC\M+5A5A4\M+5A5E4\M+5A960 1`
	want := "レイヤー 1"
	doc := &drawing.Document{
		LayerColors: map[uint64]drawing.LayerColor{1: {Name: raw, Index: 7}},
		ModelSpace: []any{
			&entity.EntLine{BaseEntity: entity.BaseEntity{Handle: 10, Layer: 1}, Start: entity.Point3{X: 0, Y: 0}, End: entity.Point3{X: 10, Y: 0}},
			&entity.EntText{BaseEntity: entity.BaseEntity{Handle: 11, Layer: 1}, Text: "A", Height: 2},
		},
	}
	svg, err := RenderSVG(doc, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), want) || strings.Contains(string(svg), `\M+`) {
		t.Errorf("RenderSVG layer name not decoded: %s", string(svg))
	}
	p, err := PrepareDrawing(doc, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fsvgBytes, err := p.SVG()
	if err != nil {
		t.Fatal(err)
	}
	fsvg := string(fsvgBytes)
	if !strings.Contains(fsvg, `data-layer="`+want+`"`) || strings.Contains(fsvg, `\M+`) {
		t.Errorf("Faithful SVG data-layer not decoded: %s", fsvg)
	}
	texts := SheetTexts(doc, nil)
	if len(texts) != 1 || texts[0].Layer != want {
		t.Errorf("SheetTexts layer=%+v, want %q", texts, want)
	}
}
