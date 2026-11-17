package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
)

// appendLeaderArrow 按源 DIMLDRBLK 引用展开实际箭头图块，应用引线方向和箭头尺寸。
// 缺失自定义箭头时记录限制，不根据外观猜测或补造三角形。
func (ts *Tessellator) appendLeaderArrow(out []Primitive, l *entity.EntLeader, t Xform, depth int, ref SourceRef) []Primitive {
	if !l.ArrowheadOn || len(l.Points) < 2 {
		return out
	}
	note := func(reason string) {
		ts.Diagnostics = append(ts.Diagnostics, Diagnostic{Source: ref, Status: "approximated", Detail: reason})
	}
	style := ts.Doc.InternalObjs[l.Dimstyle]
	if style == nil {
		note("LEADER arrow DIMSTYLE unavailable")
		return out
	}
	block, _ := style.Field("DIMLDRBLK").(uint64)
	size, _ := style.Field("DIMASZ").(float64)
	scale, _ := style.Field("DIMSCALE").(float64)
	if block == 0 || len(ts.Doc.Blocks[block]) == 0 {
		note("LEADER arrow block unavailable; builtin/default arrow generation not implemented")
		return out
	}
	if !finitePoint(entity.Point2{X: size, Y: scale}) || size <= 0 || scale <= 0 {
		note("LEADER arrow size or annotation scale unresolved")
		return out
	}
	for _, h := range ts.insPath {
		if h == block {
			note("cyclic LEADER arrow block")
			return out
		}
	}
	a, b := l.Points[0], l.Points[1]
	if math.Hypot(a.X-b.X, a.Y-b.Y) < 1e-12 {
		note("LEADER first segment has no direction")
		return out
	}
	if l.PathType != 0 {
		note("spline LEADER arrow uses first segment tangent approximation")
	}
	ins := &entity.EntInsert{BlockHeader: block, Position: a, Scale: entity.Point3{X: size * scale, Y: size * scale, Z: size * scale}, Rotation: math.Atan2(a.Y-b.Y, a.X-b.X)}
	transform := ts.blockTransform(ins, t)
	ts.insPath = append(ts.insPath, block)
	ts.instancePath = append(ts.instancePath, l.Handle)
	defer func() {
		ts.insPath = ts.insPath[:len(ts.insPath)-1]
		ts.instancePath = ts.instancePath[:len(ts.instancePath)-1]
	}()
	for _, e := range ts.Doc.OrderedEntities(ts.Doc.Blocks[block]) {
		out = ts.AppendEntity(out, e, transform, depth+1)
	}
	return out
}
