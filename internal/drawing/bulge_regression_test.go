package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"testing"
)

// nearPoint 断言两个二维点在规定误差内一致，并把失败定位到测试调用处。
func nearPoint(t *testing.T, got, want entity.Point2) {
	t.Helper()
	if math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-7 {
		t.Fatalf("point %+v; want %+v", got, want)
	}
}

// TestBulgeDirectionAndMajorArc 验证正负 bulge 的弧方向、边界以及大于半圆的扫角。
func TestBulgeDirectionAndMajorArc(t *testing.T) {
	for _, b := range []float64{0.01, -0.01, 0.1, -0.1, 1, -1, 2, -2} {
		t.Run(fmt.Sprint(b), func(t *testing.T) {
			e := &entity.EntLwPolyline{Vertices: []entity.Point2{{0, 0}, {10, 0}}, Bulges: []float64{b}}
			strokes := TessLwPolyline(e, IdentityXform())
			if len(strokes) == 0 {
				t.Fatal("missing arc")
			}
			nearPoint(t, entity.Point2{strokes[0].X1, strokes[0].Y1}, e.Vertices[0])
			last := strokes[len(strokes)-1]
			nearPoint(t, entity.Point2{last.X2, last.Y2}, e.Vertices[1])
			low, high := 0.0, 0.0
			for _, s := range strokes {
				low = math.Min(low, math.Min(s.Y1, s.Y2))
				high = math.Max(high, math.Max(s.Y1, s.Y2))
			}
			// Signed sagitta: positive bulges lie below this left-to-right chord.
			if b > 0 && (high > 1e-7 || math.Abs(low+5*b) > 0.02) {
				t.Fatalf("bulge %g: y=[%g,%g], want [%g,0]", b, low, high, -5*b)
			}
			if b < 0 && (low < -1e-7 || math.Abs(high+5*b) > 0.02) {
				t.Fatalf("bulge %g: y=[%g,%g], want [0,%g]", b, low, high, -5*b)
			}
		})
	}
}

// TestBulgeTransformAppliedOnce 验证 bulge 圆弧只应用一次实例变换，避免重复缩放或平移。
func TestBulgeTransformAppliedOnce(t *testing.T) {
	for _, b := range []float64{0.1, -0.1, 2, -2} {
		e := &entity.EntLwPolyline{Vertices: []entity.Point2{{2, 3}, {12, 3}}, Bulges: []float64{b}}
		local := TessLwPolyline(e, IdentityXform())
		transforms := []Xform{
			{Sx: 1, Sy: 1, Cos: 1, Tx: 100, Ty: -40},
			{Sx: 2, Sy: 0.5, Cos: math.Cos(0.7), sin: math.Sin(0.7), Tx: 12, Ty: 21},
			{Sx: -2, Sy: 3, Cos: math.Cos(0.3), sin: math.Sin(0.3), Tx: -20, Ty: 17},
		}
		for _, tr := range transforms {
			got := TessLwPolyline(e, tr)
			if len(got) != len(local) {
				t.Fatal("transform changed segment count")
			}
			for i, s := range local {
				nearPoint(t, entity.Point2{got[i].X1, got[i].Y1}, tr.Apply(entity.Point2{s.X1, s.Y1}))
				nearPoint(t, entity.Point2{got[i].X2, got[i].Y2}, tr.Apply(entity.Point2{s.X2, s.Y2}))
			}
		}
	}
}

// TestHatchBulgeFollowsSameGeometry 验证 HATCH 的 bulge 边界与普通多段线使用一致的弧段几何。
func TestHatchBulgeFollowsSameGeometry(t *testing.T) {
	for _, b := range []float64{0.1, -0.1, 1, -1, 2, -2} {
		pts := entity.PolylineWithBulges([]entity.Point2{{0, 0}, {10, 0}}, []float64{b}, false, 64)
		if len(pts) == 0 {
			t.Fatal("missing hatch path")
		}
		nearPoint(t, pts[0], entity.Point2{0, 0})
		nearPoint(t, pts[len(pts)-1], entity.Point2{10, 0})
		for _, p := range pts {
			if b*p.Y > 1e-7 || math.Abs(p.Y) > 5*math.Abs(b)+1e-7 {
				t.Fatalf("hatch bulge %g escaped arc: %+v", b, p)
			}
		}
	}
}
