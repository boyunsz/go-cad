package drawing

import (
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"testing"
)

// TestR2004SourcePlotStyles 验证私有 R2004 样本的全局线型比例、图层线宽和打印属性解析。
func TestR2004SourcePlotStyles(t *testing.T) {
	b, e := os.ReadFile(testfixtures.Path(t, "drawing-b"))
	if os.IsNotExist(e) {
		t.Skip("local source unavailable")
	}
	if e != nil {
		t.Fatal(e)
	}
	d, e := Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	if d.Ltscale != 10 {
		t.Errorf("source LTSCALE=10, got %v", d.Ltscale)
	}
	if d.HeaderVars["PSLTSCALE"] != uint8(1) || d.HeaderVars["CELTSCALE"] != float64(1) {
		t.Fatal(d.HeaderVars)
	}
	for _, h := range []uint64{2061, 1591, 222} {
		l := d.LayerColors[h]
		if !l.StyleKnown || !l.PlotKnown {
			t.Errorf("layer %X %s lost original plot style", h, l.Name)
		}
	}
	if l := d.LayerColors[2061]; l.Ltype != 209200 || l.Lineweight != 4 {
		t.Errorf("Defpoints lost style: %+v", l)
	}
}
