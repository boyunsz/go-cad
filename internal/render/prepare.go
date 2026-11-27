package render

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"image/color"
	"math"
	"path/filepath"
	"sort"
)

type RenderAudit struct {
	ExternalDrawings []XrefUse
	Plot             bool
	Monochrome       bool
	SelectedSpace    string
	Layouts          []drawing.LayoutInfo
	FillMode         string
	HeaderStyle      map[string]any
	LinetypeScale    float64
	Objects          map[string]int

	Version                                           string `json:"version"`
	SourceEntities, SourceObjects, Primitives, Glyphs int
	Bounds                                            drawing.Box2
	Width, Height                                     int
	CurveTolerance                                    float64
	Diagnostics                                       []drawing.Diagnostic
	Fonts                                             []FontUse
	Limitations                                       []string
}
type PreparedDrawing struct {
	Images     map[int]*preparedImage
	Doc        *drawing.Document
	Options    RenderOptions
	Primitives []drawing.Primitive
	Texts      map[int]textLayoutPlan
	Audit      RenderAudit
}

// emptyBounds 创建尚未包含任何点的无效包围盒，供后续增量扩展。
func emptyBounds() drawing.Box2 {
	return drawing.Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
}

// extendBox 把有效包围盒 b 合并到 dst；忽略无效边界。
func extendBox(dst *drawing.Box2, b drawing.Box2) {
	if !b.Invalid() && b.MinX <= b.MaxX && b.MinY <= b.MaxY && finite(b.MinX) && finite(b.MaxX) && finite(b.MinY) && finite(b.MaxY) {
		dst.Extend(b.MinX, b.MinY)
		dst.Extend(b.MaxX, b.MaxY)
	}
}

// finite 判断浮点数是否既不是 NaN，也不是正负无穷。
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// PrepareDrawing 校验选项并准备模型或布局的几何、字体、图片及共享视口。
// 参数或外参加载错误直接返回；可恢复的显示限制保存在返回值的 Audit 中。
func PrepareDrawing(doc *drawing.Document, opts RenderOptions) (*PreparedDrawing, error) {
	return prepareDrawing(doc, opts, false)
}

