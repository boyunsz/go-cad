package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"testing"
)

// TestNonPlotBlockInheritanceAndViewportDashScale 验证块的不可打印属性向下继承，视口虚线比例遵守 PSLTSCALE。
func TestNonPlotBlockInheritanceAndViewportDashScale(t *testing.T) {
	d := &Document{Ltscale: 10, HeaderVars: map[string]any{"PSLTSCALE": uint8(1)}, LayerColors: map[uint64]LayerColor{1: {Name: "0"}, 2: {Name: "construction", NoPlot: true, Ltype: 50}, 3: {Name: "visible", Ltype: 50}}, InternalObjs: map[uint64]*object.ObjGeneric{50: {Fields: []object.ObjField{{Key: "dash_lengths", Val: []float64{2, -1}}}}}}
	ts := NewTessellator(d)
	ts.ViewportScale = .5
	parent, c, l := ts.resolveStyle(&entity.BaseEntity{Layer: 2})
	ts.parentStyle, ts.parentColor, ts.parentLayer = parent, c, l
	for _, layer := range []uint64{1, 3} {
		s, _, _ := ts.resolveStyle(&entity.BaseEntity{Layer: layer})
		if !s.NoPlot {
			t.Fatal("non-plot INSERT leaked children")
		}
	}
	ts.parentStyle = ResolvedStyle{}
	ts.parentLayer = 0
	s, _, _ := ts.resolveStyle(&entity.BaseEntity{Layer: 3, Head: &entity.CommonEntityHead{LtypeScale: 2}})
	if s.Dash[0] != 40 {
		t.Fatalf("PSLTSCALE=1: %v", s.Dash)
	}
	d.HeaderVars["PSLTSCALE"] = uint8(0)
	s, _, _ = ts.resolveStyle(&entity.BaseEntity{Layer: 3, Head: &entity.CommonEntityHead{LtypeScale: 2}})
	if s.Dash[0] != 20 {
		t.Fatalf("PSLTSCALE=0: %v", s.Dash)
	}
}
