package drawing

import (
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"testing"
)

// TestLocalR2004LayoutReferences 验证私有 R2004 样本的布局块、活动视口和属性归属，缺失外参须有诊断。
func TestLocalR2004LayoutReferences(t *testing.T) {
	b, err := os.ReadFile(testfixtures.Path(t, "drawing-b"))
	if os.IsNotExist(err) {
		t.Skip("R2004 local sample unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ByHandle) != 7059 || d.Skipped() != 0 {
		t.Fatalf("source records lost: records=%d skipped=%d", len(d.ByHandle), d.Skipped())
	}
	seen := map[string]bool{}
	for _, l := range d.Layouts() {
		seen[l.Name] = true
		switch l.Name {
		case "Model":
			if !l.Model || l.Block != 0x1F || l.ActiveViewport != 0x2A1D01 {
				t.Fatalf("wrong model reference: %+v", l)
			}
		case testfixtures.Value(t, "drawing-b.layout"):
			if l.Model || l.Block != 0x2B7AD || l.ActiveViewport != 0x2B7B1 || l.Entities == 0 {
				t.Fatalf("wrong paper reference: %+v", l)
			}
		}
	}
	if !seen["Model"] || !seen[testfixtures.Value(t, "drawing-b.layout")] {
		t.Fatal("missing layout", seen)
	}
	sc, err := BuildLayoutScene(d, testfixtures.Value(t, "drawing-b.layout"), .01)
	if err != nil || len(sc.Primitives) == 0 {
		t.Fatal("paper layout exported blank", err)
	}
	for _, h := range []uint64{2888781, 2888963, 2889224} {
		n := 0
		for _, p := range sc.Primitives {
			if p.Source.Handle == h {
				n++
			}
		}
		if n != 1 {
			t.Errorf("visible part-number attribute %X rendered %d times, want 1", h, n)
		}
	}
	for _, h := range []uint64{2887619, 2887632} {
		missing := false
		for _, diag := range sc.Diagnostics {
			if diag.Source.Handle == h && diag.Status == "missing_resource" {
				missing = true
			}
		}
		if !missing {
			t.Errorf("uncached external part %X was not reported", h)
		}
	}
}