// prepareDrawing 执行场景展开、文字排版、填充处理和边界计算，建立可重复输出的渲染计划。
// refined 标记是否已按像素尺度细化过曲线容差，避免重复细化递归。
func prepareDrawing(doc *drawing.Document, opts RenderOptions, refined bool) (*PreparedDrawing, error) {
	if doc == nil {
		return nil, fmt.Errorf("cad: nil document")
	}
	var xrefErr error
	doc, xrefErr = bindExternalDrawings(doc, opts.XrefFiles, opts.XrefLoader)
	if xrefErr != nil {
		return nil, xrefErr
	}
	if opts.Width == 0 {
		opts.Width = 4096
	}
	if opts.Width < 1 || opts.Width > 16384 {
		return nil, fmt.Errorf("width must be 1..16384")
	}
	if opts.Bounds == "" {
		opts.Bounds = "full"
	}
	if opts.Bounds != "full" && opts.Bounds != "explicit" {
		return nil, fmt.Errorf("faithful bounds must be full or explicit")
	}
	if opts.FillMode == "" {
		opts.FillMode = "source"
	}
	if opts.FillMode != "source" && opts.FillMode != "outline" {
		return nil, fmt.Errorf("fill mode must be source or outline")
	}
	if opts.TextMode == "" {
		opts.TextMode = "path"
	}
	if opts.TextMode != "path" && opts.TextMode != "text" {
		return nil, fmt.Errorf("text mode must be path or text")
	}
	if opts.CurveTolerance == 0 {
		opts.CurveTolerance = .01
	}
	if !finite(opts.CurveTolerance) || opts.CurveTolerance <= 0 {
		return nil, fmt.Errorf("invalid curve tolerance")
	}
	if opts.UnitsPerMM == 0 {
		opts.UnitsPerMM = 1
	}
	if !finite(opts.UnitsPerMM) || opts.UnitsPerMM <= 0 {
		return nil, fmt.Errorf("units-per-mm must be positive")
	}
	if opts.Background == (color.RGBA{}) {
		opts.Background = color.RGBA{255, 255, 255, 255}
	}
	if !finite(opts.LineScale) || opts.LineScale < 0 {
		return nil, fmt.Errorf("invalid line scale")
	}
	var sc drawing.Scene
	if opts.Layout != "" && opts.Layout != "model" {
		var err error
		if opts.Plot {
			sc, err = drawing.BuildPlotLayoutScene(doc, opts.Layout, opts.CurveTolerance)
		} else {
			sc, err = drawing.BuildLayoutScene(doc, opts.Layout, opts.CurveTolerance)
		}
		if err != nil {
			return nil, err
		}
	} else {
		if opts.Plot {
			sc = drawing.BuildPlotScene(doc, opts.CurveTolerance)
		} else {
			sc = drawing.BuildScene(doc, opts.CurveTolerance)
		}
	}
	p := &PreparedDrawing{Doc: doc, Options: opts, Primitives: sc.Primitives, Texts: map[int]textLayoutPlan{}, Images: map[int]*preparedImage{}, Audit: RenderAudit{Version: doc.Version(), SourceEntities: sc.SourceEntities, SourceObjects: sc.SourceObjects, Diagnostics: sc.Diagnostics, CurveTolerance: opts.CurveTolerance}}
	p.Audit.FillMode = opts.FillMode
	p.Audit.ExternalDrawings = externalInventory(doc)
	p.Audit.Plot, p.Audit.Monochrome = opts.Plot, opts.Monochrome
	p.Audit.SelectedSpace = "model"
	if opts.Layout != "" {
		p.Audit.SelectedSpace = opts.Layout
	}
	p.Audit.Layouts = doc.Layouts()
	p.Audit.HeaderStyle = doc.HeaderVars
	p.Audit.LinetypeScale = doc.Ltscale
	p.Audit.Objects = map[string]int{}
	for _, obj := range doc.InternalObjs {
		p.Audit.Objects[obj.Name]++
	}
	p.Audit.Limitations = []string{"model and orthographic paper-layout viewports supported; CTB/STB plotting not reproduced", "decoded SORTENTSTABLE mappings applied within each containing space; unspecified order remains stable", "lineweights use explicit units-per-mm; default 1 is a display assumption, not detected DWG units", "SHX and bigfont outlines require substitution; substituted fonts do not reproduce original glyphs", "NURBS subdivision checks midpoint and quarter-points; arbitrary rational curves do not have a proven global error bound", "MTEXT advanced paragraph tabs/indents and complex-script shaping are not reproduced; inline A0/A1/A2 alignment is supported"}
	resolver := newFontResolver(opts)
	bbox := emptyBounds()
	maxLW := 0.0
	for i := range p.Primitives {
		e := &p.Primitives[i]
		previousBounds := bbox
		bbox = emptyBounds()
		if e.Raster != nil {
			imageOpts := opts
			if e.Source.File != "" {
				imageOpts.SourceDir = filepath.Dir(e.Source.File)
			}
			im, err := prepareImage(e.Raster, imageOpts)
			if err != nil {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "missing_resource", Detail: err.Error()})
			} else {
				p.Images[i] = im
				if e.Raster.Image.Brightness != 50 || e.Raster.Image.Contrast != 50 || e.Raster.Image.Fade != 0 {
					p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "approximated", Detail: "raster brightness/contrast/fade adjustments not reproduced; original pixels embedded"})
				}
				extendBox(&bbox, im.Bounds)
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "rendered", Detail: "embedded local raster: " + im.Path})
			}
		}
		if e.Geometry != nil {
			extendBox(&bbox, e.Geometry.Bounds())
			for _, w := range e.Geometry.Warnings {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "approximated", Detail: w})
			}
		}
		if e.Kind == 1 {
			layout, fonts := layoutFaithfulText(*e, resolver)
			p.Texts[i] = layout
			p.Audit.Fonts = append(p.Audit.Fonts, fonts...)
			p.Audit.Glyphs += len(layout.Glyphs)
			extendBox(&bbox, layout.Bounds)
			if e.Lb.Tx != nil && (e.Lb.Tx.HAlign == 3 || e.Lb.Tx.HAlign == 5) && e.Lb.Tx.FitWidth <= 0 {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "approximated", Detail: "TEXT aligned/fit constraint not available; insertion alignment retained"})
			}
		}
		if e.Kind == 0 && e.Geometry == nil {
			for _, s := range e.Strokes {
				if !finite(s.X1) || !finite(s.Y1) || !finite(s.X2) || !finite(s.Y2) {
					p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "invalid_geometry", Detail: "nonfinite coordinates"})
					continue
				}
				bbox.Extend(s.X1, s.Y1)
				bbox.Extend(s.X2, s.Y2)
			}
		}
		if e.Fill != nil {
			for _, loop := range e.Fill.Loops {
				for _, q := range loop {
					if finite(q.X) && finite(q.Y) {
						bbox.Extend(q.X, q.Y)
					}
				}
			}
		}
		if e.Clip != nil {
			bbox.MinX = math.Max(bbox.MinX, e.Clip.MinX)
			bbox.MinY = math.Max(bbox.MinY, e.Clip.MinY)
			bbox.MaxX = math.Min(bbox.MaxX, e.Clip.MaxX)
			bbox.MaxY = math.Min(bbox.MaxY, e.Clip.MaxY)
		}
		extendBox(&previousBounds, bbox)
		bbox = previousBounds
		maxLW = math.Max(maxLW, e.Style.LineMM*opts.UnitsPerMM*renderLineScale(opts.LineScale))
	}
	if bbox.Invalid() || !finite(bbox.MinX) || !finite(bbox.MaxX) {
		bbox = drawing.Box2{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}
		p.Audit.Limitations = append(p.Audit.Limitations, "no finite geometry; default viewport used for infinite entities")
	}
	margin := math.Max(math.Max(bbox.MaxX-bbox.MinX, bbox.MaxY-bbox.MinY)*.02, maxLW)
	if margin == 0 {
		margin = 1
	}
	bbox.MinX -= margin
	bbox.MinY -= margin
	bbox.MaxX += margin
	bbox.MaxY += margin
	if opts.Bounds == "explicit" {
		if opts.ViewBox == nil || opts.ViewBox.Invalid() || !finite(opts.ViewBox.MinX) || !finite(opts.ViewBox.MinY) || !finite(opts.ViewBox.MaxX) || !finite(opts.ViewBox.MaxY) || opts.ViewBox.MaxX <= opts.ViewBox.MinX || opts.ViewBox.MaxY <= opts.ViewBox.MinY {
			return nil, fmt.Errorf("explicit bounds require valid view box")
		}
		bbox = *opts.ViewBox
	}
	widthWorld := bbox.MaxX - bbox.MinX
	heightWorld := bbox.MaxY - bbox.MinY
	if !finite(widthWorld) || !finite(heightWorld) || widthWorld <= 0 || heightWorld <= 0 {
		return nil, fmt.Errorf("invalid scene bounds")
	}
	height := int(math.Ceil(heightWorld / widthWorld * float64(opts.Width)))
	if height < 1 {
		height = 1
	}
	if height > 16384 || int64(height)*int64(opts.Width) > 64_000_000 {
		return nil, fmt.Errorf("raster exceeds 64M pixels or 16384px height; choose explicit viewport or smaller width")
	}
	p.Audit.Bounds = bbox
	p.Audit.Width = opts.Width
	p.Audit.Height = height
	tol := math.Min(opts.CurveTolerance, .25*widthWorld/float64(opts.Width))
	if tol < opts.CurveTolerance && !refined {
		opts.CurveTolerance = tol
		return prepareDrawing(doc, opts, true)
	}
	p.Audit.CurveTolerance = opts.CurveTolerance
	for i := range p.Primitives {
		e := &p.Primitives[i]
		if e.Geometry != nil {
			e.Strokes = e.Geometry.Strokes(tol)
		}
		if e.Ray != nil {
			rayBox := bbox
			if e.Clip != nil {
				rayBox = *e.Clip
			}
			e.Strokes = clipRay(*e.Ray, rayBox)
			e.Kind = 0
			if len(e.Strokes) == 0 {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "outside_view", Detail: "infinite entity outside viewport"})
			}
		}
		if e.Fill != nil && e.Fill.Hatch != nil && e.Fill.Hatch.SolidFill && opts.FillMode == "outline" && !dimensionOwned(primitiveDocument(doc, *e), e.Source) {
			e.Kind = 0
			e.Strokes = nil
			for _, loop := range e.Fill.Loops {
				for j, q := range loop {
					r := loop[(j+1)%len(loop)]
					e.Strokes = append(e.Strokes, drawing.Stroke{X1: q.X, Y1: q.Y, X2: r.X, Y2: r.Y})
				}
			}
			e.Style.Dash = nil
			p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "display_override", Detail: "solid HATCH shown as boundary contours, per requested outline display; source fill data preserved"})
		}
		if e.Fill != nil && e.Fill.Hatch != nil && !e.Fill.Hatch.SolidFill {
			st, err := hatchStrokes(e.Fill, tol)
			e.Strokes = st
			if err != nil {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: e.Source, Status: "unsupported", Detail: err.Error()})
			}
			e.Kind = 0
		}
	}
	for h, lc := range doc.LayerColors {
		if !lc.StyleKnown {
			p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: drawing.SourceRef{Handle: h, Type: "LAYER"}, Status: "approximated", Detail: "layer visibility/linetype/lineweight metadata incomplete"})
		}
	}
	for h, g := range doc.InternalObjs {
		if g.Name == "LTYPE" {
			if c, _ := g.Field("complex").(bool); c {
				p.Audit.Diagnostics = append(p.Audit.Diagnostics, drawing.Diagnostic{Source: drawing.SourceRef{Handle: h, Type: "LTYPE"}, Status: "unsupported", Detail: "complex shape/text linetype rendered using its dash lengths"})
			}
		}
	}
	if doc.Ltscale <= 0 {
		p.Audit.Limitations = append(p.Audit.Limitations, "DWG header LTSCALE not decoded; global linetype scale defaults to 1")
	}
	p.Audit.Primitives = len(p.Primitives)
	sort.SliceStable(p.Audit.Diagnostics, func(i, j int) bool {
		a, b := p.Audit.Diagnostics[i], p.Audit.Diagnostics[j]
		if a.Source.File != b.Source.File {
			return a.Source.File < b.Source.File
		}
		if a.Source.Handle != b.Source.Handle {
			return a.Source.Handle < b.Source.Handle
		}
		return a.Status < b.Status
	})
	// Stable compact font inventory.
	seen := map[string]bool{}
	fonts := p.Audit.Fonts
	p.Audit.Fonts = nil
	for _, f := range fonts {
		k := fmt.Sprintf("%s|%X|%s|%s|%s|%s|%s|%t|%t", f.SourceFile, f.Handle, f.Source, f.Actual, f.Face.PostScript, f.Missing, f.UsedCharacters, f.RequestedBold, f.RequestedItalic)
		if !seen[k] {
			seen[k] = true
			p.Audit.Fonts = append(p.Audit.Fonts, f)
		}
	}
	if opts.FillMode == "outline" {
		p.Audit.Limitations = append(p.Audit.Limitations, "requested outline display: ordinary solid HATCH contours; dimension arrow HATCH and SOLID retain source fill")
	}
	if opts.TextMode == "text" {
		p.Audit.Limitations = append(p.Audit.Limitations, "SVG text mode depends on viewer font matching; path mode fixes chosen glyph outlines")
	}
	return p, nil
}

