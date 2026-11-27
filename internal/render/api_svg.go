// 本文件提供 SVG 输出入口和预览模式的 SVG 编码。
//
// 预览模式将图元编码为路径和可编辑文字，并附带源元数据。
// Fidelity 为 faithful 时委托 PreparedDrawing 输出，与 PNG 共用几何、
// 文字布局、裁剪和可见性处理。

package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
)

// svgExpandBudget bounds recursive expansion in the preview SVG renderer.
// A larger budget accommodates repeated detail blocks without pixel buffers.
const svgExpandBudget = 2000000

// RenderSVG 根据 Fidelity 选择预览或 faithful 渲染路径，并返回 SVG 字节。
// 预览路径处理模型空间；faithful 路径支持布局选择。Width 控制输出的
// 固有宽度，视口和高度由相应渲染管线计算。
func RenderSVG(doc *drawing.Document, opts RenderOptions) ([]byte, error) {
	if opts.Fidelity == "faithful" {
		p, e := PrepareDrawing(doc, opts)
		if e != nil {
			return nil, e
		}
		return p.SVG()
	}
	// 空文档按导出接口约定返回错误。
	if doc == nil {
		return nil, fmt.Errorf("cad: 文档为空，无法渲染")
	}
	if opts.Width <= 0 {
		opts.Width = 2048
	}
	if opts.Background == (color.RGBA{}) {
		opts.Background = color.RGBA{255, 255, 255, 255}
	}

	// Preview output uses discrete primitives and heuristic bounds. Faithful
	// output returns above, before these model-space preview filters.
	prim := drawing.NewTessellator(doc)
	prim.Budget = svgExpandBudget
	prims := prim.ExpandAll()
	prims = filterRadiatingStrokes(prims)
	// 使用与预览 PNG 相同的分位范围和主要坐标簇估计视口。
	bbox := quantileSheetBounds(prims)
	if bbox.Invalid() {
		bbox = drawing.Box2{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}
	}
	prims = dropOriginAnchored(prims, bbox)

	marginX := (bbox.MaxX - bbox.MinX) * 0.05
	marginY := (bbox.MaxY - bbox.MinY) * 0.05
	if marginX == 0 && marginY == 0 {
		marginX, marginY = 1, 1 // 退化（单点/空图）回退
	} else if marginX == 0 {
		marginX = marginY
	} else if marginY == 0 {
		marginY = marginX
	}
	bbox.MinX -= marginX
	bbox.MaxX += marginX
	bbox.MinY -= marginY
	bbox.MaxY += marginY
	vw := bbox.MaxX - bbox.MinX
	vh := bbox.MaxY - bbox.MinY
	width := float64(opts.Width)
	height := int(vh/vw*width + 0.5)
	if height < 1 {
		height = 1
	}
	// 线宽取 PNG 圆盘笔刷直径（1.5px）的世界当量：同宽视觉，缩放跟随矢量
	strokeW := vw / width * 1.5 * renderLineScale(opts.LineScale)

	bgWhite := opts.Background.R > 127
	em := newSVGEmitter(bbox, strokeW, opts.Background, &svgDocMeta{
		Generator: "go-cad",
		Version:   doc.Version(),
		Entities:  len(prims),
	})
	em.softPalette = opts.SoftPalette
	for i := range prims {
		em.emit(doc, &prims[i], bgWhite)
	}
	return em.finish(opts.Width, height, vw, vh), nil
}

// svgMaxGroups 线段分组数上限：真实图纸图层常为几十个，超过该值一般是
// 解码噪声图层（句柄无名/名字碎片化），按「颜色×图层」分组只会碎片化
// path 而无语义增益——触顶后退化为纯颜色分组（SVG 内输出注释说明）。
const svgMaxGroups = 200

// svgStrokeGroup 线段分组组：stroke/fill/stroke-width 声明提升到 <g>，
// 组内连续追加 <path> 片段，避免数十万图元逐个携带属性重复。
type svgStrokeGroup struct {
	hex string // 组颜色 #rrggbb
	id  string // 组 id（"layer:图层名"，退化纯颜色组为空）
	buf []byte // 连续的 <path d="…"/> 片段
}

