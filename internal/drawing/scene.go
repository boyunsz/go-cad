package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
	"reflect"
	"sort"
	"strings"
)

type SourceRef struct {
	File     string          `json:"file,omitempty"`
	XrefPath []XrefPlacement `json:"xref_path,omitempty"`
	Handle   uint64          `json:"handle"`
	Type     string          `json:"type"`
	Layer    uint64          `json:"layer"`
	Space    string          `json:"space"`
	Inserts  []uint64        `json:"insert_path,omitempty"`
	Matrix   [6]float64      `json:"transform"`
	Spatial  [12]float64     `json:"transform_3d"`
}
type Diagnostic struct {
	Source SourceRef `json:"source"`
	Status string    `json:"status"`
	Detail string    `json:"detail"`
}
type ResolvedStyle struct {
	LineMM float64
	Dash   []float64
	Hidden bool
	NoPlot bool
}
type FillGeometry struct {
	Loops      [][]entity.Point2
	Hatch      *entity.EntHatch
	Transform  Xform
	LocalLoops [][]entity.Point2
}
type RayGeometry struct {
	Origin, Direction entity.Point2
	Both              bool
}
type Scene struct {
	Primitives     []Primitive
	Diagnostics    []Diagnostic
	SourceEntities int
	SourceObjects  int
}

// SelectedModelSpace 合并模型空间实体与具名模型块内容，按非零句柄去重并应用源绘制顺序。
// 不把其他块按大小或位置推测为模型空间。
func (d *Document) SelectedModelSpace() []any {
	// Only named model-space blocks are eligible. Never select the largest block.
	out := append([]any(nil), d.ModelSpace...)
	seen := map[uint64]bool{}
	for _, e := range out {
		if c, ok := e.(entity.EntityCommon); ok && c.Common().Handle != 0 {
			seen[c.Common().Handle] = true
		}
	}
	var hs []uint64
	for h, g := range d.InternalObjs {
		if g.Name == "BLOCK_HEADER" {
			n, _ := g.Field("name").(string)
			if strings.EqualFold(n, "*Model_Space") || strings.EqualFold(n, "$MODEL_SPACE") {
				hs = append(hs, h)
			}
		}
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i] < hs[j] })
	for _, h := range hs {
		for _, e := range d.Blocks[h] {
			c, ok := e.(entity.EntityCommon)
			if !ok {
				continue
			}
			id := c.Common().Handle
			if id == 0 || !seen[id] {
				out = append(out, e)
				seen[id] = true
			}
		}
	}
	return d.OrderedEntities(out)
}

// BuildScene 按显示可见性展开模型空间并汇总诊断，tolerance 使用图纸单位。
func BuildScene(d *Document, tolerance float64) Scene { return buildScene(d, tolerance, false) }

// BuildPlotScene 按打印可见性展开模型空间并汇总诊断，tolerance 使用图纸单位。
func BuildPlotScene(d *Document, tolerance float64) Scene { return buildScene(d, tolerance, true) }

// buildScene 初始化 faithful 展开器和顶点索引，展开选定模型实体并生成场景诊断。
func buildScene(d *Document, tolerance float64, plot bool) Scene {
	ts := NewTessellator(d)
	ts.Faithful = true
	ts.Plot = plot
	ts.Tolerance = tolerance
	ts.BuildVertexIndex()
	var out []Primitive
	for _, e := range d.SelectedModelSpace() {
		out = ts.AppendEntity(out, e, IdentityXform(), 0)
	}
	return finishScene(d, out, ts.Diagnostics)
}