// clipRay 把射线或双向直线裁切到矩形 b，返回至多一条有限线段。
// 无交集、方向退化或交点参数非有限时返回 nil。
func clipRay(r drawing.RayGeometry, b drawing.Box2) []drawing.Stroke {
	lo, hi := 0.0, math.Inf(1)
	if r.Both {
		lo = math.Inf(-1)
	}
	for _, a := range [][4]float64{{r.Origin.X, r.Direction.X, b.MinX, b.MaxX}, {r.Origin.Y, r.Direction.Y, b.MinY, b.MaxY}} {
		if math.Abs(a[1]) < 1e-15 {
			if a[0] < a[2] || a[0] > a[3] {
				return nil
			}
			continue
		}
		x, y := (a[2]-a[0])/a[1], (a[3]-a[0])/a[1]
		if x > y {
			x, y = y, x
		}
		lo = math.Max(lo, x)
		hi = math.Min(hi, y)
	}
	if lo > hi || !finite(lo) || !finite(hi) {
		return nil
	}
	return []drawing.Stroke{{X1: r.Origin.X + lo*r.Direction.X, Y1: r.Origin.Y + lo*r.Direction.Y, X2: r.Origin.X + hi*r.Direction.X, Y2: r.Origin.Y + hi*r.Direction.Y}}
}