// rgbKey 颜色分组键（可哈希数组，免每图元格式化十六进制字符串的分配）。
type rgbKey struct{ r, G, b uint8 }

// svgGroupKey 线段分组键：颜色 × 图层 id（同名图层自动合并）。
type svgGroupKey struct {
	rgb   rgbKey
	layer string // 图层 id（layerID 产物：图层名或 handle-<hex>）
}

// svgDocMeta <metadata> 图纸摘要：写给 AI 消费者的图纸语义概览（版本/
// 图元数/文本数/图框名/图层清单）。metadata 是 SVG 规范元素，浏览器不
// 渲染其内容，对人类查看者视觉零影响。
type svgDocMeta struct {
	Generator string   `json:"generator"`        // 生成器标识
	Version   string   `json:"version"`          // 源文件版本（AC1015 等，DXF/JSON 来源同样填充）
	Entities  int      `json:"entities"`         // 展开图元数（线段+文本，含 INSERT 递归展开）
	Texts     int      `json:"texts"`            // 文本图元数（finish 时按实际发射数回填）
	Sheet     string   `json:"sheet,omitempty"`  // 图框名（拆图 RenderSheetSVG 模式下）
	Layers    []string `json:"layers,omitempty"` // 图层清单（finish 时按实际出图图层回填，升序去重）
}

// svgEmitter SVG 序列化器：线段按「颜色×图层」分组收集 path，文本收集为
// <text> 片段（文本置于线段之上输出，避免被线盖住）。
type svgEmitter struct {
	softPalette bool
	bbox        drawing.Box2
	strokeW     float64
	bg          color.RGBA
	groups      []*svgStrokeGroup
	groupIdx    map[svgGroupKey]int // 颜色×图层 → 组序号
	plainIdx    map[rgbKey]int      // 退化后的纯颜色 → 组序号
	degraded    bool                // 分组数触顶，新图元一律并入纯颜色组
	layerIDs    map[uint64]string   // 图层句柄 → 分组 id 片段（缓存，免逐图元格式化）
	layersSeen  map[string]struct{} // 实际出图的图层 id（metadata 图层清单，小图不背全文档清单）
	texts       [][]byte            // 每项一个完整 <text …>…</text> 片段
	meta        *svgDocMeta         // 图纸摘要（nil 不输出 metadata）
}

// newSVGEmitter 创建预览 SVG 写出器，保存视口、线宽、背景和元数据，并初始化图层分组索引。
func newSVGEmitter(bbox drawing.Box2, strokeW float64, bg color.RGBA, meta *svgDocMeta) *svgEmitter {
	return &svgEmitter{
		bbox: bbox, strokeW: strokeW, bg: bg, meta: meta,
		groupIdx:   make(map[svgGroupKey]int, 16),
		plainIdx:   make(map[rgbKey]int, 4),
		layerIDs:   make(map[uint64]string, 32),
		layersSeen: make(map[string]struct{}, 32),
	}
}

// xy 世界坐标 → SVG 视口毫单位坐标：平移到包围盒原点、Y 翻转、量化到
// 0.001（整数毫单位参与 path 连续性判断与相对增量计算，无浮点漂移）。
func (em *svgEmitter) xy(x, y float64) (int64, int64) {
	return int64(math.Round((x - em.bbox.MinX) * 1000)),
		int64(math.Round((em.bbox.MaxY - y) * 1000))
}

// layerID 图层句柄 → 分组 id 片段：有名图层用真名（doc.LayerColors 解析
// 产物），无名/无图层信息用 handle-<hex>；按句柄缓存免逐图元格式化。
func (em *svgEmitter) layerID(doc *drawing.Document, h uint64) string {
	if id, ok := em.layerIDs[h]; ok {
		return id
	}
	id := ""
	if lc, ok := doc.LayerColors[h]; ok {
		if name := lc.DisplayName(); name != "" {
			id = name
		}
	}
	if id == "" {
		id = fmt.Sprintf("handle-%X", h)
	}
	em.layerIDs[h] = id
	return id
}