// finishScene 汇总展开结果和解析失败，为源记录补充结构性或未实例化的处置说明。
// 诊断按源文件、句柄和状态排序，便于重复导出比较。
func finishScene(d *Document, out []Primitive, diagnostics []Diagnostic) Scene {
	sc := Scene{Primitives: out, Diagnostics: diagnostics, SourceEntities: len(d.ByHandle), SourceObjects: len(d.InternalObjs)}
	for h, msg := range d.ObjectFailures {
		sc.Diagnostics = append(sc.Diagnostics, Diagnostic{Source: SourceRef{File: d.SourceFile, Handle: h}, Status: "decode_failed", Detail: msg})
	}
	for h, msg := range d.DebugFailures() {
		sc.Diagnostics = append(sc.Diagnostics, Diagnostic{Source: SourceRef{File: d.SourceFile, Handle: h}, Status: "decode_failed", Detail: msg})
	}
	seen := map[uint64]bool{}
	for _, v := range sc.Diagnostics {
		if v.Source.File == d.SourceFile {
			seen[v.Source.Handle] = true
		}
	}
	for _, p := range out {
		if p.Source.File != d.SourceFile {
			continue
		}
		if poly, ok := d.ByHandle[p.Source.Handle].(*entity.EntPolyline2d); ok {
			for _, h := range poly.OwnedHandles {
				if !seen[h] {
					sc.Diagnostics = append(sc.Diagnostics, Diagnostic{Source: SourceRef{File: d.SourceFile, Handle: h, Type: "VERTEX_2D", Space: "model"}, Status: "structural", Detail: fmt.Sprintf("geometry consumed by POLYLINE_2D %X", poly.Handle)})
					seen[h] = true
				}
			}
		}
	}
	for h, e := range d.ByHandle {
		if !seen[h] {
			if c, ok := e.(entity.EntityCommon); ok {
				b := c.Common()
				reason := "definition or owned child not reached from selected space/viewport"
				space := "definition"
				if b.Mode == 1 {
					reason = "paper-space record not instantiated in selected view"
					space = "paper"
				}
				sc.Diagnostics = append(sc.Diagnostics, Diagnostic{Source: SourceRef{File: d.SourceFile, Handle: h, Type: b.TypeName, Layer: b.Layer, Space: space}, Status: "not_instantiated", Detail: reason})
			}
		}
	}
	sort.SliceStable(sc.Diagnostics, func(i, j int) bool {
		a, b := sc.Diagnostics[i], sc.Diagnostics[j]
		if a.Source.File != b.Source.File {
			return a.Source.File < b.Source.File
		}
		if a.Source.Handle != b.Source.Handle {
			return a.Source.Handle < b.Source.Handle
		}
		return a.Status < b.Status
	})
	return sc
}

