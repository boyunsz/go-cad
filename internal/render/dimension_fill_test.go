package render

import (
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"testing"
)

// TestOutlineModeKeepsDimensionArrowHatch 验证轮廓模式只改变普通实心填充，标注箭头 HATCH 仍实心显示。
func TestOutlineModeKeepsDimensionArrowHatch(t *testing.T) {
	h := &entity.EntHatch{BaseEntity: entity.BaseEntity{Handle: 2, TypeName: "HATCH"}, SolidFill: true, Paths: []entity.HatchPath{{Points: []entity.Point2{{X: 0, Y: 0}, {X: 2, Y: 1}, {X: 2, Y: -1}}}}}
	dim := &entity.EntDimension{BaseEntity: entity.BaseEntity{Handle: 1, TypeName: "DIM_LINEAR"}, AnonymousBlock: 100}
	ins := &entity.EntInsert{BaseEntity: entity.BaseEntity{Handle: 3, TypeName: "INSERT"}, BlockHeader: 100, Scale: entity.Point3{X: 1, Y: 1, Z: 1}, Position: entity.Point3{X: 10}}
	doc := &drawing.Document{ModelSpace: []any{dim, ins}, Blocks: map[uint64][]any{100: {h}}, ByHandle: map[uint64]any{1: dim, 2: h, 3: ins}}
	p, err := PrepareDrawing(doc, RenderOptions{FillMode: "outline"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Primitives) != 2 || p.Primitives[0].Kind != 2 || p.Primitives[1].Kind != 0 {
		t.Fatal("dimension arrow must stay filled while ordinary solid hatch uses contours")
	}
}
