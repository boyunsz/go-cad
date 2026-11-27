package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"sort"
	"strings"
)

type LayoutInfo struct {
	Handle         uint64 `json:"handle"`
	Name           string `json:"name"`
	RawName        string `json:"raw_name,omitempty"`
	Block          uint64 `json:"block"`
	ActiveViewport uint64 `json:"active_viewport"`
	TabOrder       int64  `json:"tab_order"`
	Entities       int    `json:"entities"`
	Model          bool   `json:"model"`
}

// currentPaperBlock 从 BLOCK_CONTROL 的句柄尾项读取当前纸张空间块；未找到时返回 0。
func (d *Document) currentPaperBlock() uint64 {
	for _, g := range d.InternalObjs {
		if g.Name == "BLOCK_CONTROL" && len(g.Handles) >= 2 {
			return g.Handles[len(g.Handles)-1]
		}
	}
	return 0
}

// Layouts 枚举有效的 LAYOUT 记录，解析布局名称、所属块和活动视口。
// 返回值按页签顺序及句柄排序，名称解码发生变化时保留 RawName。
func (d *Document) Layouts() []LayoutInfo {
	var out []LayoutInfo
	for h, g := range d.InternalObjs {
		if g.Name != "LAYOUT" || len(g.Handles) < 2 {
			continue
		}
		name, _ := g.Field("layout_name").(string)
		order, _ := g.Field("tab_order").(int64)
		block := g.Handles[0]
		active := g.Handles[1]
		// Named references avoid version-dependent PLOTSETTINGS handle prefixes.
		if h, ok := g.Field("block_header").(uint64); ok {
			block = h
		}
		if h, ok := g.Field("active_viewport").(uint64); ok {
			active = h
		}
		model := false
		if b := d.InternalObjs[block]; b != nil {
			n, _ := b.Field("name").(string)
			model = isModelSpaceBlockName(n)
		}
		display, raw := expandPercentCodes(expandSymbolNameEscapes(name)), ""
		if display != name {
			raw = name
		}
		out = append(out, LayoutInfo{Handle: h, Name: display, RawName: raw, Block: block, ActiveViewport: active, TabOrder: order, Entities: len(d.LayoutEntities(block)), Model: model})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TabOrder != out[j].TabOrder {
			return out[i].TabOrder < out[j].TabOrder
		}
		return out[i].Handle < out[j].Handle
	})
	return out
}

// LayoutEntities 依据块归属与绘制顺序收集布局实体，并按句柄去重补充记录。
// R2000 的 mode=1 实体通过当前纸张块关联；模型块转交 SelectedModelSpace。
func (d *Document) LayoutEntities(block uint64) []any {
	if b := d.InternalObjs[block]; b != nil {
		name, _ := b.Field("name").(string)
		if isModelSpaceBlockName(name) {
			return d.SelectedModelSpace()
		}
	}
	list := append([]any(nil), d.Blocks[block]...)
	seen := map[uint64]bool{}
	for _, e := range list {
		if c, ok := e.(entity.EntityCommon); ok {
			seen[c.Common().Handle] = true
		}
	}
	current := d.currentPaperBlock()
	var extra []uint64
	for h, e := range d.ByHandle {
		if seen[h] {
			continue
		}
		if c, ok := e.(entity.EntityCommon); ok {
			b := c.Common()
			if (b.Owner == block && block != 0) || (b.Mode == 1 && block == current && current != 0) {
				extra = append(extra, h)
			}
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i] < extra[j] })
	for _, h := range extra {
		list = append(list, d.ByHandle[h])
	}
	return d.OrderedEntities(list)
}

// viewportTransform 构造从世界坐标经视图坐标到纸张空间的正交投影变换。
// 尺寸无效、透视、前后裁切、非矩形裁切或句柄解析异常时返回错误。
func viewportTransform(v *entity.EntViewport) (Xform, error) {
	if v.Width <= 0 || v.Height <= 0 || v.ViewSize <= 0 {
		return Xform{}, fmt.Errorf("invalid viewport dimensions")
	}
	if v.StatusFlag&1 != 0 {
		return Xform{}, fmt.Errorf("perspective viewport requires a 3D renderer")
	}
	if v.StatusFlag&6 != 0 {
		return Xform{}, fmt.Errorf("front/back viewport clipping is not supported")
	}
	if v.StatusFlag&0x10000 != 0 {
		return Xform{}, fmt.Errorf("nonrectangular viewport clip %X is not supported", v.ClipBoundary)
	}
	if v.HandleError != "" {
		return Xform{}, fmt.Errorf("viewport handle stream: %s", v.HandleError)
	}
	// World -> DCS: transpose arbitrary-axis basis, then negative view twist.
	f := ocsFrame(v.ViewDir).SpatialMatrix()
	rot := SpatialXform([12]float64{f[0], f[4], f[8], 0, f[1], f[5], f[9], 0, f[2], f[6], f[10], 0})
	rot = MatrixXform([6]float64{math.Cos(v.ViewTwist), -math.Sin(v.ViewTwist), math.Sin(v.ViewTwist), math.Cos(v.ViewTwist), 0, 0}).Compose(rot)
	scale := v.Height / v.ViewSize
	paper := MatrixXform([6]float64{scale, 0, 0, scale, v.Center.X - scale*v.ViewCtr.X, v.Center.Y - scale*v.ViewCtr.Y})
	return paper.Compose(rot).Compose(translation3(-v.ViewTarget.X, -v.ViewTarget.Y, -v.ViewTarget.Z)), nil
}

