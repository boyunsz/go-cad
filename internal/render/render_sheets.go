// 本文件识别模型空间中的图框，并按图框范围逐张渲染。
//
// 图框识别综合块尺寸、矩形边界、网格排列和内容分布等信号，随后从
// 标题栏提取图纸名称。未识别到图框时回退为整图；已识别图框之外的
// 内容可按空间分布生成补充区域。识别结果属于启发式分幅。
//
// 渲染管线复用展开和过滤结果。未指定 PNG 宽度时，根据文字像素高度
// 选择宽度，并在资源限制内尝试放大重渲；该检查不保证所有文字均可读。
// SVG 使用矢量输出，不执行基于像素高度的重渲检查。

package render

import (
	"bytes"
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Sheet 一个识别出的图框：图名与世界坐标包围盒。
type Sheet struct {
	Name string     // 图名（"图号-图名" > 图号 > 标题栏最大字号文本 > 块名 > "图 N"）
	Box  [4]float64 // 世界坐标 {minX, minY, maxX, maxY}
}

// SheetResult 批量渲染的单张结果。
type SheetResult struct {
	Name      string  // 图名（与 Sheet.Name 一致）
	Data      []byte  // 图片字节流（PNG 或 SVG，由 format 决定）
	Width     int     // 最终渲染宽度像素（PNG 为实际画布宽；SVG 为名义宽）
	PassRate  float64 // 文字清晰达标率：框内全部文本像素高 ≥8px 的占比（0~1）；SVG 等未统计时为 -1
	Rerenders int     // 清晰度自检触发的自动重渲次数（0=一次成型；SVG 恒为 0）
}

// sheetStdout 清晰度告警/重渲说明的输出流：默认进程 stdout（CLI 出图
// 时用户直接可见，无需人工调参判断），测试替换为内存缓冲断言告警路径。
var sheetStdout io.Writer = os.Stdout

// 图框识别与切分渲染常量。
const (
	// sheetRatioMin/Max 标准图幅宽高比窗口：A0~A4 系列宽高比 1.29~1.41
	//（含加宽图幅），两端各放宽 5% 容差；该窗口内单图框即可入选。
	sheetRatioMin = 1.29 * 0.95
	sheetRatioMax = 1.41 * 1.05
	// sheetRatioLongMax 加长图幅宽高比上限：真实设计院图纸常见 A2+/A3+
	// 加长图框（如 420×1189 比例 2.83），超出标准窗口、必须配合网格
	// 排列信号才入选。
	sheetRatioLongMax = 3.2
	// sheetMinSide 图框最短边下界（世界单位）：排除普通符号/设备块。
	sheetMinSide = 500
	// sheetOverlapFrac 重叠判定：相交面积超过较小框面积的该比例视为
	// 同一区域（贪心保留面积更大者）。
	sheetOverlapFrac = 0.3
	// sheetViewportMargin 单张视口在图框包围盒基础上外扩的比例，
	// 避免边框线中心贴画布边缘被裁半。
	sheetViewportMargin = 0.02
	// SheetDefaultWidth 是未指定尺寸时单张图纸的初始渲染宽度，单位为像素。
	SheetDefaultWidth = 4096
	// sheetDetectorBudget 图框检测的展开预算：块定义局部展开按定义缓存、
	// 候选精确展开数量有限；与整图渲染预算同级（NewTessellator 默认值），
	// 防大样图大块展开中途截断污染候选包围盒。
	sheetDetectorBudget = 2000000
	// frameEdgeFrac/frameTolFrac 框线信号参数：每边至少一条长 ≥ 边长
	// ×frameEdgeFrac 的平行线段，位于边容差带（×frameTolFrac）内。
	frameEdgeFrac = 0.8
	frameTolFrac  = 0.01
	// gridSizeTol/gridPosTol 网格排列判定容差：实例尺寸一致 ±2%、
	// 行/列对齐 ±2%。
	gridSizeTol = 0.02
	gridPosTol  = 0.02
	// sheetMaxBlockEnts 图框块定义实体数上限：图框块 = 框线 + 标题栏
	// 文字（含少量装饰分格），实体数少；内容大块（整张子图打包块数千
	// 实体）直接排除在展开之外，兼防展开预算被超大块耗尽。
	sheetMaxBlockEnts = 512
	// sheetMinContentDots 框内直属实体代表点下限：图框必然容纳图内容，
	// 孤立装饰框（设备外形矩形框/图例小框）框内近乎无直属图元。
	sheetMinContentDots = 8
	// sheetContentPassRate 内容判定块级通过率下限：同名块的全部实例中
	// 框内内容达标的占比低于该值时整块排除（设备块只有个别实例碰巧
	// 落在标注密集区，真图框每张都装内容）。
	sheetContentPassRate = 0.8
	// SheetMaxAutoWidth 自适应宽度上限：防微缩标注把宽度推到内存失控
	//（16384×~6500 RGBA 约 420MB/张，已覆盖 A0 图幅全部正文字清晰）。
	SheetMaxAutoWidth = 16384
	// sheetTextMinPx 是自适应宽度和渲染后检查使用的文字像素高度阈值。
	// 该阈值用于估算可读性，不表示所有字体和查看条件下的阅读保证。
	sheetTextMinPx = 8.0
	// sheetPassRateMin 渲染后清晰度自检的达标率下限：视口内全部文本
	// 像素高达标（≥sheetTextMinPx）占比低于该值时触发加倍宽度重渲。
	// 自适应宽度按 p25 字高对齐 8px（天然 ≥75% 达标），低于 60% 只会
	// 发生在自适应被 SheetMaxAutoWidth 钳制的微缩标注场景，正是需要
	// 自动重渲的输入。
	sheetPassRateMin = 0.6
	// sheetHardMaxWidth 清晰度自检重渲链的宽度硬上限：16384 仍不达标
	// 时允许翻倍到 32768（微缩标注最后一档），再往上已无物理意义。
	sheetHardMaxWidth = 32768
	// sheetMaxPixels 是单张画布的像素数上限，用于限制渲染内存占用。
	// RGBA 每像素占 4 字节；候选宽高乘积超过上限时，不再使用该尺寸重渲染。
	sheetMaxPixels = 4.0e8
	// sheetResidualQuantile 残余点分位裁剪比例：X/Y 各保留 [q, 1-q]
	// 分位窗口内的点，窗口外为离群带（错位解码/垃圾实体远端坐标）。
	sheetResidualQuantile = 0.005
	// sheetResidualMinDots 残余连通分量点数下限：低于该值的零散区域
	// 不成图（宁可漏识别不可把整图切成碎片）。
	sheetResidualMinDots = 50
	// sheetResidualMarginFrac 补充区域包围盒每边外扩比例（边框文字
	// 贴边不被裁）。
	sheetResidualMarginFrac = 0.05
	// sheetResidualOverlapFrac 补充区域与已识别框重叠剔除阈值：分量
	// 包围盒与任一标准框相交面积超过自身面积该比例视为紧贴图框的
	// 标注带（环绕框的轴号圈/尺寸链）而非独立内容区。
	sheetResidualOverlapFrac = 0.3
	// sheetResidualCoarseRes 和 sheetResidualCoarseCell 控制粗网格分辨率及有效点数阈值。
	// 低密度单元被忽略，以减少零散标注将相距较远的区域连接起来。
	sheetResidualCoarseRes  = 64
	sheetResidualCoarseCell = 5
	// sheetResidualGapFrac 是补充区域合并时使用的相对间距阈值。
	// 当两个区域的间距小于其较小边长的该比例时，将它们合并为一个区域。
	sheetResidualGapFrac = 0.3
	// sheetResidualFineCell 指定细分聚类的世界坐标网格宽度。
	// 网格数量受 sheetResidualFineRes 限制；该参数用于控制区域分离粒度，
	// 具体效果取决于图纸尺寸及内容分布。
	sheetResidualFineCell = 10000.0
	// sheetResidualFineCellMin 是细网格参与聚类所需的最低点数。
	// 过滤低密度单元可减少稀疏轴网或标注连接独立内容区域的情况。
	sheetResidualFineCellMin = 5
	// sheetResidualFineRes 细分网格每轴格数上限（内存/规模防护）。
	sheetResidualFineRes = 512
	// sheetResidualMinDensity 指定补充区域的最低点密度。
	// 密度以每 1e10 世界坐标面积单位的点数计算，用于排除稀疏标注区域。
	sheetResidualMinDensity = 100.0
	// sheetResidualMinTextFrac 指定细分区域独立成页所需的直属文字点比例。
	// 未达到该比例的符号区域合并处理，以减少过度分幅。
	sheetResidualMinTextFrac = 0.3
	// sheetResidualMaxSheets 补充区域产出上限：按分量点数取前 N，
	// 防异常样本输出爆炸。
	sheetResidualMaxSheets = 16
)

// DetectSheets 根据模型空间块尺寸、边界和内容分布识别图框。
// 返回名称及世界坐标包围盒，名称依次尝试标题栏、块名和自动编号。
// 找不到图框时返回“整图”范围；识别到图框后，可追加未覆盖内容的
// 补充区域。结果为启发式识别，doc 为 nil 时返回 nil。
func DetectSheets(doc *drawing.Document) []Sheet {
	if doc == nil {
		return nil
	}
	sd := newSheetDetector(doc)
	cands := sd.candidates()
	if len(cands) == 0 {
		return sd.fallbackWhole()
	}
	kept := dedupSheets(cands)
	if len(kept) == 0 {
		return sd.fallbackWhole()
	}
	sheets := make([]Sheet, 0, len(kept)+2)
	covered := make([]drawing.Box2, 0, len(kept))
	for i, c := range kept {
		name := sd.sheetTitle(c)
		if name == "" {
			name = sd.blockName(c.ins.BlockHeader)
		}
		if name == "" {
			name = fmt.Sprintf("图 %d", i+1)
		}
		sheets = append(sheets, Sheet{
			Name: name,
			Box:  [4]float64{c.box.MinX, c.box.MinY, c.box.MaxX, c.box.MaxY},
		})
		covered = append(covered, c.box)
	}
	return append(sheets, sd.residualSheets(covered)...)
}

// sheetTextLike 直属文本样本（图名提取用）。
type sheetTextLike struct {
	X, y   float64 // 插入点（世界坐标）
	hWorld float64 // 世界字高
	text   string
}

// sheetDetector 图框检测器：共享顶点索引与展开预算，块定义局部展开
// 结果按块句柄缓存（同一符号块被引用多次只展开一次）。
type sheetDetector struct {
	doc         *drawing.Document
	ts          *drawing.Tessellator                      // 共享顶点索引 + 展开预算
	blockPrimsC map[uint64][]drawing.Primitive            // 块句柄 → 块定义局部展开图元（缓存）
	blockBox    map[uint64]drawing.Box2                   // 块句柄 → 块定义局部包围盒（缓存）
	insertPrims map[*entity.EntInsert][]drawing.Primitive // 候选 INSERT → 世界展开图元（缓存）
	modelTexts  []sheetTextLike                           // 模型空间直属文本（图名提取第二来源）
	dots        [][2]float64                              // 模型空间直属实体代表点（内容密度与残余聚类共用）
	dotText     map[[2]float64]struct{}                   // dots 中文本插入点集合（文字页/符号区判别）
}

// newSheetDetector 构造检测器：BuildVertexIndex 一次供全部块展开共用
// （POLYLINE 顶点聚合），预算放宽防大块展开中途截断；直属文本清单
// 供标题栏图名提取。
func newSheetDetector(doc *drawing.Document) *sheetDetector {
	ts := drawing.NewTessellator(doc)
	ts.BuildVertexIndex()
	ts.Budget = sheetDetectorBudget
	dots := contentDots(doc)
	return &sheetDetector{
		doc:         doc,
		ts:          ts,
		blockPrimsC: make(map[uint64][]drawing.Primitive, 64),
		blockBox:    make(map[uint64]drawing.Box2, 64),
		insertPrims: make(map[*entity.EntInsert][]drawing.Primitive, 16),
		modelTexts:  collectModelTexts(doc),
		dots:        dots,
		dotText:     textDotSet(doc),
	}
}

// textDotSet 直属文本插入点集合（与 contentDots 同源同坐标，浮点精确
// 匹配），供残余聚类判别分量是文字页还是符号图块区。
func textDotSet(doc *drawing.Document) map[[2]float64]struct{} {
	set := make(map[[2]float64]struct{}, 1024)
	for _, ent := range doc.ModelSpace {
		switch e := ent.(type) {
		case *entity.EntText:
			if drawing.Plausible(e.Insertion.X, e.Insertion.Y) {
				set[[2]float64{e.Insertion.X, e.Insertion.Y}] = struct{}{}
			}
		case *entity.EntMText:
			if drawing.Plausible(e.Insertion.X, e.Insertion.Y) {
				set[[2]float64{e.Insertion.X, e.Insertion.Y}] = struct{}{}
			}
		case *entity.EntAttrib:
			if drawing.Plausible(e.Insertion.X, e.Insertion.Y) {
				set[[2]float64{e.Insertion.X, e.Insertion.Y}] = struct{}{}
			}
		}
	}
	return set
}

// sheetCand 通过粗筛的图框候选。
type sheetCand struct {
	ins *entity.EntInsert // 图框块参照
	box drawing.Box2      // 精确世界包围盒（AABB，含旋转外扩）
}

// contentDots 模型空间直属实体的代表点样本（内容密度判定用）：
// 图框的使命是容纳图内容，框内应落有相当数量的直属图元；
// 装饰框/设备图例框（同比例、带框线但孤立）框内近乎无直属图元。
func contentDots(doc *drawing.Document) [][2]float64 {
	var dots [][2]float64
	add := func(x, y float64) {
		if drawing.Plausible(x, y) {
			dots = append(dots, [2]float64{x, y})
		}
	}
	for _, ent := range doc.ModelSpace {
		switch e := ent.(type) {
		case *entity.EntLine:
			add(e.Start.X, e.Start.Y)
			add(e.End.X, e.End.Y)
		case *entity.EntText:
			add(e.Insertion.X, e.Insertion.Y)
		case *entity.EntAttrib:
			add(e.Insertion.X, e.Insertion.Y)
		case *entity.EntMText:
			add(e.Insertion.X, e.Insertion.Y)
		case *entity.EntCircle:
			add(e.Center.X, e.Center.Y)
		case *entity.EntArc:
			add(e.Center.X, e.Center.Y)
		case *entity.EntLwPolyline:
			for _, v := range e.Vertices {
				add(v.X, v.Y)
			}
		case *entity.EntPoint:
			add(e.Location.X, e.Location.Y)
		}
	}
	return dots
}

// candidates 收集模型空间 INSERT 中通过组合判据的候选：
//   - big：缩放后最短边超过 sheetMinSide（排除普通符号/设备块）；
//   - ents：块定义实体数 ≤ sheetMaxBlockEnts（图框块 = 框线 + 标题栏，
//     实体数少；内容大块排除在展开之外，兼防展开预算被超大块耗尽）；
//   - ratio：宽高比落在标准 A 系窗口 [sheetRatioMin, sheetRatioMax] 或
//     加长图幅窗口（至 sheetRatioLongMax，A2+/A3+ 加长在真实设计院图纸
//     常见，宽高比可达 ~3）；
//   - rect：块内存在覆盖包围盒四边的框线（图框外框的本质特征，展开
//     stroke 通用判定，LWPOLYLINE/LINE/POLYLINE 边框均覆盖）；
//   - grid 或 ratioA：同名块 ≥2 引用且网格排列（平铺多张图的强信号，
//     加长图幅必须满足），或标准比例窗（单图框图纸无网格信号）；
//   - content：框内直属实体代表点 ≥ sheetMinContentDots（框是"装内容
//     的"，排除设备外形矩形框等孤立装饰框）。
func (sd *sheetDetector) candidates() []sheetCand {
	// 模型空间 INSERT 按块聚合（实例世界近似尺寸供网格判定），
	// 块序保持首次出现顺序保证候选输出顺序确定
	type insts struct {
		list  []*entity.EntInsert
		boxes []drawing.Box2 // 角点外接近似世界包围盒（网格判定用）
	}
	var blockOrder []uint64
	byBlock := map[uint64]*insts{}
	for _, ent := range sd.doc.ModelSpace {
		ins, ok := ent.(*entity.EntInsert)
		if !ok {
			continue
		}
		if n := len(sd.doc.Blocks[ins.BlockHeader]); n == 0 || n > sheetMaxBlockEnts {
			continue
		}
		lb := sd.blockLocalBox(ins.BlockHeader)
		if lb.Invalid() {
			continue
		}
		w := (lb.MaxX - lb.MinX) * math.Abs(ins.Scale.X)
		h := (lb.MaxY - lb.MinY) * math.Abs(ins.Scale.Y)
		if w <= 0 || h <= 0 {
			continue
		}
		if math.Min(w, h) < sheetMinSide {
			continue
		}
		ratio := math.Max(w, h) / math.Min(w, h)
		if ratio < sheetRatioMin || ratio > sheetRatioLongMax {
			continue
		}
		prims := sd.blockPrims(ins.BlockHeader)
		if !frameRect(sd.doc, ins.BlockHeader, prims, lb) {
			continue
		}
		g := byBlock[ins.BlockHeader]
		if g == nil {
			g = &insts{}
			byBlock[ins.BlockHeader] = g
			blockOrder = append(blockOrder, ins.BlockHeader)
		}
		g.list = append(g.list, ins)
		g.boxes = append(g.boxes, cornerBox(lb, drawing.InsertXform(ins)))
	}
	dots := sd.dots
	var cands []sheetCand
	for _, h := range blockOrder {
		g := byBlock[h]
		ratioOKA := false
		if lb := sd.blockLocalBox(h); !lb.Invalid() {
			w := (lb.MaxX - lb.MinX) * math.Abs(g.list[0].Scale.X)
			hh := (lb.MaxY - lb.MinY) * math.Abs(g.list[0].Scale.Y)
			ratio := math.Max(w, hh) / math.Min(w, hh)
			ratioOKA = ratio <= sheetRatioMax
		}
		// 标准比例窗单图框即可入选；加长图幅（粗筛 ratio 已 ≤LongMax）
		// 必须网格排列（平铺多张图强信号）防加长内容块误纳
		if !ratioOKA && !gridAligned(g.boxes) {
			continue
		}
		// 根据同一块各实例的内容密度通过率筛选图框候选。
		// 低于 sheetContentPassRate 的块整体排除，以减少设备外形框误识别。
		boxes := make([]drawing.Box2, len(g.list))
		counts := make([]int, len(g.list))
		pass := 0
		for i, ins := range g.list {
			boxes[i] = drawing.PrimitivesBounds(sd.expandInsert(ins))
			counts[i] = countDotsIn(dots, boxes[i])
			if counts[i] >= sheetMinContentDots {
				pass++
			}
		}
		if pass == 0 || float64(pass) < float64(len(g.list))*sheetContentPassRate {
			continue
		}
		for i, ins := range g.list {
			if boxes[i].Invalid() || counts[i] < sheetMinContentDots {
				continue
			}
			cands = append(cands, sheetCand{ins: ins, box: boxes[i]})
		}
	}
	return cands
}

// countDotsIn 统计落在框内的代表点数（线性扫描；候选数量有限，
// 点样本量级 1e4~1e5，总开销可控）。
func countDotsIn(dots [][2]float64, b drawing.Box2) int {
	n := 0
	for _, d := range dots {
		if d[0] >= b.MinX && d[0] <= b.MaxX && d[1] >= b.MinY && d[1] <= b.MaxY {
			n++
		}
	}
	return n
}

// cornerBox 局部包围盒经仿射变换后的角点外接近似世界包围盒（旋转非
// 90° 倍数时高估，仅网格判定粗用；候选最终 bbox 走精确展开）。
func cornerBox(b drawing.Box2, t drawing.Xform) drawing.Box2 {
	out := drawing.Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
	framePts := []entity.Point2{{X: b.MinX, Y: b.MinY}, {X: b.MaxX, Y: b.MinY}, {X: b.MinX, Y: b.MaxY}, {X: b.MaxX, Y: b.MaxY}}
	for _, p := range framePts {
		q := t.Apply(p)
		out.Extend(q.X, q.Y)
	}
	return out
}

// frameRect 框线信号：块内存在覆盖包围盒四边的框线（图框外框特征）。
// 两级判定：
//  1. stroke 级（rectStrokeFrame）：每边至少一条平行长线段（长 ≥ 边长
//     × frameEdgeFrac，位于边容差带内），覆盖 LINE/闭合 LWPOLYLINE/
//     POLYLINE 展开段；
//  2. 顶点级：单个 LWPOLYLINE 顶点构成轴对齐矩形——覆盖未显式闭合的
//     4 顶点矩形外框（TessLwPolyline 对开放折线只展开 n-1 段，闭合边
//     缺失，stroke 级会漏判）。
//
// GB 图框必有矩形外框，设备/符号块无此特征，是图框与内容块的本质区分。
func frameRect(doc *drawing.Document, h uint64, prims []drawing.Primitive, b drawing.Box2) bool {
	if rectStrokeFrame(prims, b) {
		return true
	}
	for _, e := range doc.Blocks[h] {
		p, ok := e.(*entity.EntLwPolyline)
		if !ok || len(p.Vertices) < 4 || len(p.Vertices) > 5 {
			continue
		}
		// 矩形顶点特征：唯一 x/y 各至多 2 值，全部顶点落在其上；
		// 矩形面积覆盖块包围盒 ≥ frameEdgeFrac（块内图幅标注文字会
		// 撑高包围盒，不能要求顶点贴合包围盒边界）
		xs := [2]float64{math.Inf(1), math.Inf(-1)}
		ys := [2]float64{math.Inf(1), math.Inf(-1)}
		for _, v := range p.Vertices {
			if v.X < xs[0] {
				xs[0] = v.X
			}
			if v.X > xs[1] {
				xs[1] = v.X
			}
			if v.Y < ys[0] {
				ys[0] = v.Y
			}
			if v.Y > ys[1] {
				ys[1] = v.Y
			}
		}
		isRect := true
		for _, v := range p.Vertices {
			if (v.X != xs[0] && v.X != xs[1]) || (v.Y != ys[0] && v.Y != ys[1]) {
				isRect = false
				break
			}
		}
		if isRect && (xs[1]-xs[0])*(ys[1]-ys[0]) >= (b.MaxX-b.MinX)*(b.MaxY-b.MinY)*frameEdgeFrac {
			return true
		}
	}
	return false
}

// rectStrokeFrame stroke 级四边框线判定（每边至少一条容差带内的长平行线段）。
func rectStrokeFrame(prims []drawing.Primitive, b drawing.Box2) bool {
	w := b.MaxX - b.MinX
	h := b.MaxY - b.MinY
	if w <= 0 || h <= 0 {
		return false
	}
	tol := math.Max(w, h) * frameTolFrac
	top, bottom, left, right := false, false, false, false
	for _, p := range prims {
		if p.Kind != 0 {
			continue
		}
		for _, s := range p.Strokes {
			if !drawing.Plausible(s.X1, s.Y1) || !drawing.Plausible(s.X2, s.Y2) {
				continue
			}
			dx := math.Abs(s.X2 - s.X1)
			dy := math.Abs(s.Y2 - s.Y1)
			if dy <= tol && dx >= w*frameEdgeFrac {
				if math.Abs(s.Y1-b.MinY) <= tol {
					bottom = true
				}
				if math.Abs(s.Y1-b.MaxY) <= tol {
					top = true
				}
			}
			if dx <= tol && dy >= h*frameEdgeFrac {
				if math.Abs(s.X1-b.MinX) <= tol {
					left = true
				}
				if math.Abs(s.X1-b.MaxX) <= tol {
					right = true
				}
			}
			if top && bottom && left && right {
				return true
			}
		}
	}
	return false
}

// gridAligned 同块引用的网格排列判定（平铺图框强信号）：实例世界
// 尺寸一致（±gridSizeTol）且满足列对齐 / 行对齐 / 网格填充之一。
// 列/行对齐覆盖单列/单行平铺；网格填充覆盖多行多列（去重容差聚类，
// 要求两维相邻间距 ≥ 半个尺寸防密集符号块误判）。
func gridAligned(boxes []drawing.Box2) bool {
	if len(boxes) < 2 {
		return false
	}
	w := boxes[0].MaxX - boxes[0].MinX
	h := boxes[0].MaxY - boxes[0].MinY
	for _, b := range boxes[1:] {
		if math.Abs((b.MaxX-b.MinX)-w) > w*gridSizeTol || math.Abs((b.MaxY-b.MinY)-h) > h*gridSizeTol {
			return false
		}
	}
	colAlign, rowAlign := true, true
	for _, b := range boxes[1:] {
		if math.Abs(b.MinX-boxes[0].MinX) > w*gridPosTol {
			colAlign = false
		}
		if math.Abs(b.MinY-boxes[0].MinY) > h*gridPosTol {
			rowAlign = false
		}
	}
	if colAlign || rowAlign {
		return true
	}
	xs := gridCluster(boxes, gridSizeTol, func(b drawing.Box2) float64 { return (b.MinX + b.MaxX) / 2 })
	ys := gridCluster(boxes, gridSizeTol, func(b drawing.Box2) float64 { return (b.MinY + b.MaxY) / 2 })
	if len(xs)*len(ys) < len(boxes) {
		return false
	}
	return spaced(xs, w) && spaced(ys, h)
}

// gridCluster 按容差聚类一维坐标（返回各簇均值，升序）。
func gridCluster(boxes []drawing.Box2, tol float64, key func(drawing.Box2) float64) []float64 {
	vals := make([]float64, 0, len(boxes))
	for _, b := range boxes {
		vals = append(vals, key(b))
	}
	sort.Float64s(vals)
	var out []float64
	sum, n := vals[0], 1
	for i := 1; i < len(vals); i++ {
		if vals[i]-vals[i-1] <= tol {
			sum += vals[i]
			n++
			continue
		}
		out = append(out, sum/float64(n))
		sum, n = vals[i], 1
	}
	return append(out, sum/float64(n))
}

// spaced 聚类中心相邻间距是否 ≥ 半个尺寸（排除散布符号的偶然重叠）。
func spaced(centers []float64, size float64) bool {
	for i := 1; i < len(centers); i++ {
		if centers[i]-centers[i-1] < size*0.5 {
			return false
		}
	}
	return true
}

// blockPrims 块定义局部展开图元（identity 变换，缓存）。
// 展开中的递归环用占位短路；每块独立预算（保存/恢复），防超大块把
// 共享预算耗尽导致后续块缓存被截断污染。
func (sd *sheetDetector) blockPrims(h uint64) []drawing.Primitive {
	if p, ok := sd.blockPrimsC[h]; ok {
		return p
	}
	// 占位空展开：同块递归引用（自包含环）短路，budget 不被环消耗
	sd.blockPrimsC[h] = nil
	sd.blockBox[h] = drawing.Box2{MinX: math.Inf(1), MaxX: math.Inf(-1)}
	saved := sd.ts.Budget
	sd.ts.Budget = sheetDetectorBudget
	prims := make([]drawing.Primitive, 0, 64)
	for _, inner := range sd.doc.Blocks[h] {
		prims = sd.ts.AppendEntity(prims, inner, drawing.IdentityXform(), 0)
	}
	sd.ts.Budget = saved
	sd.blockPrimsC[h] = prims
	sd.blockBox[h] = drawing.PrimitivesBounds(prims)
	return prims
}

// blockLocalBox 块定义局部坐标包围盒（blockPrims 缓存派生）。
func (sd *sheetDetector) blockLocalBox(h uint64) drawing.Box2 {
	sd.blockPrims(h)
	return sd.blockBox[h]
}

// expandInsert 图框候选的完整世界展开：块内图元经插入变换展开，
// 关联 ATTRIB 文字一并展开（标题栏图名常为属性文本）。
func (sd *sheetDetector) expandInsert(ins *entity.EntInsert) []drawing.Primitive {
	if p, ok := sd.insertPrims[ins]; ok {
		return p
	}
	child := drawing.InsertXform(ins)
	out := make([]drawing.Primitive, 0, 64)
	for _, inner := range sd.doc.Blocks[ins.BlockHeader] {
		out = sd.ts.AppendEntity(out, inner, child, 0)
	}
	for _, ah := range ins.Attribs {
		if a, ok := sd.doc.Attribs[ah]; ok {
			out = sd.ts.AppendEntity(out, a, child, 0)
		}
	}
	sd.insertPrims[ins] = out
	return out
}

// dedupSheets 贪心重叠去重：按框面积降序稳定排序（同面积保持模型空间
// 出现顺序，结果顺序确定），与已保留框的相交面积超过较小框面积
// sheetOverlapFrac 的候选剔除（同一区域重复图框或图框套图框场景保留最大者）。
func dedupSheets(cands []sheetCand) []sheetCand {
	sort.SliceStable(cands, func(i, j int) bool {
		return sheetArea(cands[i].box) > sheetArea(cands[j].box)
	})
	kept := make([]sheetCand, 0, len(cands))
	for _, c := range cands {
		overlap := false
		for _, k := range kept {
			if sheetOverlapArea(c.box, k.box) > sheetOverlapFrac*math.Min(sheetArea(c.box), sheetArea(k.box)) {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, c)
		}
	}
	return kept
}

// sheetArea 返回矩形宽高之积，不校验边界顺序或有限性。
// 调用方应先保证包围盒有效。
func sheetArea(b drawing.Box2) float64 {
	return (b.MaxX - b.MinX) * (b.MaxY - b.MinY)
}

// sheetOverlapArea 两框相交面积（不相交为 0）。
func sheetOverlapArea(a, b drawing.Box2) float64 {
	w := math.Min(a.MaxX, b.MaxX) - math.Max(a.MinX, b.MinX)
	h := math.Min(a.MaxY, b.MaxY) - math.Max(a.MinY, b.MinY)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// sheetTitle 提取标题栏图名，三级策略：
//  1. 图号优先：图框右下角标题栏区（约 1/4 宽 × 1/6 高，Y 轴向上坐标系
//     中 x 大 y 小）内匹配图号模式（sheetNoRe，如 RD-24 / RD(XF)-05）的
//     文本取作图号；图名取图号附近（同行或上方一个标题栏行距、右端
//     对齐）的中文为主长文本，组合为 "图号-图名"（如
//     "RD(XF)-05-地下一层消防平面图"），无图名时仅用图号；
//  2. 标题栏区无图号时回退区内世界字高最大的文本（历史策略；图旁的
//     图例表/线缆表字号可能更大，图号优先正是为绕开它们）；
//  3. 标题栏区无文本返回空串，由调用方回退块名/序号。
//
// 文本两个来源：图框块展开的 label（块内 TEXT/MTEXT/嵌套/ATTRIB 属性）
// 与模型空间直属文本（画完图框后直接标注的标题栏文字），跨来源统一
// 处理。世界字高 = 展开推进基向量长度 × 字高（块内局部），跨嵌套块可比。
func (sd *sheetDetector) sheetTitle(c sheetCand) string {
	prims := sd.expandInsert(c.ins)
	tw := (c.box.MaxX - c.box.MinX) * 0.25
	th := (c.box.MaxY - c.box.MinY) / 6
	x0 := c.box.MaxX - tw
	y0 := c.box.MinY
	y1 := c.box.MinY + th
	inTitle := func(x, y float64) bool {
		return drawing.Plausible(x, y) && x >= x0 && x <= c.box.MaxX && y >= y0 && y <= y1
	}
	var inTexts, allTexts []sheetTextLike
	add := func(x, y, hWorld float64, text string) {
		t := sheetTextLike{X: x, y: y, hWorld: hWorld, text: text}
		allTexts = append(allTexts, t)
		if inTitle(x, y) {
			inTexts = append(inTexts, t)
		}
	}
	for _, p := range prims {
		if p.Kind != 1 || p.Lb.Tx == nil || !drawing.Plausible(p.Lb.X, p.Lb.Y) {
			continue
		}
		text := strings.TrimSpace(strings.Join(p.Lb.Tx.Lines, " "))
		if text == "" {
			continue
		}
		add(p.Lb.X, p.Lb.Y, math.Hypot(p.Lb.Tx.Ux, p.Lb.Tx.Uy)*p.Lb.Tx.HWorld, text)
	}
	for _, t := range sd.modelTexts {
		add(t.X, t.y, t.hWorld, t.text)
	}
	if no := pickSheetNo(inTexts, c.box); no != nil {
		if name := sheetNameNear(no, allTexts); name != "" {
			return no.text + "-" + name
		}
		return no.text
	}
	best, bestH := "", 0.0
	for _, t := range inTexts {
		if t.hWorld > bestH {
			bestH = t.hWorld
			best = t.text
		}
	}
	return best
}

// sheetNoRe 图号文本模式：大写字母段 + 可选括号专业代号 + 连字符 + 数字，
// 如 RD-24 / EL-1 / RD(XF)-05（首尾锚定，线缆型号 WDZB1N-KYJY-… 不匹配）。
var sheetNoRe = regexp.MustCompile(`^[A-Z]{1,6}(?:\([A-Z]{1,4}\))?-\d{1,3}$`)

// pickSheetNo 从标题栏区文本中挑选图号：匹配 sheetNoRe 者，世界字高大者
// 优先，同级取距图框中心最近者（标题栏居中一格为正式图号栏）。
func pickSheetNo(texts []sheetTextLike, b drawing.Box2) *sheetTextLike {
	var best *sheetTextLike
	bestD := 0.0
	cx, cy := (b.MinX+b.MaxX)/2, (b.MinY+b.MaxY)/2
	for i := range texts {
		t := &texts[i]
		if !sheetNoRe.MatchString(t.text) {
			continue
		}
		if best == nil || t.hWorld > best.hWorld+1e-9 {
			best, bestD = t, math.Hypot(t.X-cx, t.y-cy)
			continue
		}
		if t.hWorld >= best.hWorld-1e-9 {
			if d := math.Hypot(t.X-cx, t.y-cy); d < bestD {
				best, bestD = t, d
			}
		}
	}
	return best
}

// sheetNameNear 从图号附近选择候选图名。
// 候选须满足相对字高定义的行距和右端对齐范围，并通过 sheetCjkName
// 检查；优先选择字号较大的文本，同字号时选择距离较近者。
func sheetNameNear(no *sheetTextLike, texts []sheetTextLike) string {
	rightNo := no.X + estTextWidth(no.text, no.hWorld)
	var best *sheetTextLike
	bestD := 0.0
	for i := range texts {
		t := &texts[i]
		if t == no || !sheetCjkName(t.text) {
			continue
		}
		dy := t.y - no.y
		if dy <= -1.5*no.hWorld || dy > 25*no.hWorld {
			continue
		}
		if right := t.X + estTextWidth(t.text, t.hWorld); math.Abs(right-rightNo) > 6*no.hWorld {
			continue
		}
		if best == nil || t.hWorld > best.hWorld+1e-9 {
			best, bestD = t, math.Abs(dy)
			continue
		}
		if t.hWorld >= best.hWorld-1e-9 {
			if d := math.Abs(dy); d < bestD {
				best, bestD = t, d
			}
		}
	}
	if best == nil {
		return ""
	}
	return best.text
}

// sheetCjkName 中文为主长文本判定：非空白字符中汉字 ≥4 个且占比 >50%
// （图纸名称、楼层/系统描述；图例文字、线缆型号、纯编号排除）。
func sheetCjkName(s string) bool {
	han, total := 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	return han >= 4 && han*2 > total
}

// estTextWidth 估算文本世界宽度：CJK 全角字符 ≈1.0×字高、其余 ≈0.55×
// 字高（半角字母数字经验值）。仅用于标题栏图名/图号对齐判定，非排版。
func estTextWidth(text string, h float64) float64 {
	w := 0.0
	for _, r := range text {
		if r > 0x2e80 {
			w += h
		} else {
			w += h * 0.55
		}
	}
	return w
}

// blockName 块定义句柄 → BLOCK_HEADER 真名（InternalObjs 登记值），
// 匿名块名（"*Model_Space" 等 * 前缀）返回空串避免误作图名。
func (sd *sheetDetector) blockName(h uint64) string {
	g := sd.doc.InternalObjs[h]
	if g == nil {
		return ""
	}
	s, _ := g.Field("name").(string)
	if strings.HasPrefix(s, "*") {
		return ""
	}
	return s
}

// fallbackWhole 为没有可用图框候选的文档生成单张“整图”结果。
// 范围由 quantileSheetBounds 估计，可能排除远离主要内容的坐标簇。
func (sd *sheetDetector) fallbackWhole() []Sheet {
	prim := drawing.NewTessellator(sd.doc)
	prims := prim.ExpandAll()
	bbox := quantileSheetBounds(prims)
	if bbox.Invalid() {
		bbox = drawing.Box2{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}
	}
	return []Sheet{{
		Name: "整图",
		Box:  [4]float64{bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY},
	}}
}

// residualComp 残余网格密度聚类的连通分量结果：成员点（共享调用方
// 切片底层）、分量内点精确包围盒与点数。
type residualComp struct {
	pts  [][2]float64
	box  drawing.Box2
	dots int
}

// residualGridParams 网格密度聚类参数。
type residualGridParams struct {
	res     int     // 每轴格数上限（N×N）；cell<=0 时为实际格数（包围盒均分）
	cell    float64 // 固定世界格宽（>0 时生效，格数=跨度/格宽 钳制到 res 上限）
	cellMin int     // 格计数下限：低于该值的格视为空格（零星噪声格剔除，防桥接连片）
	gapJump bool    // 跳格桥接：相隔恰好 1 个空格的达标格视为连通（容忍文字
	// 行跳格/双栏中缝等页内小空隙；2 格以上空隙——页间距离——保持分离）
	minDots int // 分量点数下限
}

// residualSheets 将已识别图框之外的内容尝试划分为补充区域。
// 先对代表点作分位裁剪，再进行粗网格和细网格聚类；根据区域间距、
// 文字比例、点数和密度决定保留或合并。与标准图框重叠的区域会被过滤。
// 区域以候选标题或“补充区域 N”命名，输出数量受上限约束。
func (sd *sheetDetector) residualSheets(covered []drawing.Box2) []Sheet {
	residual := make([][2]float64, 0, len(sd.dots))
	for _, d := range sd.dots {
		inFrame := false
		for _, b := range covered {
			if pointInBox(d[0], d[1], b) {
				inFrame = true
				break
			}
		}
		if !inFrame {
			residual = append(residual, d)
		}
	}
	residual = clipDotsQuantile(residual)
	comps := gridDensityComponents(residual, residualGridParams{
		res:     sheetResidualCoarseRes,
		cellMin: sheetResidualCoarseCell,
		gapJump: true,
		minDots: sheetResidualMinDots,
	})
	// 对粗聚类分量进行细网格重聚类，再根据间距合并相邻子区域。
	// 无法进一步分离的内容保留为一个区域。
	finals := make([]residualComp, 0, len(comps))
	for _, c := range comps {
		subs := gridDensityComponents(c.pts, residualGridParams{
			res:     sheetResidualFineRes,
			cell:    sheetResidualFineCell,
			cellMin: sheetResidualFineCellMin,
			gapJump: true,
			minDots: sheetResidualMinDots,
		})
		if len(subs) <= 1 {
			finals = append(finals, c)
			continue
		}
		merged := mergeCloseComps(subs, sheetResidualGapFrac)
		// 文字页独立成块，符号图块区（文本占比低，细分只会长出符号列
		// 碎片）合并为整块
		var symbol residualComp
		symbol.box = drawing.Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
		for _, mc := range merged {
			if sd.textFrac(mc) >= sheetResidualMinTextFrac {
				finals = append(finals, mc)
				continue
			}
			symbol.pts = append(symbol.pts, mc.pts...)
			for _, d := range mc.pts {
				symbol.box.Extend(d[0], d[1])
			}
			symbol.dots += mc.dots
		}
		if symbol.dots >= sheetResidualMinDots {
			finals = append(finals, symbol)
		}
	}
	// 密度下限（轴号标注带剔除）+ 产出上限（按点数保留前 N，顺序确定）
	dense := finals[:0]
	for _, c := range finals {
		area := (c.box.MaxX - c.box.MinX) * (c.box.MaxY - c.box.MinY)
		if area > 0 && float64(c.dots)/area*1e10 >= sheetResidualMinDensity {
			dense = append(dense, c)
		}
	}
	sort.SliceStable(dense, func(i, j int) bool { return dense[i].dots > dense[j].dots })
	if len(dense) > sheetResidualMaxSheets {
		dense = dense[:sheetResidualMaxSheets]
	}
	sheets := make([]Sheet, 0, len(dense))
	n := 0
	for _, comp := range dense {
		// 与任一标准框大面积重叠的分量是紧贴图框的环绕标注带而非
		// 独立内容区，剔除防碎片化
		overlap := false
		for _, b := range covered {
			if sheetOverlapArea(comp.box, b) > sheetResidualOverlapFrac*sheetArea(comp.box) {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		n++
		mx := (comp.box.MaxX - comp.box.MinX) * sheetResidualMarginFrac
		my := (comp.box.MaxY - comp.box.MinY) * sheetResidualMarginFrac
		if mx == 0 && my == 0 {
			mx, my = 1, 1
		} else if mx == 0 {
			mx = my
		} else if my == 0 {
			my = mx
		}
		box := [4]float64{comp.box.MinX - mx, comp.box.MinY - my, comp.box.MaxX + mx, comp.box.MaxY + my}
		// 命名时使用外扩后的范围，以包含可能位于点云边缘之外的标题基线。
		name := sd.residualTitle(drawing.Box2{MinX: box[0], MinY: box[1], MaxX: box[2], MaxY: box[3]})
		if name == "" {
			name = fmt.Sprintf("补充区域 %d", n)
		}
		sheets = append(sheets, Sheet{Name: name, Box: box})
	}
	return sheets
}

// boxGap 两包围盒的最近间距（AABB 分离距离：x/y 方向间隙的较大者，
// 相交或接触为 0）。残余细分子分量的粘连判定用。
func boxGap(a, b drawing.Box2) float64 {
	dx := math.Max(a.MinX-b.MaxX, b.MinX-a.MaxX)
	dy := math.Max(a.MinY-b.MaxY, b.MinY-a.MaxY)
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}
	return math.Hypot(dx, dy)
}

// compMinSide 分量包围盒较小边长（粘连判定的相对基准：页面自己的
// 尺寸，替代绝对世界单位阈值）。
func compMinSide(b drawing.Box2) float64 {
	return math.Min(b.MaxX-b.MinX, b.MaxY-b.MinY)
}

// mergeCloseComps 残余细分产物的间距合并（并查集）：两分量间距小于
// 两者较小边长 × gapFrac 视为同页粘连（一个页面被细网格切碎的相邻
// 块），合并回同页；间距更大视为不同页保持拆分。相对判据与图纸量纲
// 无关，替代原固定跨度阈值。输出按各组首个成员的输入顺序排列（结果
// 确定），组内成员点/包围盒/点数累加。
func mergeCloseComps(comps []residualComp, gapFrac float64) []residualComp {
	n := len(comps)
	if n <= 1 {
		return comps
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if boxGap(comps[i].box, comps[j].box) < gapFrac*math.Min(compMinSide(comps[i].box), compMinSide(comps[j].box)) {
				parent[find(j)] = find(i)
			}
		}
	}
	// 按 root 归并成员，组序取组内最小下标（首现顺序，结果确定）
	idx := make([]int, n)
	for i := range idx {
		idx[i] = -1
	}
	var out []residualComp
	for i, c := range comps {
		r := find(i)
		if idx[r] < 0 {
			idx[r] = len(out)
			out = append(out, residualComp{box: drawing.Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}})
		}
		m := &out[idx[r]]
		m.pts = append(m.pts, c.pts...)
		m.box.Extend(c.box.MinX, c.box.MinY)
		m.box.Extend(c.box.MaxX, c.box.MaxY)
		m.dots += c.dots
	}
	return out
}

// clipDotsQuantile 按 X/Y 分位窗口原地过滤 residual 中的离群点。
// 返回值复用输入切片的底层数组；调用方不得假定原元素顺序或内容保持不变。
func clipDotsQuantile(dots [][2]float64) [][2]float64 {
	n := len(dots)
	if n < 4 {
		return dots
	}
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i, d := range dots {
		xs[i], ys[i] = d[0], d[1]
	}
	sort.Float64s(xs)
	sort.Float64s(ys)
	at := func(sorted []float64, q float64) float64 {
		return sorted[int(float64(n-1)*q)]
	}
	loX, hiX := at(xs, sheetResidualQuantile), at(xs, 1-sheetResidualQuantile)
	loY, hiY := at(ys, sheetResidualQuantile), at(ys, 1-sheetResidualQuantile)
	out := dots[:0]
	for _, d := range dots {
		if d[0] >= loX && d[0] <= hiX && d[1] >= loY && d[1] <= hiY {
			out = append(out, d)
		}
	}
	return out
}

// gridDensityComponents 按网格统计点密度并提取八邻域连通分量。
// 低于 cellMin 的网格不参与聚类；可选的邻域扩张用于连接小范围空隙。
// 仅保留点数达到 minDots 的分量，并按点数降序返回。
func gridDensityComponents(dots [][2]float64, p residualGridParams) []residualComp {
	if p.res <= 0 || p.minDots <= 0 || len(dots) == 0 {
		return nil
	}
	var bb drawing.Box2
	bb.MinX, bb.MinY = math.Inf(1), math.Inf(1)
	bb.MaxX, bb.MaxY = math.Inf(-1), math.Inf(-1)
	for _, d := range dots {
		bb.Extend(d[0], d[1])
	}
	if bb.Invalid() {
		return nil
	}
	// 按固定世界格宽或包围盒均分确定网格，两个轴的格数均受 res 限制。
	spanX, spanY := bb.MaxX-bb.MinX, bb.MaxY-bb.MinY
	cw, ch := spanX/float64(p.res), spanY/float64(p.res)
	nx, ny := p.res, p.res
	if p.cell > 0 {
		cw, ch = p.cell, p.cell
		nx = int(spanX/p.cell) + 1
		ny = int(spanY/p.cell) + 1
		if nx > p.res {
			nx = p.res
		}
		if ny > p.res {
			ny = p.res
		}
	}
	if cw <= 0 {
		cw = 1
	}
	if ch <= 0 {
		ch = 1
	}
	cellOf := func(x, y float64) int {
		i := int((x - bb.MinX) / cw)
		if i >= nx {
			i = nx - 1
		}
		j := int((y - bb.MinY) / ch)
		if j >= ny {
			j = ny - 1
		}
		return j*nx + i
	}
	grid := make([]int, nx*ny)
	for _, d := range dots {
		grid[cellOf(d[0], d[1])]++
	}
	// 达标格判定（计数达下限；未达标格的点不参与任何分量）
	ok := make([]bool, len(grid))
	for i, c := range grid {
		ok[i] = c >= p.cellMin
	}
	// 栈式 BFS 提取 8-连通分量，达标格间若恰好隔 1 个空格且开启跳格
	// 桥接则同样连通（吸收文字行跳格/双栏中缝等页内小空隙；2 格以上
	// 空隙——页间距离——保持分离），compID 记录达标格归属供点级归并
	compID := make([]int, len(grid))
	for i := range compID {
		compID[i] = -1
	}
	var comps []residualComp
	reach := func(ci, cj int) int {
		if ci < 0 || ci >= nx || cj < 0 || cj >= ny {
			return -1
		}
		ni := cj*nx + ci
		if !ok[ni] || compID[ni] >= 0 {
			return -1
		}
		return ni
	}
	for start := range grid {
		if !ok[start] || compID[start] >= 0 {
			continue
		}
		id := len(comps)
		// 包围盒初始化为 ±Inf：零值 Box2{0,0,0,0} 会让全正象限分量的
		// minX/minY 停留在原点，bbox 被撑到 (0,0) 造成虚假大跨度
		comps = append(comps, residualComp{box: drawing.Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}})
		cells := []int{start}
		compID[start] = id
		for k := 0; k < len(cells); k++ {
			cx, cy := cells[k]%nx, cells[k]/nx
			for dj := -2; dj <= 2; dj++ {
				for di := -2; di <= 2; di++ {
					if di == 0 && dj == 0 {
						continue
					}
					step := di
					if step < 0 {
						step = -step
					}
					sj := dj
					if sj < 0 {
						sj = -sj
					}
					if step > 1 && sj > 1 {
						continue // 跳格只走正交直线方向
					}
					if step <= 1 && sj <= 1 {
						// 8 邻直达
						if ni := reach(cx+di, cy+dj); ni >= 0 {
							compID[ni] = id
							cells = append(cells, ni)
						}
						continue
					}
					// 跳 2 格：中间格必须为空格
					if p.gapJump {
						mx, my := cx+di/2, cy+dj/2
						if mx >= 0 && mx < nx && my >= 0 && my < ny && !ok[my*nx+mx] {
							if ni := reach(cx+di, cy+dj); ni >= 0 {
								compID[ni] = id
								cells = append(cells, ni)
							}
						}
					}
				}
			}
		}
	}
	// 点级归并：每点按所属达标格的分量 id 累计成员点、精确包围盒与计数
	for _, d := range dots {
		if id := compID[cellOf(d[0], d[1])]; id >= 0 {
			comps[id].pts = append(comps[id].pts, d)
			comps[id].box.Extend(d[0], d[1])
			comps[id].dots++
		}
	}
	out := comps[:0]
	for _, c := range comps {
		if c.dots >= p.minDots {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].dots > out[j].dots })
	return out
}

