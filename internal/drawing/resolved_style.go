package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"strings"
)

// resolveStyle 结合实体、图层和父块继承关系解析颜色、线宽、线型及可见性。
// 返回解析后的样式、颜色和有效图层；虚线长度应用实体及布局线型比例。
func (ts *Tessellator) resolveStyle(b *entity.BaseEntity) (ResolvedStyle, entity.EntColor, uint64) {
	layer := b.Layer
	lc := ts.Doc.LayerColors[layer]
	if lc.Name == "0" && ts.parentLayer != 0 {
		layer = ts.parentLayer
		lc = ts.Doc.LayerColors[layer]
	}
	col := b.Color
	if !col.HasTrue {
		if col.HasIndex && col.Index == 0 {
			col = ts.parentColor
		}
		if !col.HasTrue && (!col.HasIndex || col.Index == 256 || col.Index == 0) {
			col = entity.EntColor{Index: lc.Index, HasIndex: true, TrueColor: lc.TrueColor, HasTrue: lc.HasTrue}
			if col.Index == 0 {
				col.Index = 7
			}
		}
	}
	style := ResolvedStyle{Hidden: lc.Off || lc.Frozen, NoPlot: lc.NoPlot || strings.EqualFold(lc.Name, "Defpoints") || ts.parentStyle.NoPlot, LineMM: .25}
	lw := uint16(29)
	lt := lc.Ltype
	scale := 1.0
	inheritedDash := false
	if h := b.Head; h != nil {
		// DWG stores a bitfield here: only bit 0 means invisible. Bit 1 is
		// present on visible ATTRIB records and must not suppress their values.
		style.Hidden = style.Hidden || h.Invisible&1 != 0
		lw = h.Linewt
		scale = h.LtypeScale
		switch h.LtypeFlags {
		case 1:
			style.Dash = append([]float64(nil), ts.parentStyle.Dash...)
			inheritedDash = true
			lt = 0
		case 2:
			lt = 0
		case 3:
			lt = h.LtypeHandle
		}
	}
	if lw == 29 {
		lw = lc.Lineweight
		if !lc.StyleKnown {
			lw = 31
		}
	}
	if lw == 30 {
		style.LineMM = ts.parentStyle.LineMM
		if style.LineMM < 0 {
			style.LineMM = .25
		}
	} else {
		style.LineMM = LineweightMM(lw)
	}
	if lt != 0 {
		if g := ts.Doc.InternalObjs[lt]; g != nil {
			if ds, ok := g.Field("dash_lengths").([]float64); ok {
				style.Dash = append([]float64(nil), ds...)
			}
		}
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1
	}
	global := ts.Doc.Ltscale
	if global <= 0 {
		global = 1
	}
	if !inheritedDash && ts.ViewportScale > 0 {
		if v, ok := ts.Doc.HeaderVars["PSLTSCALE"].(uint8); !ok || v == 0 {
			global *= ts.ViewportScale
		}
	}
	if inheritedDash {
		global = 1
	}
	for i := range style.Dash {
		style.Dash[i] *= scale * global
	}
	return style, col, layer
}

// LineweightMM 将 DWG 线宽索引转换为毫米值；代码 28 表示零宽，其他未知代码使用 0.25 mm。
func LineweightMM(code uint16) float64 {
	values := []float64{0, .05, .09, .13, .15, .18, .20, .25, .30, .35, .40, .50, .53, .60, .70, .80, .90, 1, 1.06, 1.20, 1.40, 1.58, 2, 2.11}
	if int(code) < len(values) {
		return values[code]
	}
	if code == 28 {
		return 0
	}
	return .25
}

// textStyle 把实体与 STYLE 记录的字体、宽度、倾斜和对齐约束写入图元文字布局。
// p.Lb.Tx 必须已初始化；TEXT/ATTRIB 的实体宽度优先，MTEXT 可继承 STYLE 宽度。
func (ts *Tessellator) textStyle(e any, p *Primitive) {
	var handle uint64
	var raw string
	switch x := e.(type) {
	case *entity.EntText:
		handle = x.StyleHandle
		raw = x.Text
	case *entity.EntMText:
		handle = x.StyleHandle
		raw = x.Text
	case *entity.EntAttrib:
		handle = x.StyleHandle
		raw = x.Text
	}
	tx := p.Lb.Tx
	if m, ok := e.(*entity.EntMText); ok {
		v, _ := m.Extra["linespace_style"].(int64)
		tx.ExactLineSpacing = v == 2
	}
	var insertion entity.Point3
	var align *entity.Point2
	var width float64
	switch x := e.(type) {
	case *entity.EntText:
		insertion = x.Insertion
		align = x.AlignPt
		width = x.WidthFactor
	case *entity.EntAttrib:
		insertion = x.Insertion
		align = x.AlignPt
		width = x.WidthFactor
	}
	if width > 0 {
		tx.WidthFactor = width
	}
	if (tx.HAlign == 3 || tx.HAlign == 5) && align != nil {
		dx, dy := align.X-insertion.X, align.Y-insertion.Y
		length := math.Hypot(dx, dy)
		if length > 0 {
			t := MatrixXform(p.Source.Matrix)
			o := t.Apply(entity.Point2{X: insertion.X, Y: insertion.Y})
			u := t.Apply(entity.Point2{X: insertion.X + dx/length, Y: insertion.Y + dy/length})
			v := t.Apply(entity.Point2{X: insertion.X - dy/length, Y: insertion.Y + dx/length})
			p.Lb.X, p.Lb.Y = o.X, o.Y
			tx.Ux, tx.Uy = u.X-o.X, u.Y-o.Y
			tx.Vx, tx.Vy = v.X-o.X, v.Y-o.Y
			tx.FitWidth = length
			tx.FitScaleHeight = tx.HAlign == 3
		}
	}
	tx.StyleHandle = handle
	tx.Raw = raw
	if g := ts.Doc.InternalObjs[handle]; g != nil {
		// TEXT/ATTRIB store their effective width on the entity. MTEXT uses
		// the STYLE base width in addition to per-run formatting.
		if _, ok := e.(*entity.EntMText); ok {
			if v, ok := g.Field("width_factor").(float64); ok && v > 0 && !math.IsInf(v, 0) {
				tx.WidthFactor = v
			}
		}
		tx.FontName, _ = g.Field("font_file").(string)
		if tx.FontName == "" {
			var app uint64
			for i := 0; i < 256; i++ {
				prefix := fmt.Sprintf("eed[%d].", i)
				code, ok := g.Field(prefix + "code").(int64)
				if !ok {
					break
				}
				if h, ok := g.Field(prefix + "handle").([]uint64); ok && len(h) > 2 {
					app = h[2]
				}
				a := ts.Doc.InternalObjs[app]
				if code == 0 && a != nil && strings.EqualFold(fmt.Sprint(a.Field("name")), "ACAD") && g.Field(fmt.Sprintf("eed[%d].code", i+1)) == int64(71) {
					if name, ok := g.Field(prefix + "value").(string); ok && name != "" {
						tx.FontName = name
						break
					}
				}
			}
		}

		tx.BigFont, _ = g.Field("bigfont_file").(string)
		if v, ok := g.Field("oblique_angle").(float64); ok {
			tx.Oblique = v
		}
	}
	if c, ok := e.(entity.EntityCommon); ok {
		if v, ok := c.Common().Extra["oblique_angle"].(float64); ok {
			tx.Oblique = v
		}
	}
}