// groupFor 取线段分组组（键 = 颜色×图层，首次出现顺序保序）；分组数触顶
// 后退化：新图元（含新图层）一律并入同色纯颜色组，已有图层组保持不变。
// 出现过的图层 id 记入 layersSeen（metadata 图层清单按实际出图统计）。
func (em *svgEmitter) groupFor(doc *drawing.Document, layer uint64, k rgbKey) *svgStrokeGroup {
	id := em.layerID(doc, layer)
	em.layersSeen[id] = struct{}{}
	if !em.degraded {
		key := svgGroupKey{k, id}
		if i, ok := em.groupIdx[key]; ok {
			return em.groups[i]
		}
		if len(em.groups) < svgMaxGroups {
			g := &svgStrokeGroup{hex: hexFromRGB(k), id: "layer:" + key.layer, buf: make([]byte, 0, 4096)}
			em.groupIdx[key] = len(em.groups)
			em.groups = append(em.groups, g)
			return g
		}
		em.degraded = true
	}
	return em.plainGroup(k)
}

// plainGroup 退化后的纯颜色组：同色合并、无图层 id（视觉与原分组一致）。
func (em *svgEmitter) plainGroup(k rgbKey) *svgStrokeGroup {
	if i, ok := em.plainIdx[k]; ok {
		return em.groups[i]
	}
	g := &svgStrokeGroup{hex: hexFromRGB(k), buf: make([]byte, 0, 4096)}
	em.plainIdx[k] = len(em.groups)
	em.groups = append(em.groups, g)
	return g
}

// hexFromRGB 颜色键 → #rrggbb 小写十六进制（仅新分组时调用一次）。
func hexFromRGB(k rgbKey) string {
	const digits = "0123456789abcdef"
	var b [7]byte
	b[0] = '#'
	b[1], b[2] = digits[k.r>>4], digits[k.r&0xf]
	b[3], b[4] = digits[k.G>>4], digits[k.G&0xf]
	b[5], b[6] = digits[k.b>>4], digits[k.b&0xf]
	return string(b[:])
}

// rgbKeyOf color.RGBA → 分组键（alpha 不参与，输出恒不透明）。
func rgbKeyOf(c color.RGBA) rgbKey {
	return rgbKey{c.R, c.G, c.B}
}

// svgCollinearLimit 共线合并的分量绝对值上限：叉积/点积的中间乘积
// （2e9²×2 ≈ 8e18）不溢 int64。
const svgCollinearLimit = 2_000_000_000

// svgMaxPathAttr 是单个路径 d 属性的目标字节上限。
// 路径按子路径 M 边界分片，以减小单个 XML 属性的规模；解析器仍可能
// 对整个文件设置额外限制。
const svgMaxPathAttr = 1 << 20

// absI64 int64 绝对值（MinInt 防溢出取反）。
func absI64(v int64) int64 {
	if v < 0 {
		return -(v + 1) + 1
	}
	return v
}