// textFrac 分量内直属文本插入点占比（残余聚类文字页/符号区判别用）。
func (sd *sheetDetector) textFrac(c residualComp) float64 {
	if c.dots == 0 {
		return 0
	}
	n := 0
	for _, d := range c.pts {
		if _, ok := sd.dotText[d]; ok {
			n++
		}
	}
	return float64(n) / float64(c.dots)
}

// residualTitle 选择补充区域内字号较大的可读直属文本作为标题。
// 字号相近时优先选择靠近区域水平中心的文本；没有候选时返回空字符串。
func (sd *sheetDetector) residualTitle(b drawing.Box2) string {
	bestH := 0.0
	for _, t := range sd.modelTexts {
		if t.hWorld > bestH && pointInBox(t.X, t.y, b) && sheetReadableText(t.text) {
			bestH = t.hWorld
		}
	}
	best, bestD := "", math.Inf(1)
	cx := (b.MinX + b.MaxX) / 2
	for _, t := range sd.modelTexts {
		if t.hWorld < bestH*0.9 || !pointInBox(t.X, t.y, b) || !sheetReadableText(t.text) {
			continue
		}
		if d := math.Abs(t.X - cx); d < bestD {
			bestD = d
			best = t.text
		}
	}
	return best
}

// sheetReadableText 可读文本判定：至少含一个字母或数字字符（排除
// 纯符号/线型填充文本），补充区域命名用。
func sheetReadableText(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// quantileSheetBounds 通过坐标分位数和主要坐标簇估计图纸范围。
// 先取两个轴的 0.5%–99.5% 分位区间，再由 principalRange 收紧。
// 点数不足时使用图元包围盒，分位区间退化时使用 RobustBounds。
// 该范围用于预览，可能裁去远离主要内容的有效图元。
func quantileSheetBounds(prims []drawing.Primitive) drawing.Box2 {
	var n int
	for _, p := range prims {
		switch p.Kind {
		case 0:
			n += len(p.Strokes) * 2
		case 1:
			if drawing.Plausible(p.Lb.X, p.Lb.Y) {
				n++
			}
		}
	}
	if n < 4 {
		return drawing.PrimitivesBounds(prims)
	}
	xs := make([]float64, 0, n)
	ys := make([]float64, 0, n)
	for _, p := range prims {
		switch p.Kind {
		case 0:
			for _, s := range p.Strokes {
				if drawing.Plausible(s.X1, s.Y1) {
					xs = append(xs, s.X1)
					ys = append(ys, s.Y1)
				}
				if drawing.Plausible(s.X2, s.Y2) {
					xs = append(xs, s.X2)
					ys = append(ys, s.Y2)
				}
			}
		case 1:
			if drawing.Plausible(p.Lb.X, p.Lb.Y) {
				xs = append(xs, p.Lb.X)
				ys = append(ys, p.Lb.Y)
			}
		}
	}
	if len(xs) < 4 {
		return drawing.PrimitivesBounds(prims)
	}
	sort.Float64s(xs)
	sort.Float64s(ys)
	at := func(sorted []float64, q float64) float64 {
		return sorted[int(float64(len(sorted)-1)*q)]
	}
	b := drawing.Box2{MinX: at(xs, 0.005), MinY: at(ys, 0.005), MaxX: at(xs, 0.995), MaxY: at(ys, 0.995)}
	if b.MaxX-b.MinX <= 0 || b.MaxY-b.MinY <= 0 {
		return drawing.RobustBounds(prims)
	}
	b.MinX, b.MaxX = principalRange(xs, b.MinX, b.MaxX)
	b.MinY, b.MaxY = principalRange(ys, b.MinY, b.MaxY)
	return b
}

// principalRange 在已排序坐标的分位区间内估计主要内容范围。
// 坐标先归入等分直方图；连续空带超过 quantGapBreakFrac 指定的相对
// 阈值时分开区域，再选择点数最多的连通区域收紧范围。
// 该预览策略可能排除远处的有效图元，不用于判定源文件内容是否完整。
func principalRange(sorted []float64, lo, hi float64) (float64, float64) {
	const (
		bands = 64
		// 15% 跨度：连续空带超过 带数×3/20 视为分量断开
		quantGapBreakNum, quantGapBreakDen = 3, 20
	)
	span := hi - lo
	if span <= 0 || len(sorted) == 0 {
		return lo, hi
	}
	bw := span / bands
	gapBreak := bands * quantGapBreakNum / quantGapBreakDen
	hist := make([]int, bands)
	for _, v := range sorted {
		i := int((v - lo) / bw)
		if i < 0 {
			i = 0
		} else if i >= bands {
			i = bands - 1
		}
		hist[i]++
	}
	// 扫描连通带簇（点数/起止带），保留点数最大者
	bestLo, bestHi, bestN := lo, hi, 0
	curLo, curHi, curN, gap := -1, -1, 0, gapBreak+1
	flush := func(endBand int) {
		if curN > bestN {
			bestLo = lo + float64(curLo)*bw
			bestHi = lo + float64(endBand+1)*bw
			bestN = curN
		}
	}
	for i, c := range hist {
		if c > 0 {
			if gap > gapBreak {
				if curLo >= 0 {
					flush(curHi)
				}
				curLo, curN = i, 0
			}
			curHi = i
			curN += c
			gap = 0
		} else if curLo >= 0 {
			gap++
		}
	}
	if curLo >= 0 {
		flush(curHi)
	}
	if bestN == 0 {
		return lo, hi
	}
	return bestLo, bestHi
}

// 逐张渲染。

// sheetViewport 图框包围盒 → 渲染视口：外扩 sheetViewportMargin 边距，
// 零尺寸退化回退（与 RenderPNG 的边距规则一致）。
func sheetViewport(b [4]float64) drawing.Box2 {
	bb := drawing.Box2{MinX: b[0], MinY: b[1], MaxX: b[2], MaxY: b[3]}
	marginX := (bb.MaxX - bb.MinX) * sheetViewportMargin
	marginY := (bb.MaxY - bb.MinY) * sheetViewportMargin
	if marginX == 0 && marginY == 0 {
		marginX, marginY = 1, 1
	} else if marginX == 0 {
		marginX = marginY
	} else if marginY == 0 {
		marginY = marginX
	}
	return drawing.Box2{MinX: bb.MinX - marginX, MinY: bb.MinY - marginY, MaxX: bb.MaxX + marginX, MaxY: bb.MaxY + marginY}
}

// sheetPipeline 图框切分渲染的共享素材：整图展开与全局过滤一次完成，
// 逐张复用（多张出图只展开一遍，RenderAllSheets 的性能基础）。
type sheetPipeline struct {
	doc        *drawing.Document
	prims      []drawing.Primitive
	modelTexts []sheetTextLike // 直属文本（自适应宽度统计用）
}

// prepareSheets 准备可供多张图纸复用的预览图元和文本。
// 展开、异常线段过滤及范围估计与预览 PNG 路径使用相同策略，避免逐张
// 重复展开整份文档。
func prepareSheets(doc *drawing.Document) *sheetPipeline {
	prim := drawing.NewTessellator(doc)
	prims := prim.ExpandAll()
	prims = filterRadiatingStrokes(prims)
	bbox := quantileSheetBounds(prims)
	if bbox.Invalid() {
		bbox = drawing.Box2{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}
	}
	prims = dropOriginAnchored(prims, bbox)
	return &sheetPipeline{doc: doc, prims: prims, modelTexts: collectModelTexts(doc)}
}

// collectModelTexts 模型空间直属文本清单（图名提取与自适应宽度共用）。
func collectModelTexts(doc *drawing.Document) []sheetTextLike {
	var out []sheetTextLike
	add := func(x, y, h float64, text string) {
		if drawing.Plausible(x, y) && h > 0 {
			if t := strings.TrimSpace(text); t != "" {
				out = append(out, sheetTextLike{X: x, y: y, hWorld: h, text: t})
			}
		}
	}
	for _, ent := range doc.ModelSpace {
		switch e := ent.(type) {
		case *entity.EntText:
			add(e.Insertion.X, e.Insertion.Y, e.Height, e.Text)
		case *entity.EntMText:
			add(e.Insertion.X, e.Insertion.Y, e.TextHeight, drawing.StripMTextFormat(e.Text))
		case *entity.EntAttrib:
			add(e.Insertion.X, e.Insertion.Y, e.Height, e.Text)
		}
	}
	return out
}

// sheetWidthFor 单张渲染宽度：显式指定用之；未指定（<=0）时按框内
// 文字自适应——主体文字（世界字高 p25 分位）像素高 ≥8px，钳制到
// [SheetDefaultWidth, SheetMaxAutoWidth]。1:150 等放大图幅的微缩标注
// （字高 100 世界单位）在固定 4096 宽下仅 ~2px，必须按内容放宽才能
// 满足"图内文字标注清晰可读"。
func (sp *sheetPipeline) sheetWidthFor(vp drawing.Box2, sheet Sheet, width int) int {
	if width > 0 {
		return width
	}
	var hs []float64
	for i := range sp.prims {
		p := &sp.prims[i]
		if p.Kind != 1 || p.Lb.Tx == nil || !primInBox(p, vp) {
			continue
		}
		if h := math.Hypot(p.Lb.Tx.Ux, p.Lb.Tx.Uy) * p.Lb.Tx.HWorld; h > 0 {
			hs = append(hs, h)
		}
	}
	b := drawing.Box2{MinX: sheet.Box[0], MinY: sheet.Box[1], MaxX: sheet.Box[2], MaxY: sheet.Box[3]}
	for _, t := range sp.modelTexts {
		if pointInBox(t.X, t.y, b) {
			hs = append(hs, t.hWorld)
		}
	}
	const targetPx = sheetTextMinPx
	vw := vp.MaxX - vp.MinX
	if len(hs) == 0 || vw <= 0 {
		return SheetDefaultWidth
	}
	sort.Float64s(hs)
	hq := hs[len(hs)/4] // p25 分位（主体文字，微缩小字不绑架分辨率）
	if hq <= 0 {
		return SheetDefaultWidth
	}
	w := int(vw*targetPx/hq + 0.5)
	if w < SheetDefaultWidth {
		w = SheetDefaultWidth
	}
	if w > SheetMaxAutoWidth {
		w = SheetMaxAutoWidth
	}
	return w
}

// renderPNG 渲染单张图框为 PNG：视口裁剪到图框包围盒（外扩边距），
// 框外图元按包围盒相交预剔除（跨框长线由画布裁剪回退）。渲染后执行
// 清晰度自检：统计框内全部文本 label 的像素高达标率，低于
// sheetPassRateMin 且宽度未到上限时加倍宽度重渲（自动出图即清晰的
// 核心，用户无需人工调参选宽度）；画布像素超内存上限的档位拒绝，
// 保留已达成的最优结果。显式指定宽度（opts.Width>0）视为用户明确
// 控制，只统计达标率不自动重渲（整图全览张即走该路径不被放大）。
// 返回 PNG 字节流、最终渲染宽度、文字清晰达标率（0~1）与自动重渲次数。
func (sp *sheetPipeline) renderPNG(sheet Sheet, opts RenderOptions) ([]byte, int, float64, int, error) {
	vp := sheetViewport(sheet.Box)
	width := sp.sheetWidthFor(vp, sheet, opts.Width)
	vw := vp.MaxX - vp.MinX
	data, err := sp.renderPNGAt(vp, width, opts)
	if err != nil {
		return nil, width, 0, 0, err
	}
	rate := sheetTextPassRate(sp.prims, vp, float64(width)/vw)
	rerenders := 0
	if opts.Width > 0 {
		return data, width, rate, rerenders, nil
	}
	for rate < sheetPassRateMin && width < sheetHardMaxWidth {
		next := width * 2
		if !sheetPixelsOK(vp, next) {
			fmt.Fprintf(sheetStdout,
				"cad: 警告 图框 %q 文字清晰达标率 %.0f%% 低于 %d%%；%d 宽上档需画布 %d 像素超内存上限 %g，保留 %d 宽结果\n",
				sheet.Name, rate*100, int(sheetPassRateMin*100), next,
				int(float64(next)*(vp.MaxY-vp.MinY)/vw), sheetMaxPixels, width)
			break
		}
		var d2 []byte
		d2, err = sp.renderPNGAt(vp, next, opts)
		if err != nil {
			return nil, width, rate, rerenders, err
		}
		width, data = next, d2
		rerenders++
		rate = sheetTextPassRate(sp.prims, vp, float64(width)/vw)
	}
	if rate < sheetPassRateMin {
		fmt.Fprintf(sheetStdout,
			"cad: 警告 图框 %q 文字清晰达标率仅 %.0f%%（≥%dpx 占比），%d 宽已达微缩标注物理极限，无法自动放大\n",
			sheet.Name, rate*100, int(sheetTextMinPx), width)
	}
	return data, width, rate, rerenders, nil
}

// renderPNGAt 按指定宽度渲染一档 PNG（不含自适应与重渲决策）。
func (sp *sheetPipeline) renderPNGAt(vp drawing.Box2, width int, opts RenderOptions) ([]byte, error) {
	vw := vp.MaxX - vp.MinX
	scale := float64(width) / vw
	height := int((vp.MaxY-vp.MinY)*scale + 0.5)
	if height < 1 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	drawRect(img, img.Rect, opts.Background)
	cv := &canvas{img: img, width: float64(width), height: float64(height), lineWidth: adaptiveLineWidth(float64(width)) * renderLineScale(opts.LineScale), softPalette: opts.SoftPalette, fontPath: opts.FontPath}
	cv.setTransform(vp, scale)
	bgWhite := opts.Background.R > 127
	for i := range sp.prims {
		if !primInBox(&sp.prims[i], vp) {
			continue
		}
		cv.drawPrimitive(sp.prims[i], sp.doc, bgWhite)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sheetTextPassRate 渲染后清晰度自检：统计视口内全部文本 label 的
// 渲染像素高（世界字高 × scale，label 基向量模长已含块变换比例）达
// sheetTextMinPx 的占比。无文本的图框无清晰度问题，视为达标 1。
func sheetTextPassRate(prims []drawing.Primitive, vp drawing.Box2, scale float64) float64 {
	total, ok := 0, 0
	for i := range prims {
		p := &prims[i]
		if p.Kind != 1 || p.Lb.Tx == nil || !primInBox(p, vp) {
			continue
		}
		h := math.Hypot(p.Lb.Tx.Ux, p.Lb.Tx.Uy) * p.Lb.Tx.HWorld
		if h <= 0 {
			continue
		}
		total++
		if h*scale >= sheetTextMinPx {
			ok++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(ok) / float64(total)
}

// sheetPixelsOK 指定宽度的画布像素量是否在内存上限内（宽×高 ≤
// sheetMaxPixels，RGBA 每像素 4 字节）：超过即拒绝该档重渲，防 32768
// 宽 × 竖版高把内存推到 OOM。
func sheetPixelsOK(vp drawing.Box2, width int) bool {
	vw := vp.MaxX - vp.MinX
	if vw <= 0 || width <= 0 {
		return true
	}
	h := (vp.MaxY - vp.MinY) * (float64(width) / vw)
	return float64(width)*h <= sheetMaxPixels
}

// renderSVG 渲染单张图框为 SVG：viewBox 裁剪到图框包围盒，线段/文本
// 序列化与整图 RenderSVG 同一发射器（文字 text 元素由查看器字体渲染）。
// SVG 矢量无像素概念，清晰度自检与自动重渲不适用，返回名义宽度。
func (sp *sheetPipeline) renderSVG(sheet Sheet, opts RenderOptions) ([]byte, int, error) {
	vp := sheetViewport(sheet.Box)
	width := sp.sheetWidthFor(vp, sheet, opts.Width)
	vw := vp.MaxX - vp.MinX
	vh := vp.MaxY - vp.MinY
	wf := float64(width)
	height := int(vh/vw*wf + 0.5)
	if height < 1 {
		height = 1
	}
	strokeW := vw / wf * 1.5 * renderLineScale(opts.LineScale)
	bgWhite := opts.Background.R > 127
	em := newSVGEmitter(vp, strokeW, opts.Background, &svgDocMeta{
		Generator: "go-cad",
		Version:   sp.doc.Version(),
		Entities:  len(sp.prims),
		Sheet:     sheet.Name,
	})
	em.softPalette = opts.SoftPalette
	for i := range sp.prims {
		if !primInBox(&sp.prims[i], vp) {
			continue
		}
		em.emit(sp.doc, &sp.prims[i], bgWhite)
	}
	return em.finish(width, height, vw, vh), width, nil
}

// primInBox 图元与视口粗相交判定（框外图元剔除）：stroke 图元任一段
// 与视口 AABB 相交即保留（整段绘制由画布参数化裁剪回退）；label 图元
// 按基线起点或终点落在视口内判定。
func primInBox(p *drawing.Primitive, b drawing.Box2) bool {
	switch p.Kind {
	case 0:
		for _, s := range p.Strokes {
			ok1, ok2 := drawing.Plausible(s.X1, s.Y1), drawing.Plausible(s.X2, s.Y2)
			if !ok1 && !ok2 {
				continue
			}
			if !ok1 {
				s.X1, s.Y1 = s.X2, s.Y2
			} else if !ok2 {
				s.X2, s.Y2 = s.X1, s.Y1
			}
			if math.Max(s.X1, s.X2) >= b.MinX && math.Min(s.X1, s.X2) <= b.MaxX &&
				math.Max(s.Y1, s.Y2) >= b.MinY && math.Min(s.Y1, s.Y2) <= b.MaxY {
				return true
			}
		}
		return false
	case 1:
		if !drawing.Plausible(p.Lb.X, p.Lb.Y) {
			return false
		}
		ex := p.Lb.X + p.Lb.W*math.Cos(p.Lb.Rot)
		ey := p.Lb.Y + p.Lb.W*math.Sin(p.Lb.Rot)
		return pointInBox(p.Lb.X, p.Lb.Y, b) || pointInBox(ex, ey, b)
	}
	return false
}

// pointInBox 点是否在框内。
func pointInBox(x, y float64, b drawing.Box2) bool {
	return x >= b.MinX && x <= b.MaxX && y >= b.MinY && y <= b.MaxY
}

// normalizeSheetOptions 渲染选项归一化：白底默认；宽度 <=0 保留给
// 渲染层按框内文字自适应（normalizeSheetOptions 不感知内容）。
func normalizeSheetOptions(opts RenderOptions) RenderOptions {
	if opts.Background == (color.RGBA{}) {
		opts.Background = color.RGBA{255, 255, 255, 255}
	}
	return opts
}

// RenderSheetPNG 渲染单个图框为 PNG 字节流。opts.Width 未指定时按框内
// 文字自适应起步（主体文字像素高 ≥8px，下限 4096），渲染后执行清晰度
// 自检与自动重渲（见 renderPNG）；显式指定宽度则完全由调用方控制。
func RenderSheetPNG(doc *drawing.Document, sheet Sheet, opts RenderOptions) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("cad: 文档为空，无法渲染")
	}
	opts = normalizeSheetOptions(opts)
	data, _, _, _, err := prepareSheets(doc).renderPNG(sheet, opts)
	return data, err
}

// RenderSheetSVG 渲染单个图框为 SVG 矢量字节流（viewBox 裁剪到图框）。
func RenderSheetSVG(doc *drawing.Document, sheet Sheet, opts RenderOptions) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("cad: 文档为空，无法渲染")
	}
	opts = normalizeSheetOptions(opts)
	data, _, err := prepareSheets(doc).renderSVG(sheet, opts)
	return data, err
}

// RenderAllSheets 图框切分批量渲染：DetectSheets 识别全部图框（无图框
// 回退整图单张），整图展开一次逐张复用。输出清单首项固定为"整图全览"
// 单张（整图稳健分位视口 quantileSheetBounds）——用户除逐张图框外总要
// 一张"整的"；无图框回退单张（Name "整图"）本身即全览，不重复添加。
// format 取 "png" 或 "svg"（大小写不敏感），其余报错。
func RenderAllSheets(doc *drawing.Document, opts RenderOptions, format string) ([]SheetResult, error) {
	if doc == nil {
		return nil, fmt.Errorf("cad: 文档为空，无法渲染")
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png", "svg":
	default:
		return nil, fmt.Errorf("cad: 不支持的输出格式 %q（可选 png/svg）", format)
	}
	opts = normalizeSheetOptions(opts)
	sheets := DetectSheets(doc)
	sp := prepareSheets(doc)
	if len(sheets) != 1 || sheets[0].Name != "整图" {
		if b := quantileSheetBounds(sp.prims); !b.Invalid() {
			sheets = append([]Sheet{{Name: "整图全览", Box: [4]float64{b.MinX, b.MinY, b.MaxX, b.MaxY}}}, sheets...)
		}
	}
	results := make([]SheetResult, 0, len(sheets))
	for _, s := range sheets {
		// 全览张未显式指定宽度时钳到 SheetDefaultWidth：整图 p25 字高
		// 是微缩标注，自适应会把宽度推满 SheetMaxAutoWidth（~420MB/张），
		// 全览只需整体预览、无需逐行小字可读；显式宽度同时使自检不
		// 自动重渲（全览张是预览定位，不参与清晰度放大）
		opt := opts
		if s.Name == "整图全览" && opt.Width <= 0 {
			opt.Width = SheetDefaultWidth
		}
		var data []byte
		var width int
		var rate float64
		var rerenders int
		var err error
		if strings.EqualFold(strings.TrimSpace(format), "svg") {
			data, width, err = sp.renderSVG(s, opt)
			rate = -1 // SVG 矢量无像素概念，达标率未统计
		} else {
			data, width, rate, rerenders, err = sp.renderPNG(s, opt)
		}
		if err != nil {
			return nil, fmt.Errorf("cad: 渲染图框 %q: %w", s.Name, err)
		}
		results = append(results, SheetResult{Name: s.Name, Data: data, Width: width, PassRate: rate, Rerenders: rerenders})
	}
	return results, nil
}

// SanitizeSheetName 图名 → 文件名净化：空白与文件系统非法字符替换为
// '-'（连续合并）、其余控制字符剔除、首尾 '-.' 裁剪；空结果回退
// "sheet"，超长按 rune 截断到 80。
func SanitizeSheetName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case unicode.IsSpace(r) || strings.ContainsRune(`/\:*?"<>|`, r):
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		case r < 0x20 || r == 0x7f:
			continue
		default:
			b.WriteRune(r)
			lastDash = false
		}
	}
	s := strings.Trim(b.String(), "-.")
	if s == "" {
		return "sheet"
	}
	if runes := []rune(s); len(runes) > 80 {
		s = string(runes[:80])
	}
	return s
}
