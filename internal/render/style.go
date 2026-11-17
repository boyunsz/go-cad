package render

import (
	"image/color"
	"math"

	"github.com/boyunsz/go-cad/internal/drawing"
)

// styledEntityColor 解析实体颜色并应用可选的柔和显示配色；不修改源实体。
// 柔和模式区分文字和几何的灰度，以保持标注可读。
func styledEntityColor(doc *drawing.Document, p *drawing.Primitive, bgWhite, soft bool) color.RGBA {
	c := entityColor(doc, p, bgWhite)
	if !soft {
		return c
	}
	if p.Kind == 1 {
		return color.RGBA{77, 83, 90, 255} // Keep annotations readable.
	}
	luminance := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
	gray := uint8(math.Round(70 + 0.25*luminance))
	return color.RGBA{gray, gray + 4, gray + 8, 255}
}

// renderLineScale 规范化显示线宽倍率；非正数、NaN 和无穷值回退为 1。
func renderLineScale(scale float64) float64 {
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 1
	}
	return scale
}