// strokeWidth 将图元的毫米线宽及显示倍率换算为图纸单位。
// 零宽或负宽按当前视口的一像素宽度回退，再应用线宽倍率。
func (p *PreparedDrawing) strokeWidth(e drawing.Primitive) float64 {
	w := e.Style.LineMM * p.Options.UnitsPerMM * renderLineScale(p.Options.LineScale)
	if w <= 0 {
		w = (p.Audit.Bounds.MaxX - p.Audit.Bounds.MinX) / float64(p.Audit.Width) * 1.0 * renderLineScale(p.Options.LineScale)
	}
	return w
}

// color 解析图元所属文档的颜色并应用显示配色；黑白模式固定返回不透明黑色。
func (p *PreparedDrawing) color(e drawing.Primitive) color.RGBA {
	if p.Options.Monochrome {
		return color.RGBA{0, 0, 0, 255}
	}
	return styledEntityColor(primitiveDocument(p.Doc, e), &e, p.Options.Background.R > 127, p.Options.SoftPalette)
}

// point 构造二维坐标值，用于几何与像素坐标换算。
func point(x, y float64) entity.Point2 { return entity.Point2{X: x, Y: y} }

// dimensionOwned 检查实例来源链是否包含 DIMENSION 或 LEADER，以识别应保留填充的标注图元。
func dimensionOwned(doc *drawing.Document, source drawing.SourceRef) bool {
	for _, h := range source.Inserts {
		switch doc.ByHandle[h].(type) {
		case *entity.EntDimension, *entity.EntLeader:
			return true
		}
	}
	return false
}
