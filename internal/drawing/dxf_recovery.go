package drawing

import "github.com/boyunsz/go-cad/internal/entity"

// buildRasterImage 从 DXF 组码构造 IMAGE 实体，恢复空间基向量、资源句柄和裁切顶点。
// 沿用传入的公共实体信息；缺失组码按对应零值处理，不在此加载图片像素。
func (st *DxfState) buildRasterImage(r *DxfRec, b *entity.BaseEntity) any {
	im := &entity.EntImage{BaseEntity: *b, Pt0: r.point3(10), Uvec: r.point3(11), Vvec: r.point3(12)}
	im.ImageSize.X, _ = r.floatVal(13)
	im.ImageSize.Y, _ = r.floatVal(23)
	im.ImageDef, _ = r.hexHandle(340)
	im.ImageDefReactor, _ = r.hexHandle(360)
	v, _ := r.intVal(90)
	im.ClassVersion = uint32(v)
	v, _ = r.intVal(70)
	im.DisplayProps = uint16(v)
	v, _ = r.intVal(280)
	im.Clipping = v != 0
	v, _ = r.intVal(281)
	im.Brightness = uint8(v)
	v, _ = r.intVal(282)
	im.Contrast = uint8(v)
	v, _ = r.intVal(283)
	im.Fade = uint8(v)
	v, _ = r.intVal(71)
	im.ClipBoundaryType = uint16(v)
	v, _ = r.intVal(290)
	im.ClipMode = uint8(v)
	xs, ys := r.all(14), r.all(24)
	for i, p := range xs {
		if i < len(ys) {
			im.ClipVerts = append(im.ClipVerts, entity.Point2{X: p.floatValue(), Y: ys[i].floatValue()})
		}
	}
	return im
}
