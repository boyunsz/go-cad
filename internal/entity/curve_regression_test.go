package entity

import (
	"math"
	"testing"
)

// TestDeBoorBezierEndpointsAndInterior 验证有理 de Boor 求值在端点与内部参数处符合预期 Bézier 结果。
func TestDeBoorBezierEndpointsAndInterior(t *testing.T) {
	ctrl := []Point3{{0, 0, 0}, {0, 10, 0}, {10, 10, 0}}
	knots := []float64{0, 0, 0, 1, 1, 1}
	for _, weights := range [][]float64{nil, {1, 2, 1}} {
		for _, u := range []float64{0, 0.25, 0.5, 0.75, 1} {
			w := []float64{1, 1, 1}
			if weights != nil {
				w = weights
			}
			a, b, c := (1-u)*(1-u)*w[0], 2*(1-u)*u*w[1], u*u*w[2]
			want := Point3{10 * c / (a + b + c), 10 * (b + c) / (a + b + c), 0}
			got := DeBoor(ctrl, weights, knots, 2, 2, u)
			if math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-9 {
				t.Fatalf("u=%g, weights=%v: got %+v, want %+v", u, weights, got, want)
			}
		}
	}
}

// TestHatchShortCounterclockwiseArc 验证短逆时针 HATCH 圆弧不会退化成整圆。
func TestHatchShortCounterclockwiseArc(t *testing.T) {
	pts := appendArcPoints(nil, Point2{}, 100, 100, 0, 0.01, true, 64)
	for _, p := range pts {
		if p.X < 99.99 || p.Y < -1e-9 || p.Y > 1.001 {
			t.Fatalf("short hatch arc became full circle: %+v", p)
		}
	}
}

// TestHatchClockwiseAnglesAndFullCircle 验证顺时针 HATCH 圆弧的端点、象限和整圆处理。
func TestHatchClockwiseAnglesAndFullCircle(t *testing.T) {
	pts := appendArcPoints(nil, Point2{}, 10, 10, math.Pi/2, math.Pi, false, 64)
	first, last := pts[0], pts[len(pts)-1]
	if math.Abs(first.X) > 1e-9 || math.Abs(first.Y+10) > 1e-9 || math.Abs(last.X+10) > 1e-9 || math.Abs(last.Y) > 1e-9 {
		t.Fatalf("clockwise endpoints: %+v -> %+v", first, last)
	}
	for _, p := range pts {
		if p.X > 1e-9 || p.Y > 1e-9 {
			t.Fatalf("wrong clockwise quadrant: %+v", p)
		}
	}
	for _, ccw := range []bool{true, false} {
		pts = appendArcPoints(nil, Point2{}, 10, 10, 0, 2*math.Pi, ccw, 64)
		if math.Abs(pts[32].X+10) > 1e-9 {
			t.Fatal("full circle collapsed")
		}
	}
}
