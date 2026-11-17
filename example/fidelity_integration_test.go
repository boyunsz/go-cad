package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	cad "github.com/boyunsz/go-cad"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"io"
	"os"
	"strings"
	"testing"
)

// TestFaithfulLocalDWG 使用可选私有样本验证解析数量、实例来源、文字、曲线边界及 SVG 有效性。
func TestFaithfulLocalDWG(t *testing.T) {
	input := testfixtures.Path(t, "drawing-a")
	data, err := os.ReadFile(input)
	if os.IsNotExist(err) {
		t.Skip("local sample unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "6199d2b692293f9bd5edad3dd4627b9e7694b0f87aae47dac6c4942574f4be38" {
		t.Skip("local sample has changed; fixture-specific assertions skipped")
	}
	doc, err := cad.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.EntityCount() != 8295 || doc.Skipped() != 0 {
		t.Fatalf("source parse %d, skipped %d", doc.EntityCount(), doc.Skipped())
	}
	p, err := cad.PrepareDrawing(doc, cad.RenderOptions{Width: 4096, TextMode: "path", Bounds: "full"})
	if err != nil {
		t.Fatal(err)
	}
	leaders := map[uint64]bool{}
	for _, src := range inventory(doc) {
		if src.Type == "LEADER" {
			leaders[src.Handle] = true
		}
	}
	restoredArrows := 0
	for _, prim := range p.Primitives {
		for _, parent := range prim.Source.Inserts {
			if leaders[parent] {
				restoredArrows++
				break
			}
		}
	}
	// The previous 3082 instances remain, plus 19 source arrow blocks with
	// a solid HATCH and boundary each. Do not hide regressions by replacing
	// the old snapshot with the new total alone.
	if p.Audit.Primitives-restoredArrows != 3082 || restoredArrows != 38 {
		t.Fatalf("prior model=%d restored leader arrow primitives=%d", p.Audit.Primitives-restoredArrows, restoredArrows)
	}
	if doc.Ltscale <= 0 {
		t.Fatalf("header linetype scale not decoded: %v", doc.HeaderVars)
	}
	seen := map[uint64]bool{}
	var display strings.Builder
	for _, d := range p.Audit.Diagnostics {
		seen[d.Source.Handle] = true
	}
	for h := range doc.ByHandle {
		if !seen[h] {
			t.Fatalf("source %X has no disposition", h)
		}
	}
	for _, r := range p.Primitives {
		display.WriteString(r.Lb.Text)
		if r.Geometry != nil {
			b := r.Geometry.Bounds()
			v := p.Audit.Bounds
			if b.MinX < v.MinX || b.MaxX > v.MaxX || b.MinY < v.MinY || b.MaxY > v.MaxY {
				t.Fatalf("curve %X outside full viewport", r.Source.Handle)
			}
		}
	}
	for _, text := range []string{"技術要求", "拉直圖僅供參考", "SH-111701-A", "±"} {
		if !strings.Contains(display.String(), text) {
			t.Fatalf("missing original text %q", text)
		}
	}
	for _, f := range p.Audit.Fonts {
		if f.Missing != "" {
			t.Fatalf("missing glyphs handle %X: %q", f.Handle, f.Missing)
		}
	}
	for _, soft := range []bool{false, true} {
		p.Options.SoftPalette = soft
		svg, err := p.SVG()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(svg, []byte("\uFFFD")) {
			t.Fatal("replacement character")
		}
		d := xml.NewDecoder(bytes.NewReader(svg))
		for {
			_, err = d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("source=%d records=%d primitives=%d glyphs=%d viewport=%dx%d LTSCALE=%g", doc.EntityCount(), p.Audit.SourceEntities, p.Audit.Primitives, p.Audit.Glyphs, p.Audit.Width, p.Audit.Height, doc.Ltscale)
}

// TestLocalDWGMarkedRegionsRemainOutlines 验证私有样本指定区域保持轮廓显示，尺寸箭头仍保留实心填充。
func TestLocalDWGMarkedRegionsRemainOutlines(t *testing.T) {
	data, err := os.ReadFile(testfixtures.Path(t, "drawing-a"))
	if os.IsNotExist(err) {
		t.Skip("local sample unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	doc, err := cad.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cad.PrepareDrawing(doc, cad.RenderOptions{Width: 4096, FillMode: "outline"})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[uint64]bool{0x2A8C6B: false, 0x2A8C79: false, 0x2A8BD0: false}
	arrows := 0
	for _, r := range p.Primitives {
		if _, ok := wanted[r.Source.Handle]; ok {
			if r.Kind != 0 || len(r.Strokes) == 0 {
				t.Fatalf("marked area %X is still filled", r.Source.Handle)
			}
			wanted[r.Source.Handle] = true
		}
		if r.Kind0 == "SOLID" && r.Kind == 2 {
			arrows++
		}
	}
	for h, ok := range wanted {
		if !ok {
			t.Fatalf("marked region %X omitted", h)
		}
	}
	if arrows == 0 {
		t.Fatal("dimension arrows lost")
	}
	t.Logf("all 3 marked-region checks passed; %d SOLID arrows retained", arrows)
}

// TestLocalPaperTitleAttributesAreCompleteAndUnique 验证私有样本标题栏属性完整且唯一，布局间的修订字段不会串入其他页面。
func TestLocalPaperTitleAttributesAreCompleteAndUnique(t *testing.T) {
	data, err := os.ReadFile(testfixtures.Path(t, "drawing-a"))
	if os.IsNotExist(err) {
		t.Skip("sample unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	doc, err := cad.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cad.PrepareDrawing(doc, cad.RenderOptions{Width: 1600, FillMode: "outline", Layout: testfixtures.Value(t, "drawing-a.layout")})
	if err != nil {
		t.Fatal(err)
	}
	count := map[uint64]int{}
	text := map[string]bool{}
	for _, v := range p.Primitives {
		if v.Source.Space == "paper:"+testfixtures.Value(t, "drawing-a.layout") && v.Kind == 1 {
			count[v.Source.Handle]++
			text[v.Lb.Text] = true
		}
	}
	if count[0x34D9C] != 1 || count[0x1D0740] != 0 || count[0x1C0F22] != 0 || count[0x1C0F23] != 0 {
		t.Fatal("title defaults or duplicate values rendered", count[0x34D9C])
	}
	for _, s := range []string{"143-00-135", "10234", "MM6", "SUN-C", "7 Base", "P1/2"} {
		if !text[s] {
			t.Fatalf("missing title value %s", s)
		}
	}
	// Revision headings are real attributes; blank values are source data, not
	// missing text to be filled from ATTDEF defaults or from another layout.
	for _, h := range []uint64{0x32349, 0x3234A, 0x3234B, 0x3234C, 0x3234D, 0x28A8E0, 0x28A8E2, 0x28A8E3, 0x28A8E4} {
		if count[h] != 1 {
			t.Fatalf("revision attribute %X rendered %d times", h, count[h])
		}
	}
	want := map[uint64]string{0x3234A: "更改編號", 0x3234B: "更改內容", 0x3234D: "更改人", 0x28A8E0: "01", 0x28A8E1: "", 0x28A8E2: "-", 0x28A8E3: "-", 0x28A8E4: "-", 0x258FED: "", 0x258FEE: "08/09/2023", 0x258FEF: ""}
	for _, a := range doc.AttributeRecords() {
		if value, ok := want[a.Handle]; ok {
			if a.Type != "ATTRIB" || a.Value != value {
				t.Fatalf("revision source %X = %q, want %q", a.Handle, a.Value, value)
			}
			delete(want, a.Handle)
		}
	}
	if len(want) != 0 {
		t.Fatalf("revision source attributes missing: %v", want)
	}
	other, err := cad.PrepareDrawing(doc, cad.RenderOptions{Width: 800, Layout: testfixtures.Value(t, "drawing-a.other-layout")})
	if err != nil {
		t.Fatal(err)
	}
	dateCount := 0
	for _, v := range other.Primitives {
		if v.Source.Space == "paper:"+testfixtures.Value(t, "drawing-a.other-layout") && v.Source.Handle == 0x258FEE && v.Lb.Text == "08/09/2023" {
			dateCount++
		}
	}
	if dateCount != 1 || count[0x258FEE] != 0 {
		t.Fatal("revision date omitted or leaked across layouts")
	}
}