// emit 将展开后的描边或文字图元编码为 SVG。
// 描边按颜色及图层分组，文字输出为 text；缺少文字内容时绘制占位框。
// 路径使用相对命令、数值精度调整和共线段合并减小输出体积，坐标量化
// 会引入有限误差。
func (em *svgEmitter) emit(doc *drawing.Document, p *drawing.Primitive, bgWhite bool) {
	switch p.Kind {
	case 0:
		if len(p.Strokes) == 0 {
			return
		}
		g := em.groupFor(doc, p.Layer, rgbKeyOf(styledEntityColor(doc, p, bgWhite, em.softPalette)))
		curX, curY := int64(0), int64(0) // 已输出位置（毫单位；pending 段逻辑上已到达）
		hasCur := false
		// pending 段：暂不落盘，下一段同向共线则累加延长，异向时才写出；
		// lastCmd 用于连续同类命令省略命令字母（l 1 2 3 4 / h 5 3）
		pendDx, pendDy := int64(0), int64(0)
		hasPend := false
		lastCmd := byte('M') // M 后隐式续接是绝对 lineto，相对增量必须显式命令
		flush := func() {
			if !hasPend {
				return
			}
			hasPend = false
			cmd, single := relCmd(pendDx, pendDy)
			switch {
			case cmd == 'l' && lastCmd == 'l':
				// 连续 l 省略命令字母，直接续写坐标对（两坐标均需分隔：
				// 正数缺分隔会把 "4.15 109.1" 连写为 "4.15109.1" 被解析
				// 成 4.15109 + 0.1，静默损坏几何）
				g.buf = appendSepNum(g.buf, pendDx)
				g.buf = appendSepNum(g.buf, pendDy)
			case cmd != 'l' && lastCmd == cmd:
				// 连续 h/v 省略命令字母，直接续写单坐标
				g.buf = appendSepNum(g.buf, single)
			default:
				g.buf = appendRelSeg(g.buf, pendDx, pendDy)
			}
			lastCmd = cmd
		}
		for _, s := range p.Strokes {
			if !drawing.Plausible(s.X1, s.Y1) || !drawing.Plausible(s.X2, s.Y2) {
				continue
			}
			x1, y1 := em.xy(s.X1, s.Y1)
			x2, y2 := em.xy(s.X2, s.Y2)
			if !hasCur || x1 != curX || y1 != curY {
				flush()
				g.buf = append(g.buf, 'M')
				dx1 := svgAutoDigits(x1)
				g.buf = appendFixed(g.buf, rescaleMilli(x1, dx1), dx1)
				g.buf = appendSepNum(g.buf, y1)
				hasPend = false
				lastCmd = 'M'
				curX, curY = x1, y1
			}
			dx, dy := x2-curX, y2-curY
			curX, curY = x2, y2
			hasCur = true
			if dx == 0 && dy == 0 {
				continue // 零长段无几何贡献
			}
			// 同向共线合并：叉积 0（共线）且点积 >0（同向，反向合并会改变
			// 几何覆盖）；分量限幅保证叉积/点积中间值不溢 int64
			if hasPend &&
				absI64(dx) <= svgCollinearLimit && absI64(dy) <= svgCollinearLimit &&
				absI64(pendDx) <= svgCollinearLimit && absI64(pendDy) <= svgCollinearLimit &&
				dx*pendDy-dy*pendDx == 0 && dx*pendDx+dy*pendDy > 0 {
				pendDx += dx
				pendDy += dy
				continue
			}
			flush()
			pendDx, pendDy = dx, dy
			hasPend = true
		}
		flush()
	case 1:
		if !drawing.Plausible(p.Lb.X, p.Lb.Y) || p.Lb.H <= 0 {
			return
		}
		col := rgbKeyOf(styledEntityColor(doc, p, bgWhite, em.softPalette))
		em.layersSeen[em.layerID(doc, p.Layer)] = struct{}{} // 文本图层也进 metadata 清单
		if p.Lb.Text != "" {
			em.texts = append(em.texts, em.textElement(p, col))
			return
		}
		// 空文本（MTEXT 剥离格式码后为空等）：输出半高占位框，与 PNG 对齐
		em.appendLabelBox(doc, p, col)
	}
}

// appendRelSeg 追加一段相对增量（含命令字母）：dx==0 用 v、dy==0 用 h、
// 其余 l；数字按量级自适应小数位（连续同类命令的字母省略由调用方处理）。
func appendRelSeg(b []byte, dx, dy int64) []byte {
	switch {
	case dx == 0:
		b = append(b, 'v')
		return appendSepNum(b, dy)
	case dy == 0:
		b = append(b, 'h')
		return appendSepNum(b, dx)
	}
	b = append(b, 'l')
	dd := svgAutoDigits(dx)
	b = appendFixed(b, rescaleMilli(dx, dd), dd)
	return appendSepNum(b, dy)
}

// relCmd 相对增量的 path 命令字母与单坐标值（h/v 只有一个坐标分量）。
func relCmd(dx, dy int64) (byte, int64) {
	switch {
	case dx == 0:
		return 'v', dy
	case dy == 0:
		return 'h', dx
	}
	return 'l', 0
}

// appendSepNum 追加空格分隔 + 毫单位自适应小数位的数字（负号本身即 SVG
// 数字分隔符不再加空格）。
func appendSepNum(b []byte, v int64) []byte {
	if v >= 0 {
		b = append(b, ' ')
	}
	d := svgAutoDigits(v)
	return appendFixed(b, rescaleMilli(v, d), d)
}