// AppendEntity 在变换 t 下追加实体及其嵌套实例，并记录来源、可见性和处理状态。
// depth 表示当前嵌套深度；展开会消耗预算并更新诊断，非 faithful 模式转交预览路径。
func (ts *Tessellator) AppendEntity(out []Primitive, ent any, t Xform, depth int) []Primitive {
	if !ts.Faithful {
		return ts.appendEntity(out, ent, t, depth)
	}
	ec, ok := ent.(entity.EntityCommon)
	if !ok {
		return out
	}
	b := ec.Common()
	kind := b.TypeName
	if kind == "" {
		kind = strings.TrimPrefix(fmt.Sprintf("%T", ent), "*entity.Ent")
	}
	ref := SourceRef{File: ts.Doc.SourceFile, XrefPath: append([]XrefPlacement(nil), ts.XrefPath...), Handle: b.Handle, Type: kind, Layer: b.Layer, Space: "model", Inserts: append([]uint64(nil), ts.instancePath...), Matrix: t.Matrix(), Spatial: t.SpatialMatrix()}
	if ts.Space != "" {
		ref.Space = ts.Space
	}
	note := func(status, detail string) { ts.Diagnostics = append(ts.Diagnostics, Diagnostic{ref, status, detail}) }
	if !entity.EntityGeometryFinite(ent) {
		note("invalid_geometry", "nonfinite or invalid source geometry")
		return out
	}
	for _, v := range t.Matrix() {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			note("invalid_geometry", "nonfinite instance transform")
			return out
		}
	}
	if depth > ts.maxDepth || ts.Budget <= 0 {
		note("budget_exceeded", "block expansion limit")
		return out
	}
	if a, ok := ent.(*entity.EntAttrib); ok {
		if attributeFlags(a)&1 != 0 {
			note("hidden", "attribute has invisible flag")
			return out
		}
		if a.TypeName == "ATTDEF" && len(ts.insPath) > 0 && attributeFlags(a)&2 == 0 {
			note("structural", "variable attribute definition; value is supplied by INSERT-owned ATTRIB")
			return out
		}
	}
	// Non-planar OCS must be reported instead of claiming a faithful projection.
	rv := reflect.ValueOf(ent)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		f := rv.Elem().FieldByName("Extrusion")
		if f.IsValid() && f.CanInterface() {
			if n, ok := f.Interface().(entity.Point3); ok && (n.X != 0 || n.Y != 0 || (n.Z != 0 && n.Z != 1)) {
				if spatialSupported(ent) {
					// Full spatial composition now retains nested block depth.
				} else {
					note("approximated", "non-default extrusion projected to XY; full OCS projection not implemented")
				}
			}
		}
	}
	t = entityPlane(ent, t)
	ref.Matrix = t.Matrix()
	ref.Spatial = t.SpatialMatrix()
	prev := ts.parentStyle
	prevColor := ts.parentColor
	prevLayer := ts.parentLayer
	style, col, layer := ts.resolveStyle(b)
	if ts.Plot && style.NoPlot {
		note("non_plot", "effective layer is non-printing")
		return out
	}
	if style.Hidden || ts.FrozenLayers[layer] {
		status, reason := "hidden", "entity invisible flag"
		lc := ts.Doc.LayerColors[layer]
		if lc.Off {
			status, reason = "layer_off", "effective layer is off"
		} else if lc.Frozen || ts.FrozenLayers[layer] {
			status, reason = "frozen", "effective layer is frozen globally or in viewport"
		}
		note(status, reason)
		return out
	}
	ts.parentStyle, ts.parentColor, ts.parentLayer = style, col, layer
	defer func() { ts.parentStyle, ts.parentColor, ts.parentLayer = prev, prevColor, prevLayer }()
	start := len(out)
	if e, yes := ent.(*entity.EntInsert); yes {
		if resource := ts.Doc.Xrefs[e.BlockHeader]; resource != nil {
			return ts.appendExternal(out, e, t, depth, ref, resource)
		}
		if header := ts.Doc.InternalObjs[e.BlockHeader]; header != nil {
			path, _ := header.Field("xref_pname").(string)
			if path != "" {
				cached := false
				for _, child := range ts.Doc.Blocks[e.BlockHeader] {
					if _, marker := child.(*entity.EntBlockLike); !marker {
						cached = true
						break
					}
				}
				if !cached {
					note("missing_resource", fmt.Sprintf("external reference %q has no cached geometry; requires source DWG %q", header.Field("name"), path))
					return out
				}
			}
		}
		for _, h := range ts.insPath {
			if h == e.BlockHeader {
				note("cycle", "cyclic block reference")
				return out
			}
		}
		if len(ts.Doc.Blocks[e.BlockHeader]) == 0 {
			note("unsupported", "empty/missing block or external reference")
		}
		ts.instancePath = append(ts.instancePath, e.Handle)
		defer func() { ts.instancePath = ts.instancePath[:len(ts.instancePath)-1] }()
		out = ts.appendEntity(out, ent, t, depth)
		note("expanded", fmt.Sprintf("%d primitives", len(out)-start))
		return out
	}
	if dim, yes := ent.(*entity.EntDimension); yes {
		ts.instancePath = append(ts.instancePath, dim.Handle)
		defer func() { ts.instancePath = ts.instancePath[:len(ts.instancePath)-1] }()
		if len(ts.Doc.Blocks[dim.AnonymousBlock]) == 0 {
			note("approximated", "dimension anonymous block unavailable; simplified fallback")
		}
		out = ts.appendEntity(out, ent, t, depth)
		for i := start; i < len(out); i++ {
			if out[i].Source.Type == "" {
				out[i].Source = ref
				if ts.Doc.SourceFile != "" {
					out[i].SourceDocument = ts.Doc
				}
				out[i].Style = style
				out[i].Color = col
				out[i].Layer = layer
			}
		}
		note("expanded", fmt.Sprintf("%d primitives", len(out)-start))
		return out
	}
	out = ts.appendEntity(out, ent, t, depth)
	if h, yes := ent.(*entity.EntHatch); yes {
		out = out[:start]
		var loops, localLoops [][]entity.Point2
		for _, p := range h.Paths {
			pts, warnings := ts.HatchBoundary(p, t)
			localLoops = append(localLoops, pts)
			for _, w := range warnings {
				note("approximated", w)
			}
			var loop []entity.Point2
			for _, q := range pts {
				loop = append(loop, t.Apply(q))
			}
			if len(loop) > 2 {
				loops = append(loops, loop)
			}
		}
		if len(loops) > 0 {
			out = append(out, Primitive{Kind: 2, Kind0: "HATCH", Fill: &FillGeometry{Loops: loops, Hatch: h, Transform: t, LocalLoops: localLoops}})
		}
		if h.IsGradientFill != 0 {
			note("approximated", "gradient hatch uses solid color")
		}
	}
	if s, yes := ent.(*entity.EntSolid); yes {
		loop := []entity.Point2{t.Apply(s.P1), t.Apply(s.P2), t.Apply(s.P4), t.Apply(s.P3)}
		out = out[:start]
		out = append(out, Primitive{Kind: 2, Kind0: "SOLID", Fill: &FillGeometry{Loops: [][]entity.Point2{loop}, Transform: t}})
	}
	if r, yes := ent.(*entity.EntRay); yes {
		origin := t.Project(r.Start)
		p := t.Project(entity.Point3{X: r.Start.X + r.UnitVector.X, Y: r.Start.Y + r.UnitVector.Y, Z: r.Start.Z + r.UnitVector.Z})
		out = out[:start]
		out = append(out, Primitive{Kind: 3, Kind0: "RAY", Ray: &RayGeometry{Origin: origin, Direction: entity.Point2{X: p.X - origin.X, Y: p.Y - origin.Y}, Both: r.Xline}})
	}
	for i := start; i < len(out); i++ {
		p := &out[i]
		p.Source = ref
		if ts.Doc.SourceFile != "" {
			p.SourceDocument = ts.Doc
		}
		p.Style = style
		p.Color = col
		p.Layer = layer
		if p.Kind == 0 {
			if g := ts.entityGeometry(ent, t); g != nil {
				p.Geometry = g
				p.Strokes = g.Strokes(ts.Tolerance)
			}
		}
		if p.Lb.Tx != nil {
			ts.textStyle(ent, p)
		}
	}
	if leader, ok := ent.(*entity.EntLeader); ok {
		out = ts.appendLeaderArrow(out, leader, t, depth, ref)
	}
	if img, ok := ent.(*entity.EntImage); ok {
		if img.ImageDef == 0 {
			note("missing_resource", "IMAGE stores a null IMAGEDEF pointer; source pixels cannot be identified; no guessed association")
			return out
		}
		path := ""
		if obj := ts.Doc.InternalObjs[img.ImageDef]; obj != nil {
			path, _ = obj.Field("file_path").(string)
		}
		if path == "" {
			note("missing_resource", fmt.Sprintf("IMAGEDEF %X missing or has no path", img.ImageDef))
			return out
		}
		if img.DisplayProps&1 == 0 {
			note("hidden", "image display is disabled")
			return out
		}
		out = append(out, Primitive{Kind: 4, Kind0: "IMAGE", Source: ref, Style: style, Color: col, Layer: layer, Raster: &RasterGeometry{Image: img, Transform: t, Path: path}})
		if ts.Doc.SourceFile != "" {
			out[len(out)-1].SourceDocument = ts.Doc
		}
		note("resource_pending", "image definition resolved; pixel resource must be loaded before rendering")
		return out
	}

	if len(out) == start {
		switch ent.(type) {
		case *entity.EntBlockLike, *entity.EntVertex2d, *entity.EntVertex3d:
			note("structural", "non-drawable structural entity")
		default:
			note("unsupported", "no drawable representation")
		}
	} else {
		note("rendered", fmt.Sprintf("%d primitives", len(out)-start))
	}
	return out
}

