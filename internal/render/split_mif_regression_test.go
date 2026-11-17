package render

import (
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"strings"
	"testing"

	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
)

// TestSplitMIFReachesGlyphLayout 验证拆分 MIF 解码结果进入字形布局，同时保持实体原始字符串。
func TestSplitMIFReachesGlyphLayout(t *testing.T) {
	raw := `\M+50086\U+631d\M+5BB00`
	for _, e := range []any{
		&entity.EntText{Text: raw, Height: 2},
		&entity.EntAttrib{Text: raw, Height: 2},
		&entity.EntMText{Text: raw, TextHeight: 2, Attachment: 1},
	} {
		p, err := PrepareDrawing(&drawing.Document{ModelSpace: []any{e}}, RenderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var got strings.Builder
		for _, plan := range p.Texts {
			for _, g := range plan.Glyphs {
				got.WriteRune(g.Rune)
			}
		}
		if got.String() != "單位" {
			t.Errorf("%T glyphs=%q, want 單位", e, got.String())
		}
		if len(p.Primitives) != 1 || p.Primitives[0].Lb.Tx.Raw != raw {
			t.Fatalf("%T source string was changed", e)
		}
	}
}

// TestLocalSplitEncodedTitleGlyphs 验证私有标题栏的拆分编码被完整还原为字形，并保留原始转义。
func TestLocalSplitEncodedTitleGlyphs(t *testing.T) {
	b, err := os.ReadFile(testfixtures.Path(t, "titles"))
	if err != nil {
		t.Skip("local drawing unavailable")
	}
	doc, err := drawing.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PrepareDrawing(doc, RenderOptions{Layout: testfixtures.Value(t, "titles.layout"), Plot: true, FillMode: "outline"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint64]string{0x23eb24: "單位", 0x23eb32: "片彎﹕", 0x26a0bd: "膠前圈壓鼻梁", 0x23ebea: "繪圖﹕", 0x23ebeb: "審核﹕"}
	seen := map[uint64]bool{}
	for i, primitive := range p.Primitives {
		expected, ok := want[primitive.Source.Handle]
		if !ok || !strings.HasPrefix(primitive.Source.Space, "paper:") {
			continue
		}
		seen[primitive.Source.Handle] = true
		var actual strings.Builder
		for _, g := range p.Texts[i].Glyphs {
			actual.WriteRune(g.Rune)
		}
		if actual.String() != expected {
			t.Errorf("title %X glyphs=%q want %q", primitive.Source.Handle, actual.String(), expected)
		}
		if !strings.Contains(primitive.Lb.Tx.Raw, `\M+`) {
			t.Errorf("title %X lost original encoded text", primitive.Source.Handle)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("title fields rendered %d, want %d", len(seen), len(want))
	}
}
