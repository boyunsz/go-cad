package drawing

import (
	"testing"

	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
)

// TestEntityVisibilityUsesLowBit 验证实体不可见标志仅使用最低位，其他状态位不误隐藏图元。
func TestEntityVisibilityUsesLowBit(t *testing.T) {
	for _, flags := range []int16{0, 1, 2, 3} {
		l := lineFor(1, 0)
		l.Head = &entity.CommonEntityHead{Invisible: flags}
		d := &Document{ModelSpace: []any{l}}
		sc := BuildScene(d, .01)
		visible := len(sc.Primitives) == 1
		if visible != (flags&1 == 0) {
			t.Errorf("invisible flags=%d: visible=%v", flags, visible)
		}
	}
}

// TestUnloadedXrefIsMissingResource 验证仅含结构标记的外参被报告为缺失资源，而非已成功绘制。
func TestUnloadedXrefIsMissingResource(t *testing.T) {
	i := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 10, TypeName: "INSERT"}, BlockHeader: 20, Scale: entity.Point3{X: 1, Y: 1, Z: 1}}
	d := &Document{ModelSpace: []any{i}, Blocks: map[uint64][]any{20: {
		&entity.EntBlockLike{BaseEntity: entity.BaseEntity{Handle: 21, TypeName: "BLOCK"}},
		&entity.EntBlockLike{BaseEntity: entity.BaseEntity{Handle: 22, TypeName: "ENDBLK"}},
	}}, InternalObjs: map[uint64]*object.ObjGeneric{20: {Name: "BLOCK_HEADER", Fields: []object.ObjField{
		{Key: "name", Val: "part"}, {Key: "xref_pname", Val: "K:\\parts\\part.dwg"},
	}}}}
	sc := BuildScene(d, .01)
	for _, diag := range sc.Diagnostics {
		if diag.Source.Handle == 10 && diag.Status == "missing_resource" {
			return
		}
	}
	t.Fatal("xref containing only BLOCK/ENDBLK must report missing external geometry")
}