// blockTransform 把 INSERT 的变换和块基点偏移组合到父变换。
// faithful 模式保留完整空间变换，预览模式使用既有二维插入变换。
func (ts *Tessellator) blockTransform(e *entity.EntInsert, t Xform) Xform {
	if !ts.Faithful {
		return t.Compose(InsertXform(e))
	}
	child := insertSpatial(e)
	if g := ts.Doc.InternalObjs[e.BlockHeader]; g != nil {
		if p, ok := g.Field("base_pt").([]float64); ok && len(p) >= 2 {
			z := 0.0
			if len(p) > 2 {
				z = p[2]
			}
			child = child.Compose(translation3(-p[0], -p[1], -z))
		}
	}
	return t.Compose(child)
}

// finitePoint 检查二维点的两个坐标均为有限数值。
func finitePoint(p entity.Point2) bool {
	return !math.IsNaN(p.X) && !math.IsNaN(p.Y) && !math.IsInf(p.X, 0) && !math.IsInf(p.Y, 0)
}

// insertPlaneProjection 按任意轴算法投影 INSERT 的对象 XY 平面。
// 仅平面块内容可由此二维变换准确表示，非平面深度需要完整空间变换路径。
func insertPlaneProjection(n entity.Point3, z float64) Xform {
	l := math.Sqrt(n.X*n.X + n.Y*n.Y + n.Z*n.Z)
	if l == 0 {
		n = entity.Point3{Z: 1}
	} else {
		n.X /= l
		n.Y /= l
		n.Z /= l
	}
	a := entity.Point3{X: -n.Y, Y: n.X}
	if math.Abs(n.X) < 1.0/64 && math.Abs(n.Y) < 1.0/64 {
		a = entity.Point3{X: n.Z, Z: -n.X}
	}
	l = math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z)
	a.X /= l
	a.Y /= l
	a.Z /= l
	b := entity.Point3{X: n.Y*a.Z - n.Z*a.Y, Y: n.Z*a.X - n.X*a.Z, Z: n.X*a.Y - n.Y*a.X}
	return MatrixXform([6]float64{a.X, a.Y, b.X, b.Y, n.X * z, n.Y * z})
}

type RasterGeometry struct {
	Image     *entity.EntImage
	Transform Xform
	Path      string
}
