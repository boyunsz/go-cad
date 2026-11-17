package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	cad "github.com/boyunsz/go-cad"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type sourceRow struct {
	Handle uint64
	Type   string
	Owner  uint64
	Mode   uint8
}

// inventory 按句柄排序导出实体的类型、归属和空间模式，供覆盖率诊断使用。
// 通过反射读取公共嵌入字段，跳过空指针和非指针值。
func inventory(doc *cad.Document) []sourceRow {
	// The facade exposes entity values as any; reflect only the shared embedded fields.
	var out []sourceRow
	for h, e := range doc.ByHandle {
		v := reflect.ValueOf(e)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			continue
		}
		v = v.Elem()
		get := func(n string) uint64 {
			f := v.FieldByName(n)
			if f.IsValid() && f.CanUint() {
				return f.Uint()
			}
			return 0
		}
		name := v.Type().Name()
		if f := v.FieldByName("TypeName"); f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
			name = f.String()
		}
		out = append(out, sourceRow{h, name, get("Owner"), uint8(get("Mode"))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}

// runFaithful 执行 DWG 读取、布局展开、SVG/PNG 输出及来源和覆盖率诊断。
// view 可指定图纸单位边界；layout=all 递归输出各布局，renderOnly 跳过 DXF 往返校验。
// 输入、渲染或产物写入失败时返回错误，已经写出的产物不作回滚。
func runFaithful(input, out string, width int, withSVG bool, style string, lineScale float64, renderOnly bool, opts cad.RenderOptions, view string) error {
	started := time.Now()
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	doc, err := cad.Parse(data)
	if err != nil {
		return err
	}
	if view != "" {
		vs := strings.Split(view, ",")
		if len(vs) != 4 {
			return fmt.Errorf("view 需要 4 个坐标")
		}
		var n [4]float64
		for i, s := range vs {
			n[i], err = strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return err
			}
		}
		opts.ViewBox = &cad.Box2{MinX: n[0], MinY: n[1], MaxX: n[2], MaxY: n[3]}
		opts.Bounds = "explicit"
	}
	if opts.Layout == "all" {
		modelOpts := opts
		modelOpts.Layout = "model"
		if e := runFaithful(input, out, width, withSVG, style, lineScale, renderOnly, modelOpts, view); e != nil {
			return e
		}
		for _, layout := range doc.Layouts() {
			if layout.Model {
				continue
			}
			lo := opts
			lo.Layout = layout.Name
			name := fmt.Sprintf("%X-", layout.Handle) + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(layout.Name)
			if e := runFaithful(input, filepath.Join(out, "layouts", name), width, withSVG, style, lineScale, true, lo, view); e != nil {
				return e
			}
		}
		return nil
	}
	opts.SourceDir = filepath.Dir(input)
	opts.Width = width
	opts.LineScale = lineScale
	opts.Fidelity = "faithful"
	prepared, err := cad.PrepareDrawing(doc, opts)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	hashes := map[string]string{}
	save := func(name string, b []byte) error {
		if err := atomicWrite(filepath.Join(out, name), b); err != nil {
			return err
		}
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(b))
		fmt.Printf("写出 %s (%d 字节)\n", filepath.Join(out, name), len(b))
		return nil
	}
	saveJSON := func(name string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		return save(name, b)
	}
	styles := []string{style}
	if style == "both" {
		styles = []string{"original", "soft"}
	}
	for _, s := range styles {
		suffix := ".faithful"
		prepared.Options.SoftPalette = s == "soft"
		prepared.Options.Background = color.RGBA{255, 255, 255, 255}
		prepared.Options.LineScale = lineScale
		if s == "clear" {
			suffix += "-clear"
		}
		if s == "soft" {
			suffix += "-soft"
			prepared.Options.Background = color.RGBA{250, 250, 248, 255}
			if lineScale == 0 {
				prepared.Options.LineScale = .8
			}
		}
		base := filepath.Base(input) + suffix
		if withSVG {
			svg, e := prepared.SVG()
			if e != nil {
				return e
			}
			if !utf8.Valid(svg) || bytes.Contains(svg, []byte("\uFFFD")) {
				return fmt.Errorf("SVG contains invalid/replacement text")
			}
			if e = save(base+".svg", svg); e != nil {
				return e
			}
		}
		image, e := prepared.PNG()
		if e != nil {
			return e
		}
		if _, e = png.Decode(bytes.NewReader(image)); e != nil {
			return e
		}
		if e = save(base+".png", image); e != nil {
			return e
		}
	}
	if err = saveJSON("render-audit.json", prepared.Audit); err != nil {
		return err
	}
	attributes := doc.AttributeRecords()
	var revisions []any
	for _, a := range attributes {
		if strings.Contains(a.Tag, "更改") || a.Tag == "版本" {
			var refs []any
			for _, v := range prepared.Primitives {
				if v.Source.File == "" && v.Source.Handle == a.Handle {
					refs = append(refs, v.Source)
				}
			}
			revisions = append(revisions, map[string]any{"source": a, "rendered_instances": refs})
		}
	}
	if err = saveJSON("attribute-audit.json", attributes); err != nil {
		return err
	}
	if err = saveJSON("revision-log-audit.json", map[string]any{"selected_space": prepared.Audit.SelectedSpace, "records": revisions, "note": "values are decoded source strings; empty and dash remain unchanged; no revision content is synthesized"}); err != nil {
		return err
	}
	var textRows []any
	for _, p := range prepared.Primitives {
		if p.Kind == 1 && p.Lb.Tx != nil {
			textRows = append(textRows, map[string]any{"source": p.Source, "raw": p.Lb.Tx.Raw, "display": p.Lb.Text, "style_handle": p.Lb.Tx.StyleHandle})
		}
	}
	if err = saveJSON("text-font-audit.json", map[string]any{"fonts": prepared.Audit.Fonts, "texts": textRows}); err != nil {
		return err
	}
	var resources []any
	for h, g := range doc.InternalObjects() {
		if g.Name == "IMAGEDEF" || g.Name == "BLOCK_HEADER" {
			path, _ := g.Field("file_path").(string)
			if g.Name == "BLOCK_HEADER" {
				path, _ = g.Field("xref_pname").(string)
			}
			if path != "" {
				resources = append(resources, map[string]any{"handle": h, "type": g.Name, "declared_path": path, "status": map[bool]string{true: "declared xref path; cached block entities may be rendered; external file is not reloaded", false: "definition path only; actual IMAGE association and pixel loading are reported per instance in render-audit.json"}[g.Name == "BLOCK_HEADER"]})
			}
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].(map[string]any)["handle"].(uint64) < resources[j].(map[string]any)["handle"].(uint64)
	})
	for _, v := range resources {
		row := v.(map[string]any)
		if loaded := prepared.Doc.Xrefs[row["handle"].(uint64)]; loaded != nil {
			row["status"] = "loaded through explicit local DWG mapping; original INSERT transform applied"
			row["loaded_path"] = loaded.Path
			row["sha256"] = loaded.SHA256
		}
	}
	if err = saveJSON("loaded-xrefs.json", prepared.Audit.ExternalDrawings); err != nil {
		return err
	}
	if err = saveJSON("external-resources.json", resources); err != nil {
		return err
	}
	var instances []any
	for i, p := range prepared.Primitives {
		instances = append(instances, map[string]any{"index": i, "source": p.Source, "kind": p.Kind0, "text": p.Lb.Text, "anchor": []float64{p.Lb.X, p.Lb.Y}, "lineweight_mm": p.Style.LineMM, "dash": p.Style.Dash})
	}
	if err = saveJSON("entity-render-map.json", map[string]any{"sources": inventory(doc), "instances": instances}); err != nil {
		return err
	}

	type coverageRow struct{ SourceRecords, RenderedInstances, ExpandedInstances, StructuralRecords, NotInstantiated, Unsupported, Approximated, MissingResources int }
	coverage := map[string]*coverageRow{}
	row := func(k string) *coverageRow {
		if coverage[k] == nil {
			coverage[k] = &coverageRow{}
		}
		return coverage[k]
	}
	for _, r := range inventory(doc) {
		row(r.Type).SourceRecords++
	}
	for _, p := range prepared.Primitives {
		if p.Source.File != "" {
			continue
		}
		row(p.Source.Type).RenderedInstances++
	}
	for _, d := range prepared.Audit.Diagnostics {
		if d.Source.File != "" {
			continue
		}
		r := row(d.Source.Type)
		switch d.Status {
		case "expanded":
			r.ExpandedInstances++
		case "structural":
			r.StructuralRecords++
		case "not_instantiated":
			r.NotInstantiated++
		case "unsupported":
			r.Unsupported++
		case "missing_resource":
			r.MissingResources++
		case "approximated":
			r.Approximated++
		}
	}
	if err = saveJSON("coverage-report.json", map[string]any{"selected_space": prepared.Audit.SelectedSpace, "by_type": coverage, "external_drawings": prepared.Audit.ExternalDrawings, "note": "records and instances have different units; definitions may render repeatedly or remain unreferenced"}); err != nil {
		return err
	}
	var layouts []any
	for h, g := range doc.InternalObjects() {
		if g.Name == "LAYOUT" || g.Name == "VPORT" || g.Name == "BLOCK_CONTROL" {
			layouts = append(layouts, map[string]any{"handle": h, "type": g.Name, "fields": g.DebugFields(), "handles": g.Handles})
		}
	}
	sort.Slice(layouts, func(i, j int) bool {
		return layouts[i].(map[string]any)["handle"].(uint64) < layouts[j].(map[string]any)["handle"].(uint64)
	})
	if err = saveJSON("layout-inventory.json", layouts); err != nil {
		return err
	}
	if !renderOnly {
		var buf bytes.Buffer
		if err = cad.WriteDXF(doc, &buf); err != nil {
			return err
		}
		other, e := cad.ParseDXF(buf.Bytes())
		if e != nil {
			return e
		}
		if err = save("roundtrip.dxf", buf.Bytes()); err != nil {
			return err
		}
		before, after := inventory(doc), inventory(other)
		am := map[uint64]sourceRow{}
		for _, r := range after {
			am[r.Handle] = r
		}
		var missing, changed, added []sourceRow
		var changedDetails []any
		counts := map[string]int{}
		for _, r := range before {
			if v, ok := am[r.Handle]; !ok {
				missing = append(missing, r)
				counts[r.Type]++
			} else if v.Type != r.Type || v.Owner != r.Owner || v.Mode != r.Mode {
				changed = append(changed, r)
				changedDetails = append(changedDetails, map[string]any{"before": r, "after": v})
			}
		}
		bm := map[uint64]bool{}
		for _, r := range before {
			bm[r.Handle] = true
		}
		for _, r := range after {
			if !bm[r.Handle] {
				added = append(added, r)
			}
		}
		if err = saveJSON("dxf-diff.json", map[string]any{"source_entity_count": doc.EntityCount(), "roundtrip_entity_count": other.EntityCount(), "source_records": len(before), "roundtrip_records": len(after), "added": added, "missing_by_type": counts, "missing": missing, "changed_type_owner_or_space": changed, "changed_details": changedDetails, "note": "counts include structural records and definitions; this is not a visual loss count"}); err != nil {
			return err
		}
	}
	var validation strings.Builder
	fmt.Fprintf(&validation, "Input: %s\nSHA-256: %x\nEntityCount: %d\nPrimitives: %d\nGlyphs: %d\nPNG: %dx%d\nBounds: %+v\nCurve tolerance: %g drawing units\nElapsed: %v\n", input, sha256.Sum256(data), doc.EntityCount(), len(prepared.Primitives), prepared.Audit.Glyphs, prepared.Audit.Width, prepared.Audit.Height, prepared.Audit.Bounds, prepared.Audit.CurveTolerance, time.Since(started))
	fmt.Fprintf(&validation, "Args: %q\n", os.Args)
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&validation, "%s  %s\n", hashes[k], k)
	}
	if err = save("validation.txt", []byte(validation.String())); err != nil {
		return err
	}
	if err = saveJSON("source-provenance.json", sourceProvenance(input, data, prepared, hashes)); err != nil {
		return err
	}
	status := map[string]int{}
	for _, d := range prepared.Audit.Diagnostics {
		status[d.Status]++
	}
	fmt.Printf("完成：实体=%d 图元=%d 字形=%d PNG=%dx%d 状态=%v 耗时=%v\n", doc.EntityCount(), len(prepared.Primitives), prepared.Audit.Glyphs, prepared.Audit.Width, prepared.Audit.Height, status, time.Since(started).Round(time.Millisecond))
	return nil
}
