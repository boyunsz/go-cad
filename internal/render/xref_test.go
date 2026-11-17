package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestLocalMainExternalPartsLoadedAtOriginalInsertions 验证可选外参在原插入位置绘制，保留主图几何、文字、视口及外参来源诊断。
func TestLocalMainExternalPartsLoadedAtOriginalInsertions(t *testing.T) {
	files := map[string]string{}
	aliases := map[string]string{}
	for _, alias := range []string{"part-a", "part-b"} {
		path := testfixtures.Path(t, alias)
		files[testfixtures.Value(t, alias+".reference")] = path
		aliases[filepath.Clean(path)] = alias
	}
	b, e := os.ReadFile(testfixtures.Path(t, "drawing-b"))
	if e != nil {
		t.Fatal("cannot read host fixture")
	}
	d, e := drawing.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	p, e := PrepareDrawing(d, RenderOptions{Layout: testfixtures.Value(t, "drawing-b.layout"), Plot: true, FillMode: "outline", XrefFiles: files})
	if e != nil {
		t.Fatal(e)
	}
	base, e := PrepareDrawing(d, RenderOptions{Layout: testfixtures.Value(t, "drawing-b.layout"), Plot: true, FillMode: "outline"})
	if e != nil {
		t.Fatal(e)
	}
	if p.Audit.Bounds != base.Audit.Bounds {
		t.Fatal("parts changed main drawing framing")
	}
	j := 0
	for i, v := range p.Primitives {
		if v.Source.File != "" {
			continue
		}
		if j >= len(base.Primitives) || !reflect.DeepEqual(v, base.Primitives[j]) || !reflect.DeepEqual(p.Texts[i], base.Texts[j]) {
			t.Fatalf("pre-existing main primitive changed: %X", v.Source.Handle)
		}
		j++
	}
	if j != 4248 || j != len(base.Primitives) {
		t.Fatalf("pre-existing drawing lost content: %d", j)
	}
	counts := map[string]int{}
	texts := map[string]string{}
	for _, v := range p.Primitives {
		if v.Source.File != "" {
			n := aliases[filepath.Clean(v.Source.File)]
			if n == "" {
				t.Fatal("unconfigured external drawing was loaded")
			}
			counts[n]++
			texts[n] += v.Lb.Text
			if len(v.Source.XrefPath) == 0 {
				t.Fatal("external instance has no host insertion provenance")
			}
		}
	}
	for n, want := range map[string]int{"part-a": 65, "part-b": 38} {
		if counts[n] != want {
			t.Errorf("%s geometry changed: got %d want %d", n, counts[n], want)
		}
	}
	if len(p.Audit.ExternalDrawings) != 2 {
		t.Fatal("external source inventory incomplete")
	}
	for _, diag := range p.Audit.Diagnostics {
		for _, alias := range []string{"part-a", "part-b"} {
			ref := strings.TrimSuffix(testfixtures.Value(t, alias+".reference"), ".dwg")
			if diag.Status == "missing_resource" && strings.Contains(diag.Detail, ref) {
				t.Errorf("loaded fixture %s is still reported missing", alias)
			}
		}
	}
	for _, alias := range []string{"part-a", "part-b"} {
		if !strings.Contains(texts[alias], testfixtures.Value(t, alias+".title")) {
			t.Errorf("fixture %s lost its title or annotation", alias)
		}
	}
	if len(d.Xrefs) != 0 {
		t.Fatal("rendering mutated input document")
	}
	t.Logf("part primitive counts=%v", counts)
}
