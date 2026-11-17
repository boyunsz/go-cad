package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
)

// SpatialXform 从按行排列的 3×4 仿射矩阵构造空间变换。
// 组合过程中保留 Z 分量，仅在 Project 时投影到二维。
func SpatialXform(m [12]float64) Xform { return Xform{spatial: &m} }

// SpatialMatrix 返回按行排列的 3×4 矩阵；二维变换提升为保持 Z 不变的空间变换。
func (t Xform) SpatialMatrix() [12]float64 {
	if t.spatial != nil {
		return *t.spatial
	}
	m := t.Matrix()
	return [12]float64{m[0], m[2], 0, m[4], m[1], m[3], 0, m[5], 0, 0, 1, 0}
}

// Apply3 对三维点应用包含平移的空间仿射变换。
func (t Xform) Apply3(p entity.Point3) entity.Point3 {
	m := t.SpatialMatrix()
	return entity.Point3{X: m[0]*p.X + m[1]*p.Y + m[2]*p.Z + m[3], Y: m[4]*p.X + m[5]*p.Y + m[6]*p.Z + m[7], Z: m[8]*p.X + m[9]*p.Y + m[10]*p.Z + m[11]}
}

// Project 先完成三维变换，再返回结果的 XY 分量。
func (t Xform) Project(p entity.Point3) entity.Point2 {
	q := t.Apply3(p)
	return entity.Point2{X: q.X, Y: q.Y}
}

// composeSpatial 组合两个空间变换，所得结果先应用 b，再应用 a。
func composeSpatial(a, b Xform) Xform {
	x, y := a.SpatialMatrix(), b.SpatialMatrix()
	var m [12]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				m[i*4+j] += x[i*4+k] * y[k*4+j]
			}
		}
		m[i*4+3] = x[i*4+3]
		for k := 0; k < 3; k++ {
			m[i*4+3] += x[i*4+k] * y[k*4+3]
		}
	}
	return SpatialXform(m)
}

// translation3 构造沿 X、Y、Z 轴平移的空间变换。
func translation3(x, y, z float64) Xform {
	return SpatialXform([12]float64{1, 0, 0, x, 0, 1, 0, y, 0, 0, 1, z})
}

// ocsFrame 按任意轴算法构造从对象坐标系到世界坐标系的正交基。
// 法向量会归一化；零向量按世界 Z 轴处理。
func ocsFrame(n entity.Point3) Xform {
	l := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
	if l == 0 {
		n = entity.Point3{Z: 1}
	} else {
		n.X /= l
		n.Y /= l
		n.Z /= l
	}
	a := entity.Point3{X: -n.Y, Y: n.X}
	if math.Abs(n.X) < 1.0/64 && math.Abs(n.Y) < 1.0/64 {
		a = entity.Point3{X: n.Z, Z: -n.X}
	}
	l = math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z)
	a.X /= l
	a.Y /= l
	a.Z /= l
	b := entity.Point3{X: n.Y*a.Z - n.Z*a.Y, Y: n.Z*a.X - n.X*a.Z, Z: n.X*a.Y - n.Y*a.X}
	return SpatialXform([12]float64{a.X, b.X, n.X, 0, a.Y, b.Y, n.Y, 0, a.Z, b.Z, n.Z, 0})
}

// insertSpatial 构造 INSERT 的空间变换，组合缩放、旋转、插入位置和挤出方向。
// 旋转值按弧度解释；块基点偏移由调用方另外处理。
func insertSpatial(e *entity.EntInsert) Xform {
	c, s := math.Cos(e.Rotation), math.Sin(e.Rotation)
	return ocsFrame(e.Extrusion).Compose(SpatialXform([12]float64{c * e.Scale.X, -s * e.Scale.Y, 0, e.Position.X, s * e.Scale.X, c * e.Scale.Y, 0, e.Position.Y, 0, 0, e.Scale.Z, e.Position.Z}))
}

// spatialSupported 判断实体类型是否接入当前的空间变换处理路径。
func spatialSupported(e any) bool {
	switch e.(type) {
	case *entity.EntInsert, *entity.EntCircle, *entity.EntArc, *entity.EntEllipse, *entity.EntText, *entity.EntAttrib, *entity.EntLwPolyline, *entity.EntPolyline2d, *entity.EntSolid:
		return true
	}
	return false
}

// entityPlane 将支持实体的对象平面及标高组合到父变换 t。
// 不使用该对象平面处理方式的实体直接返回父变换。
func entityPlane(e any, t Xform) Xform {
	var n entity.Point3
	z := 0.0
	switch e := e.(type) {
	case *entity.EntCircle:
		n = extraNormal(e.Common())
		z = e.Center.Z
	case *entity.EntArc:
		n = extraNormal(e.Common())
		z = e.Center.Z
	case *entity.EntText:
		n = e.Extrusion
		z = e.Insertion.Z
	case *entity.EntAttrib:
		n = e.Extrusion
		z = e.Insertion.Z
	case *entity.EntLwPolyline:
		n = e.Extrusion
		z = e.Elevation
	case *entity.EntPolyline2d:
		n = e.Extrusion
		z = e.Elevation
	case *entity.EntSolid:
		n = e.Extrusion
		z = e.Elevation
	default:
		return t
	}
	return t.Compose(ocsFrame(n)).Compose(translation3(0, 0, z))
}

// extraNormal 从实体扩展字段读取三分量挤出方向；字段缺失或类型不符时返回正 Z 方向。
func extraNormal(b *entity.BaseEntity) entity.Point3 {
	if a, ok := b.Extra["extrusion"].([]float64); ok && len(a) == 3 {
		return entity.Point3{X: a[0], Y: a[1], Z: a[2]}
	}
	return entity.Point3{Z: 1}
}
