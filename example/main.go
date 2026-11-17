// Command example 将输入图纸转换为 SVG、PNG，并输出转换诊断信息。
//
// 在本目录执行 go run . -f drawing.dwg -o output；其他选项见命令行帮助。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	cad "github.com/boyunsz/go-cad"
)

// main 解析命令行及本地资源映射，分派图纸渲染、已有 SVG 增强或合并任务。
// 参数错误以退出码 2 结束，执行错误以退出码 1 结束。
func main() {
	in := flag.String("f", "", "输入 DWG 文件（必填）")
	out := flag.String("o", ".", "输出目录")
	width := flag.Int("width", 4096, "PNG 宽度，1–16384")
	svg := flag.Bool("svg", true, "同时生成可缩放 SVG")
	combine := flag.Bool("combine-svg", false, "按 DWG 页签合并图纸布局（默认不附模型总览）")
	combineModel := flag.Bool("combine-model", false, "合并时额外附加模型总览；无图纸布局时自动使用模型")
	combineOnly := flag.Bool("combine-only", false, "仅合并 -o 目录中已生成的 SVG，不重新渲染（-f 用于读取布局顺序）")
	enhanceSVG := flag.String("enhance-svg", "", "增强 pdftocairo 导出的矢量 SVG（独立模式，不解析 DWG）")
	appendSVG := flag.String("append-svg", "", "在增强版后附加一张已生成的 DWG 模型 SVG")
	plot := flag.Bool("plot", false, "按 CAD 打印规则过滤辅助图层")
	style := flag.String("style", "soft", "显示样式：soft 柔和灰色 / original 原始颜色 / clear 黑白打印 / both 两版")
	lineScale := flag.Float64("line-scale", 0, "线宽倍率；0 使用样式默认值")
	renderOnly := flag.Bool("render-only", false, "跳过 DXF 回读；仍生成图片和审计")
	fidelity := flag.String("fidelity", "faithful", "渲染流程：faithful / preview")
	bounds := flag.String("bounds", "full", "取景：full / explicit")
	view := flag.String("view", "", "指定视口 xmin,ymin,xmax,ymax")
	fillMode := flag.String("fill-mode", "outline", "实心 HATCH 显示：outline 轮廓 / source 源填充；保留箭头与剖面线")
	textMode := flag.String("text-mode", "path", "文字：path 固定字形 / text 可编辑")
	tolerance := flag.Float64("curve-tolerance", .01, "曲线容差（图纸单位）")
	layout := flag.String("layout", "model", "空间：model 或 DWG 中的布局名称；all 分别输出所有布局")
	xrefMapFile := flag.String("xref-map", "", "外参映射 JSON：DWG 文件名或块名到可访问的 DWG 路径")
	fontMapFile := flag.String("font-map", "", "字体映射 JSON：源字体名到可访问的字体路径")
	resourceDir := flag.String("resource-dir", "", "外部位图目录；仅按有效 IMAGEDEF 的文件名匹配")
	fontDir := flag.String("font-dir", "", "字体目录")
	units := flag.Float64("units-per-mm", 1, "线宽毫米对应的图纸单位；默认仅为显示假设")
	flag.Parse()
	if *enhanceSVG != "" {
		if err := writeClaritySVG(*enhanceSVG, *appendSVG, *out, *width); err != nil {
			fmt.Fprintln(os.Stderr, "SVG 清晰增强失败:", err)
			os.Exit(1)
		}
		return
	}
	if *appendSVG != "" {
		fmt.Fprintln(os.Stderr, "append-svg 需要 enhance-svg")
		os.Exit(2)
	}
	if *combine || *combineOnly {
		if !*svg || *fidelity != "faithful" {
			fmt.Fprintln(os.Stderr, "combine-svg / combine-only 需要 faithful 渲染和 SVG 输出")
			os.Exit(2)
		}
		*layout = "all"
	}
	if *in == "" {
		fmt.Fprintln(os.Stderr, "请用 -f 指定 DWG 文件，例如 go run . -f drawing.dwg -o output。")
		os.Exit(2)
	}
	if *width < 1 || *width > 16384 {
		fmt.Fprintln(os.Stderr, "width 必须在 1–16384 之间")
		os.Exit(2)
	}
	if *style != "soft" && *style != "original" && *style != "both" && *style != "clear" {
		fmt.Fprintln(os.Stderr, "style 必须为 soft、original、clear 或 both")
		os.Exit(2)
	}
	if *combineOnly {
		if err := writeCombinedSVG(*in, *out, *style, *combineModel); err != nil {
			fmt.Fprintln(os.Stderr, "合并 SVG 失败:", err)
			os.Exit(1)
		}
		return
	}
	if math.IsNaN(*lineScale) || math.IsInf(*lineScale, 0) || *lineScale < 0 || *lineScale > 4 {
		fmt.Fprintln(os.Stderr, "line-scale 必须在 0–4 之间")
		os.Exit(2)
	}
	var fontMap map[string]string
	if *fontMapFile != "" {
		b, e := os.ReadFile(*fontMapFile)
		if e == nil {
			e = json.Unmarshal(b, &fontMap)
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, "字体映射读取失败:", e)
			os.Exit(2)
		}
		for name, path := range fontMap {
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(*fontMapFile), path)
				fontMap[name] = path
			}
			if _, e = os.Stat(strings.SplitN(path, "#", 2)[0]); e != nil {
				fmt.Fprintln(os.Stderr, "字体映射文件不存在:", path)
				os.Exit(2)
			}
		}
	}
	var xrefMap map[string]string
	if *xrefMapFile != "" {
		b, e := os.ReadFile(*xrefMapFile)
		if e == nil {
			e = json.Unmarshal(b, &xrefMap)
		}
		if e != nil {
			fmt.Fprintln(os.Stderr, "外参映射读取失败:", e)
			os.Exit(2)
		}
		for name, path := range xrefMap {
			if !filepath.IsAbs(path) {
				xrefMap[name] = filepath.Join(filepath.Dir(*xrefMapFile), path)
			}
		}
	}
	var err error
	if *fidelity == "faithful" {
		err = runFaithful(*in, *out, *width, *svg, *style, *lineScale, *renderOnly, cad.RenderOptions{XrefFiles: xrefMap, Plot: *plot, Monochrome: *style == "clear", ResourceDir: *resourceDir, FontMap: fontMap, Layout: *layout, FillMode: *fillMode, Bounds: *bounds, TextMode: *textMode, CurveTolerance: *tolerance, FontDir: *fontDir, UnitsPerMM: *units}, *view)
	} else if *fidelity == "preview" && *style != "both" && *style != "clear" && !*plot && len(xrefMap) == 0 {
		err = run(*in, *out, *width, *svg, *style, *lineScale, *renderOnly)
	} else {
		err = fmt.Errorf("fidelity 必须为 faithful 或 preview；plot/clear 需要 faithful，preview 使用单一样式")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "执行失败:", err)
		os.Exit(1)
	}
	if *combine {
		if err = writeCombinedSVG(*in, *out, *style, *combineModel); err != nil {
			fmt.Fprintln(os.Stderr, "合并 SVG 失败:", err)
			os.Exit(1)
		}
	}
}