// rescaleMilli 把毫单位值降采样到目标小数位（10^(3-fracDigits) 定点）：
// svgAutoDigits 降低小数位时同步缩小数值刻度。
func rescaleMilli(v int64, fracDigits int) int64 {
	for i := fracDigits; i < 3; i++ {
		v /= 10
	}
	return v
}

// svgAutoDigits preserves the same 0.001-unit grid used by xy().
// Rounding relative deltas independently causes cumulative coordinate drift.
func svgAutoDigits(_ int64) int { return 3 }

// appendLabelBox 输出文字占位框：基线左端为原点、向上半高，随 rot 旋转
// （几何规则与 canvas.drawLabel 一致），d 数据并入同色×图层组。
func (em *svgEmitter) appendLabelBox(doc *drawing.Document, p *drawing.Primitive, k rgbKey) {
	g := em.groupFor(doc, p.Layer, k)
	lb := &p.Lb
	cos, sin := math.Cos(lb.Rot), math.Sin(lb.Rot)
	half := lb.H / 2
	curX, curY := int64(0), int64(0)
	for i, c := range [4][2]float64{{0, 0}, {lb.W, 0}, {lb.W, half}, {0, half}} {
		x, y := em.xy(lb.X+c[0]*cos-c[1]*sin, lb.Y+c[0]*sin+c[1]*cos)
		if i == 0 {
			g.buf = append(g.buf, 'M')
			dx0 := svgAutoDigits(x)
			g.buf = appendFixed(g.buf, rescaleMilli(x, dx0), dx0)
			g.buf = appendSepNum(g.buf, y)
		} else {
			g.buf = appendRelSeg(g.buf, x-curX, y-curY)
		}
		curX, curY = x, y
	}
	g.buf = append(g.buf, 'Z')
}

// textElement 序列化一个 <text> 元素：x/y 为基线起点（与 CAD 基线语义一致），
// font-size 为世界字高，text-anchor 按对齐码映射，旋转用 rotate(a x y)
// 绕基线起点旋转（Y 翻转后角度取负）；小角度省略 transform 减少体积。
// AI 元数据：data-type 文本类型（TEXT/ATTRIB/MTEXT）、data-h 源实体句柄
// 十六进制（AI 可回查原始实体；占位回退路径无句柄则省略）。
func (em *svgEmitter) textElement(p *drawing.Primitive, k rgbKey) []byte {
	lb := &p.Lb
	x, y := em.xy(lb.X, lb.Y)
	b := make([]byte, 0, 96+len(lb.Text)*3)
	b = append(b, `<text x="`...)
	b = appendFixed(b, x, 3)
	b = append(b, `" y="`...)
	b = appendFixed(b, y, 3)
	b = append(b, `" font-size="`...)
	b = appendFixed(b, int64(math.Round(lb.H*1000)), 3)
	b = append(b, `" fill="`...)
	b = append(b, hexFromRGB(k)...)
	b = append(b, '"')
	if p.Kind0 != "" {
		b = append(b, ` data-type="`...)
		b = svgAppendAttrEscaped(b, p.Kind0)
		b = append(b, '"')
	}
	if lb.Handle != 0 {
		b = append(b, ` data-h="`...)
		b = append(b, fmt.Sprintf("%X", lb.Handle)...)
		b = append(b, '"')
	}
	if deg := math.Round(-lb.Rot*18000/math.Pi) / 100; math.Abs(deg) >= 0.05 {
		b = append(b, ` transform="rotate(`...)
		b = appendFixed(b, int64(deg*100), 2)
		b = append(b, ' ')
		b = appendFixed(b, x, 3)
		b = append(b, ' ')
		b = appendFixed(b, y, 3)
		b = append(b, `)"`...)
	}
	switch lb.Anchor {
	case 1:
		b = append(b, ` text-anchor="middle"`...)
	case 2:
		b = append(b, ` text-anchor="end"`...)
	}
	b = append(b, '>')
	// 将 MTEXT 的显示行转换为 tspan，预览路径使用 1.2 倍字高的近似行距。
	// 后续行相对上一行下移，并继承文字的整体变换。
	lines := strings.Split(lb.Text, "\n")
	for i, ln := range lines {
		if i == 0 {
			b = svgAppendEscaped(b, ln)
			continue
		}
		b = append(b, `<tspan x="`...)
		b = appendFixed(b, x, 3)
		b = append(b, `" dy="`...)
		b = appendFixed(b, int64(math.Round(lb.H*1200)), 3)
		b = append(b, `">`...)
		b = svgAppendEscaped(b, ln)
		b = append(b, `</tspan>`...)
	}
	return append(b, `</text>`...)
}

