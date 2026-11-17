package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"testing"
)

// TestFontFamilyFromStyleExtendedData 验证字体族名称可从 STYLE 扩展数据恢复。
func TestFontFamilyFromStyleExtendedData(t *testing.T) {
	d := &Document{InternalObjs: map[uint64]*object.ObjGeneric{18: {Name: "APPID", Fields: []object.ObjField{{Key: "name", Val: "ACAD"}}}, 3837: {Name: "STYLE", Fields: []object.ObjField{{Key: "font_file", Val: ""}, {Key: "eed[0].handle", Val: []uint64{5, 1, 18}}, {Key: "eed[0].code", Val: int64(0)}, {Key: "eed[0].value", Val: "PMingLiU"}, {Key: "eed[1].code", Val: int64(71)}, {Key: "eed[1].value", Val: int64(34834)}}}}, ModelSpace: []any{&entity.EntAttrib{BaseEntity: entity.BaseEntity{Handle: 1, TypeName: "ATTRIB"}, Text: "更改內容", Height: 2.5, StyleHandle: 3837}}}
	s := BuildScene(d, .01)
	if len(s.Primitives) != 1 || s.Primitives[0].Lb.Tx.FontName != "PMingLiU" {
		t.Fatal("STYLE extended font family was lost")
	}
}
