// 本文件收集模型空间中的文字，并关联已识别的图框。
//
// 输出包含文字内容、世界坐标、字高、旋转角、图层名和图框序号。
// 块内文字通过展开流程收集，图框复用 DetectSheets 的结果；归属依据
// 文字插入点是否位于图框范围内，图框序号从 1 开始，0 表示框外。

package render

import (
	"math"
	"strings"

	"github.com/boyunsz/go-cad/internal/drawing"
)

// SheetText 单条图纸文本及其图框归属（docling 集成的文本导出单元）。
type SheetText struct {
	Text     string  // 文本内容（MTEXT 已剥离格式码，多行以 \n 连接）
	X, Y     float64 // 插入点世界坐标（块内文本已经 INSERT 变换到世界系）
	Height   float64 // 世界字高（含块引用缩放，与渲染清晰度自检采用相同规则）
	Rotation float64 // 基线旋转角（度，0~360；含块引用旋转与镜像后的实际方向）
	Layer    string  // 图层名（层表解析失败或未知名时为空串，不阻塞导出）
	Sheet    int     // 所属图框序号：1-based 对应 DetectSheets 输出顺序；0=不在任何图框内
}

// SheetTexts 展开模型空间及块引用，收集带实际文本的文字图元。
// 图框由调用方提供，按文字插入点确定归属；空白文字和无文本占位图元
// 不进入结果。循环引用和展开预算限制可能使部分块内文字无法收集。
// 顶层 cad.SheetTexts 会排除自动生成的“整图”回退范围。
func SheetTexts(doc *drawing.Document, sheets []Sheet) []SheetText {
	if doc == nil {
		return nil
	}
	ts := drawing.NewTessellator(doc)
	prims := ts.ExpandAll()
	out := make([]SheetText, 0, len(prims)/4+1)
	for i := range prims {
		p := &prims[i]
		if p.Kind != 1 || p.Lb.Tx == nil {
			continue
		}
		text := strings.TrimSpace(p.Lb.Text)
		if text == "" || !drawing.Plausible(p.Lb.X, p.Lb.Y) {
			continue
		}
		// 世界字高与 sheetWidthFor/sheetTitle 采用相同规则：推进基向量模长
		// （含块变换比例）× 局部字高
		h := math.Hypot(p.Lb.Tx.Ux, p.Lb.Tx.Uy) * p.Lb.Tx.HWorld
		out = append(out, SheetText{
			Text:     text,
			X:        p.Lb.X,
			Y:        p.Lb.Y,
			Height:   h,
			Rotation: sheetNormalizedDeg(p.Lb.Rot),
			Layer:    layerName(doc, p.Layer),
			Sheet:    sheetIndexOf(sheets, p.Lb.X, p.Lb.Y),
		})
	}
	return out
}

// layerName 图层句柄 → 层表真名（LayerColor.Name 由各版本 LAYER 记录解析
// 填充；句柄不在层表或层名未解析出时返回空串——导出不因层名缺失阻塞）。
func layerName(doc *drawing.Document, h uint64) string {
	return doc.LayerColors[h].DisplayName()
}

// sheetIndexOf 插入点落框归属：返回首个包含该点的图框序号（1-based，
// sheets 顺序即 DetectSheets 输出顺序；多框重叠时取序号最小者），框外
// 返回 0。含边界（与图框内容密度判定的闭区间规则一致）。
func sheetIndexOf(sheets []Sheet, x, y float64) int {
	for i := range sheets {
		if pointInBox(x, y, drawing.Box2{MinX: sheets[i].Box[0], MinY: sheets[i].Box[1], MaxX: sheets[i].Box[2], MaxY: sheets[i].Box[3]}) {
			return i + 1
		}
	}
	return 0
}

// sheetNormalizedDeg 弧度 → 规范化到 [0,360) 的角度（负角与多圈归一，
// 导出规则稳定便于下游比对）。
func sheetNormalizedDeg(rad float64) float64 {
	if math.IsNaN(rad) || math.IsInf(rad, 0) {
		// 错位解码可能产生 NaN/Inf 弧度，归一前剔除
		return 0
	}
	deg := math.Mod(rad*180/math.Pi, 360)
	if deg < 0 {
		deg += 360
	}
	return deg
}
