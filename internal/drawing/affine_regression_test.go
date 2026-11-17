package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"testing"
)

// TestNestedNonUniformTransform 验证嵌套非均匀缩放与仿射组合产生预期的坐标。
func TestNestedNonUniformTransform(t *testing.T) {
	parent := Xform{Sx: 2, Sy: .5, Cos: math.Cos(.3), sin: math.Sin(.3), Tx: 10, Ty: -4}
	child := Xform{Sx: -1, Sy: 3, Cos: math.Cos(.7), sin: math.Sin(.7), Tx: 3, Ty: 7}
	for _, p := range []entity.Point2{{0, 0}, {1, 0}, {0, 1}, {7, -3}} {
		got, want := parent.Compose(child).Apply(p), parent.Apply(child.Apply(p))
		if math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-10 {
			t.Fatalf("nested transform: got %+v want %+v", got, want)
		}
	}
}
