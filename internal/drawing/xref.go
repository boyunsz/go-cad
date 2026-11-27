package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
	"sort"
	"strings"
)

type XrefDocument struct {
	Document     *Document
	Path, SHA256 string
}
type XrefPlacement struct {
	File          string     `json:"file,omitempty"`
	InsertHandle  uint64     `json:"insert_handle"`
	ParentInserts []uint64   `json:"parent_inserts,omitempty"`
	BlockHandle   uint64     `json:"block_handle"`
	Transform     [6]float64 `json:"transform"`
}

// appendExternal 在主图插入位置展开已加载外参的模型空间，并传播来源链及诊断。
// 通过渲染副本应用主图图层覆盖，隔离外参句柄命名空间；循环引用停止展开，
// 子实例与主图共享剩余展开预算，不修改外参源文档的图层。
func (ts *Tessellator) appendExternal(out []Primitive, ins *entity.EntInsert, t Xform, depth int, ref SourceRef, resource *XrefDocument) []Primitive {
	note := func(status, detail string) {
		ts.Diagnostics = append(ts.Diagnostics, Diagnostic{Source: ref, Status: status, Detail: detail})
	}
	if resource.Document.SourceFile == ts.Doc.SourceFile {
		note("cycle", "external DWG references itself")
		return out
	}
	for _, parent := range ts.XrefPath {
		if parent.File == resource.Path {
			note("cycle", "cyclic external DWG reference")
			return out
		}
	}
	doc := *resource.Document
	// Isolate every external handle namespace; only project host xref layer
	// overrides into a rendering copy. Never merge/remap entity handles.
	doc.LayerColors = map[uint64]LayerColor{}
	for h, l := range resource.Document.LayerColors {
		doc.LayerColors[h] = l
	}
	doc.InternalObjs = map[uint64]*object.ObjGeneric{}
	maxHandle := uint64(0)
	for h, g := range resource.Document.InternalObjs {
		doc.InternalObjs[h] = g
		if h > maxHandle {
			maxHandle = h
		}
	}
	for h := range resource.Document.ByHandle {
		if h > maxHandle {
			maxHandle = h
		}
	}
	doc.HeaderVars = map[string]any{}
	for k, v := range resource.Document.HeaderVars {
		doc.HeaderVars[k] = v
	}
	doc.Ltscale = ts.Doc.Ltscale
	doc.HeaderVars["PSLTSCALE"] = ts.Doc.HeaderVars["PSLTSCALE"]
	name, _ := ts.Doc.InternalObjs[ins.BlockHeader].Field("name").(string)
	name = expandSymbolNameEscapes(name)
	type hostLayer struct {
		handle uint64
		layer  LayerColor
	}
	hosts := map[string]hostLayer{}
	for h, l := range ts.Doc.LayerColors {
		hosts[strings.ToLower(l.DisplayName())] = hostLayer{h, l}
	}
	layers := make([]uint64, 0, len(doc.LayerColors))
	for h := range doc.LayerColors {
		layers = append(layers, h)
	}
	sort.Slice(layers, func(i, j int) bool { return layers[i] < layers[j] })
	zero := uint64(0)
	retain := true
	if v, ok := ts.Doc.HeaderVars["VISRETAIN"].(uint8); ok {
		retain = v != 0
	}
	for _, h := range layers {
		l := doc.LayerColors[h]
		if l.Name == "0" {
			zero = h
		}
		hl, found := hosts[strings.ToLower(name+"|"+l.DisplayName())]
		if !found && l.Name == "0" {
			hl = hostLayer{ts.parentLayer, ts.Doc.LayerColors[ts.parentLayer]}
			found = true
		}
		if found && (retain || l.Name == "0") {
			l = hl.layer
			l.Frozen = l.Frozen || ts.FrozenLayers[hl.handle]
			if lt := ts.Doc.InternalObjs[l.Ltype]; lt != nil {
				maxHandle++
				doc.InternalObjs[maxHandle] = lt
				l.Ltype = maxHandle
			}
			doc.LayerColors[h] = l
		}
	}
	child := NewTessellator(&doc)
	child.Faithful = true
	child.Plot = ts.Plot
	child.Tolerance = ts.Tolerance
	child.ViewportScale = ts.ViewportScale
	child.Space = ts.Space
	child.Budget = ts.Budget
	child.maxDepth = ts.maxDepth
	child.parentStyle = ts.parentStyle
	child.parentColor = ts.parentColor
	child.parentLayer = zero
	transform := ts.blockTransform(ins, t)
	child.XrefPath = append(append([]XrefPlacement(nil), ts.XrefPath...), XrefPlacement{File: ts.Doc.SourceFile, InsertHandle: ins.Handle, ParentInserts: append([]uint64(nil), ts.instancePath...), BlockHandle: ins.BlockHeader, Transform: transform.Matrix()})
	child.BuildVertexIndex()
	var external []Primitive
	for _, e := range doc.SelectedModelSpace() {
		external = child.AppendEntity(external, e, transform, depth+1)
	}
	ts.Budget = child.Budget
	sc := finishScene(&doc, external, child.Diagnostics)
	ts.Diagnostics = append(ts.Diagnostics, sc.Diagnostics...)
	note("xref_loaded", fmt.Sprintf("%d primitives from %s; SHA256 %s", len(external), resource.Path, resource.SHA256))
	return append(out, external...)
}
