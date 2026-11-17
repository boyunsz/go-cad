package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"testing"
)

// TestMTextUsesStyleWidthWithoutDoublingTextEntityWidth 验证 MTEXT 使用 STYLE 宽度，TEXT/ATTRIB 不重复乘入已有实体宽度。
func TestMTextUsesStyleWidthWithoutDoublingTextEntityWidth(t *testing.T) {
	d := &Document{InternalObjs: map[uint64]*object.ObjGeneric{7: {Fields: []object.ObjField{{Key: "width_factor", Val: 0.9}}}}}
	ts := NewTessellator(d)
	for _, tc := range []struct {
		e    any
		want float64
	}{
		{&entity.EntMText{StyleHandle: 7}, .9},
		{&entity.EntText{StyleHandle: 7, WidthFactor: .8}, .8},
		{&entity.EntAttrib{StyleHandle: 7, WidthFactor: .7}, .7},
	} {
		p := Primitive{Lb: Label{Tx: &GlyphTextInfo{WidthFactor: 1}}}
		ts.textStyle(tc.e, &p)
		if p.Lb.Tx.WidthFactor != tc.want {
			t.Errorf("%T width=%g want %g", tc.e, p.Lb.Tx.WidthFactor, tc.want)
		}
	}
}

// TestLayoutNameDecodesMIFAndRetainsSource 验证布局名称的 MIF 显示解码，同时保留原始名称。
func TestLayoutNameDecodesMIFAndRetainsSource(t *testing.T) {
	raw := `\M+5D6D0\M+5C1BA`
	g := &object.ObjGeneric{Name: "LAYOUT", Handles: []uint64{2, 0}, Fields: []object.ObjField{{Key: "layout_name", Val: raw}}}
	d := &Document{InternalObjs: map[uint64]*object.ObjGeneric{1: g}}
	l := d.Layouts()
	if len(l) != 1 || l[0].Name != "中梁" || l[0].RawName != raw || g.Field("layout_name") != raw {
		t.Fatalf("layout decode changed source or lost display: %+v", l)
	}
}