// BuildLayoutScene 展开指定布局及其模型视口，保留屏幕显示规则下的可见图元。
// tolerance 使用图纸单位；找不到布局时返回错误，视口限制记录在场景诊断中。
func BuildLayoutScene(d *Document, name string, tolerance float64) (Scene, error) {
	return buildLayoutScene(d, name, tolerance, false)
}

// BuildPlotLayoutScene 按打印可见性展开指定布局及其模型视口。
// tolerance 使用图纸单位；找不到布局时返回错误，视口限制记录在场景诊断中。
func BuildPlotLayoutScene(d *Document, name string, tolerance float64) (Scene, error) {
	return buildLayoutScene(d, name, tolerance, true)
}

// buildLayoutScene 按布局名选择纸张块，展开模型视口后追加纸张空间实体。
// plot 控制打印图层过滤；关闭或不支持的视口记录诊断，矩形视口附带裁切范围。
func buildLayoutScene(d *Document, name string, tolerance float64, plot bool) (Scene, error) {
	var chosen *LayoutInfo
	for _, v := range d.Layouts() {
		if v.Name == name || v.RawName != "" && v.RawName == name {
			q := v
			chosen = &q
			break
		}
	}
	if chosen == nil {
		return Scene{}, fmt.Errorf("layout %q not found", name)
	}
	if chosen.Model || strings.EqualFold(name, "model") {
		return buildScene(d, tolerance, plot), nil
	}
	list := d.LayoutEntities(chosen.Block)
	ts := NewTessellator(d)
	ts.Faithful = true
	ts.Plot = plot
	ts.Tolerance = tolerance
	ts.Space = "paper:" + name
	ts.BuildVertexIndex()
	var out []Primitive
	// The first viewport in a paper block is the sheet's own editor viewport (ID 1).
	var primary uint64
	if g := d.InternalObjs[chosen.Block]; g != nil && len(g.Handles) > 2 {
		h := g.Handles[2]
		if _, ok := d.ByHandle[h].(*entity.EntViewport); ok {
			primary = h
		}
	}
	if primary == 0 {
		for _, e := range list {
			if v, ok := e.(*entity.EntViewport); ok {
				primary = v.Handle
				break
			}
		}
	}
	for _, e := range list {
		v, ok := e.(*entity.EntViewport)
		if !ok {
			continue
		}
		ref := SourceRef{Handle: v.Handle, Type: "VIEWPORT", Space: "paper:" + name}
		if v.Handle == primary {
			ts.Diagnostics = append(ts.Diagnostics, Diagnostic{ref, "structural", "paper editor viewport (ID 1); not a model view"})
			continue
		}
		if v.StatusFlag&0x20000 != 0 {
			ts.Diagnostics = append(ts.Diagnostics, Diagnostic{ref, "hidden", "viewport is off"})
			continue
		}
		transform, err := viewportTransform(v)
		if err != nil {
			ts.Diagnostics = append(ts.Diagnostics, Diagnostic{ref, "unsupported", err.Error()})
			continue
		}
		ts.ViewportScale = v.Height / v.ViewSize
		ts.FrozenLayers = map[uint64]bool{}
		for _, h := range v.FrozenLayers {
			ts.FrozenLayers[h] = true
		}
		ts.instancePath = []uint64{v.Handle}
		ts.Space = "viewport:" + name
		start := len(out)
		for _, m := range d.SelectedModelSpace() {
			out = ts.AppendEntity(out, m, transform, 0)
		}
		clip := &Box2{MinX: v.Center.X - v.Width/2, MaxX: v.Center.X + v.Width/2, MinY: v.Center.Y - v.Height/2, MaxY: v.Center.Y + v.Height/2}
		for i := start; i < len(out); i++ {
			out[i].Clip = clip
		}
		ts.Diagnostics = append(ts.Diagnostics, Diagnostic{ref, "expanded", fmt.Sprintf("%d model primitives; %d frozen layers; rectangular clip", len(out)-start, len(v.FrozenLayers))})
	}
	ts.FrozenLayers = nil
	ts.ViewportScale = 0
	ts.instancePath = nil
	ts.Space = "paper:" + name
	for _, e := range list {
		if _, ok := e.(*entity.EntViewport); !ok {
			out = ts.AppendEntity(out, e, IdentityXform(), 0)
		}
	}
	return finishScene(d, out, ts.Diagnostics), nil
}