// run 执行预览渲染流程，导出图片、文本和结构化记录，并可进行 DXF 往返检查。
// width 控制渲染宽度；文件读取、转换或写出失败时返回带上下文的错误。
func run(input, outDir string, width int, withSVG bool, style string, lineScale float64, renderOnly bool) error {
	started := time.Now()
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("读取输入: %w", err)
	}
	doc, err := cad.Parse(data)
	if err != nil {
		return fmt.Errorf("解析 DWG: %w", err)
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	// Keep original example outputs available for before/after comparison.
	base := filepath.Base(input) + ".fixed"
	visualBase := base
	opts := cad.RenderOptions{Width: width, LineScale: lineScale}
	if style == "soft" {
		visualBase = filepath.Base(input) + ".soft"
		opts.SoftPalette = true
		opts.Background = color.RGBA{250, 250, 248, 255}
		if lineScale == 0 {
			opts.LineScale = 0.8
		}
	}
	save := func(suffix string, payload []byte) (string, error) {
		name := base + suffix
		if suffix == ".png" || suffix == ".svg" {
			name = visualBase + suffix
		}
		path := filepath.Join(outDir, name)
		if err := atomicWrite(path, payload); err != nil {
			return "", err
		}
		fmt.Printf("写出 %s (%d 字节)\n", path, len(payload))
		return path, nil
	}
	fmt.Printf("解析: 版本=%s 实体=%d 跳过=%d 文本=%d\n", doc.Version(), doc.EntityCount(), doc.Skipped(), len(doc.Texts()))

	pngData, err := cad.RenderPNG(doc, opts)
	if err != nil {
		return fmt.Errorf("PNG 渲染: %w", err)
	}
	pngPath, err := save(".png", pngData)
	if err != nil {
		return err
	}
	f, err := os.Open(pngPath)
	if err != nil {
		return err
	}
	img, decodeErr := png.Decode(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return fmt.Errorf("PNG 文件回读: %w", decodeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Printf("PNG 校验: %dx%d\n", img.Bounds().Dx(), img.Bounds().Dy())
	if withSVG {
		svgData, err := cad.RenderSVG(doc, opts)
		if err != nil {
			return fmt.Errorf("SVG 渲染: %w", err)
		}
		if _, err = save(".svg", svgData); err != nil {
			return err
		}
	}

	if renderOnly {
		fmt.Printf("图片生成完成，样式=%s，耗时 %s\n", style, time.Since(started).Round(time.Millisecond))
		return nil
	}

	var dwg bytes.Buffer
	if err = cad.WriteDwg(doc, &dwg); err != nil {
		return fmt.Errorf("DWG 回写: %w", err)
	}
	dwgPath, err := save(".rewrite.dwg", dwg.Bytes())
	if err != nil {
		return err
	}
	rewritten, err := os.ReadFile(dwgPath)
	if err != nil {
		return err
	}
	doc2, err := cad.Parse(rewritten)
	if err != nil {
		return fmt.Errorf("DWG 文件回读: %w", err)
	}
	if doc2.EntityCount() != doc.EntityCount() {
		return fmt.Errorf("DWG 回读实体数不一致: %d → %d", doc.EntityCount(), doc2.EntityCount())
	}
	fmt.Printf("DWG 校验: 回读实体数=%d，与输入一致\n", doc2.EntityCount())

	var dxf bytes.Buffer
	if err = cad.WriteDXF(doc, &dxf); err != nil {
		return fmt.Errorf("DXF 写出: %w", err)
	}
	dxfPath, err := save(".dxf", dxf.Bytes())
	if err != nil {
		return err
	}
	dxfBytes, err := os.ReadFile(dxfPath)
	if err != nil {
		return err
	}
	dxfDoc, err := cad.ParseDXF(dxfBytes)
	if err != nil {
		return fmt.Errorf("DXF 文件回读: %w", err)
	}
	fmt.Printf("DXF 校验: 回读实体数=%d\n", dxfDoc.EntityCount())
	if dxfDoc.EntityCount() != doc.EntityCount() {
		fmt.Fprintf(os.Stderr, "注意: DXF 回读实体数 %d → %d；当前库的 DXF 转换不能视为无损。PNG/SVG 直接从原始 DWG 生成。\n", doc.EntityCount(), dxfDoc.EntityCount())
	}

	// Reuse the parsed document; avoid parsing the DWG a second time.
	entities, err := cad.DumpEntitiesDoc(doc)
	if err != nil {
		return fmt.Errorf("JSON 导出: %w", err)
	}
	if !json.Valid([]byte(entities)) {
		return fmt.Errorf("实体 JSON 格式无效")
	}
	if _, err = save(".entities.json", []byte(entities)); err != nil {
		return err
	}
	texts, err := json.MarshalIndent(doc.Texts(), "", "  ")
	if err != nil {
		return err
	}
	if _, err = save(".texts.json", texts); err != nil {
		return err
	}
	fmt.Printf("完成，耗时 %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

// atomicWrite 在目标目录创建临时文件，写入并关闭后通过重命名替换目标文件。
// 父目录必须存在；失败时清理临时文件，不保证断电后的磁盘持久性。
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".cad-output-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