// finish 组装最终 SVG 文档：声明 → 图纸摘要 <metadata>（首子元素，AI 消费）
// → 背景 rect → 按颜色×图层分组的线段 <g> → 文本 <g>（置于最上层）。
// 整段单 buffer 输出，避免中途多次拼接拷贝。
func (em *svgEmitter) finish(pxW, pxH int, vw, vh float64) []byte {
	size := 512 + len(em.texts)*96
	for _, g := range em.groups {
		size += 160 + len(g.buf)
	}
	b := make([]byte, 0, size)
	b = append(b, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"...)
	b = append(b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `...)
	b = appendFixed(b, int64(math.Round(vw*1000)), 3)
	b = append(b, ' ')
	b = appendFixed(b, int64(math.Round(vh*1000)), 3)
	b = append(b, `" width="`...)
	b = strconv.AppendInt(b, int64(pxW), 10)
	b = append(b, `" height="`...)
	b = strconv.AppendInt(b, int64(pxH), 10)
	b = append(b, `">
`...)
	// 图纸摘要：SVG 规范 metadata 元素，浏览器不渲染其内容（首子元素，
	// 便于 AI 解析器按序定位）；JSON 经 Marshal 的 HTML 转义 + XML 转义
	// 双重处理，图层名/图框名含特殊字符不破坏良构性
	if em.meta != nil {
		em.meta.Texts = len(em.texts)
		if len(em.layersSeen) > 0 {
			ls := make([]string, 0, len(em.layersSeen))
			for n := range em.layersSeen {
				ls = append(ls, n)
			}
			sort.Strings(ls)
			em.meta.Layers = ls
		}
		if jb, err := json.Marshal(em.meta); err == nil {
			b = append(b, `<metadata>`...)
			b = svgAppendEscaped(b, string(jb))
			b = append(b, `</metadata>
`...)
		}
	}
	// 背景矩形：截图/打印不透底
	b = append(b, `<rect x="0" y="0" width="`...)
	b = appendFixed(b, int64(math.Round(vw*1000)), 3)
	b = append(b, `" height="`...)
	b = appendFixed(b, int64(math.Round(vh*1000)), 3)
	b = append(b, `" fill="`...)
	b = append(b, hexFromRGB(rgbKeyOf(em.bg))...)
	b = append(b, `"/>
`...)
	if em.degraded {
		// 分组退化说明：图层碎片化时按纯颜色分组，图层语义见 metadata 图层清单
		b = append(b, `<!-- 线段分组数超过上限 `...)
		b = strconv.AppendInt(b, svgMaxGroups, 10)
		b = append(b, `，已退化为纯颜色分组 -->
`...)
	}
	for _, g := range em.groups {
		if len(g.buf) == 0 {
			continue
		}
		b = append(b, `<g`...)
		if g.id != "" {
			b = append(b, ` id="`...)
			b = svgAppendAttrEscaped(b, g.id)
			b = append(b, '"')
		}
		b = append(b, ` stroke="`...)
		b = append(b, g.hex...)
		b = append(b, `" fill="none" stroke-width="`...)
		b = appendFixed(b, int64(math.Round(em.strokeW*1000)), 3)
		b = append(b, `" stroke-linecap="round" stroke-linejoin="round"><path d="`...)
		// 单个 d 属性值长度受解析器限制（svgMaxPathAttr 注释），按 1MB
		// 分片；切点回退到最近的子路径 M 边界，
		// 保证每片 d 数据仍合法（每图元 d 均以 M 开头）
		chunk := g.buf
		for len(chunk) > svgMaxPathAttr {
			cut := bytes.LastIndexByte(chunk[:svgMaxPathAttr], 'M')
			if cut <= 0 {
				cut = svgMaxPathAttr // 防御：分片内无 M（正常不发生）
			}
			b = append(b, chunk[:cut]...)
			b = append(b, `"/>
<path d="`...)
			chunk = chunk[cut:]
		}
		b = append(b, chunk...)
		b = append(b, `"/>
</g>
`...)
	}
	if len(em.texts) > 0 {
		b = append(b, `<g font-family="'Heiti SC', 'Heiti TC', 'PingFang SC', 'PingFang TC', 'Microsoft YaHei', 'Noto Sans CJK SC', sans-serif">
`...)
		for _, t := range em.texts {
			b = append(b, t...)
			b = append(b, '\n')
		}
		b = append(b, `</g>
`...)
	}
	return append(b, `</svg>
`...)
}

// appendFixed 把 v/10^fracDigits 的定点整数写为十进制小数：去尾零与末位
// 小数点、正数省略整数部分的 0（0.087 → .087），零写 "0"。手写定点格式化
// 比 strconv.AppendFloat 快数倍（大图纸数百万坐标的序列化热点）。
// 调用方负责把值缩放到 10^fracDigits 定点（毫单位坐标经 rescaleMilli）。
func appendFixed(b []byte, v int64, fracDigits int) []byte {
	neg := v < 0
	var u uint64
	if neg {
		u = uint64(-(v + 1)) + 1 // 免 int64 溢出取负
	} else {
		u = uint64(v)
	}
	divisor := uint64(1)
	for i := 0; i < fracDigits; i++ {
		divisor *= 10
	}
	intPart, frac := u/divisor, u%divisor
	if neg && u != 0 {
		b = append(b, '-')
	}
	if intPart != 0 {
		b = strconv.AppendUint(b, intPart, 10)
	} else if frac != 0 && fracDigits > 0 {
		b = append(b, '0') // 纯小数保留整数位 0（属性值可读性；path 相对增量经 appendSepFixed 已有分隔）
	}
	if frac == 0 {
		if intPart == 0 {
			b = append(b, '0') // v==0 输出 "0"
		}
		return b
	}
	var digs [18]byte
	n := 0
	for n < fracDigits {
		digs[n] = byte('0' + frac%10)
		frac /= 10
		n++
	}
	// digs 为逆序（低位在前），数值尾零对应数组前部的 '0'，从前部裁剪
	// （frac>0 必有非零位，n 恒 ≥1）
	start := 0
	for start < n && digs[start] == '0' {
		start++
	}
	b = append(b, '.')
	for i := n - 1; i >= start; i-- {
		b = append(b, digs[i])
	}
	return b
}

// svgAppendEscaped XML 文本内容转义：& < > 转实体；剔除 XML 1.0 非法的
// 控制字符（错位解码文本可能含垃圾字节）；中文等多字节 UTF-8 原样输出。
func svgAppendEscaped(b []byte, s string) []byte {
	for _, r := range s {
		switch r {
		case '&':
			b = append(b, `&amp;`...)
		case '<':
			b = append(b, `&lt;`...)
		case '>':
			b = append(b, `&gt;`...)
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			b = append(b, string(r)...)
		}
	}
	return b
}

// svgAppendAttrEscaped XML 属性值转义：文本内容转义基础上加双引号
// （属性值以双引号包裹，图层名/类型名含引号时不破坏属性边界）。
// 独立遍历而非对整个 buffer ReplaceAll——只转义本次追加部分，已写入
// buffer 的引号是调用方输出的属性边界，不能动。
func svgAppendAttrEscaped(b []byte, s string) []byte {
	for _, r := range s {
		switch r {
		case '&':
			b = append(b, `&amp;`...)
		case '<':
			b = append(b, `&lt;`...)
		case '>':
			b = append(b, `&gt;`...)
		case '"':
			b = append(b, `&quot;`...)
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			b = append(b, string(r)...)
		}
	}
	return b
}
