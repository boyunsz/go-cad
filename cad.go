// Package cad 提供 DWG、DXF 和 JSON 图纸的解析、文本提取、渲染与导出接口。
//
// 解析结果统一为 Document；PreparedDrawing 用于在 SVG 和 PNG 输出之间
// 复用几何、文字布局和视口。格式解析与编码由 internal 下的各子包实现。
package cad

import (
	"io"

	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/render"
	"github.com/boyunsz/go-cad/internal/writer"
)

// Document 解析完成的 DWG 文档模型（实现见 internal/drawing）。
type Document = drawing.Document

// TextInfo 提取的图纸文本及位置（世界坐标）。
type TextInfo = drawing.TextInfo

// DumpEntityRow 单个实体的对照字段（键名与参考实现对齐）。
type DumpEntityRow = drawing.DumpEntityRow

// Entity 对外暴露的图元接口（几何访问用于渲染与包围盒计算）。
// bounds 为非导出方法：仅本包模型可满足，外部只读消费。
type Entity interface {
	bounds() drawing.Box2
}

// RenderOptions 渲染选项（尺寸/背景等）。
type RenderOptions = render.RenderOptions

// Sheet 图纸分幅定义。
type Sheet = render.Sheet

// SheetResult 单图纸渲染产出。
type SheetResult = render.SheetResult

// SheetText 单条图纸文本及其图框归属（docling 集成的文本导出单元）。
type SheetText = render.SheetText

// RenderPNG 将文档渲染为 PNG 字节流。
func RenderPNG(doc *Document, opts RenderOptions) ([]byte, error) {
	return render.RenderPNG(doc, opts)
}

// RenderSVG 将文档渲染为 SVG 字节流。
func RenderSVG(doc *Document, opts RenderOptions) ([]byte, error) {
	return render.RenderSVG(doc, opts)
}

// RenderSheetPNG 将 sheet 指定的图框渲染为 PNG 字节流。
// 未指定输出宽度时根据框内文字自适应选取；文档为空或渲染失败时返回错误。
func RenderSheetPNG(doc *Document, sheet Sheet, opts RenderOptions) ([]byte, error) {
	return render.RenderSheetPNG(doc, sheet, opts)
}

// RenderSheetSVG 将 sheet 指定的图框渲染为 SVG 矢量字节流，并按图框范围设置视口。
// 文档为空或渲染失败时返回错误。
func RenderSheetSVG(doc *Document, sheet Sheet, opts RenderOptions) ([]byte, error) {
	return render.RenderSheetSVG(doc, sheet, opts)
}

// RenderAllSheets 渲染全部图纸（format 为 "png"/"svg"）。
func RenderAllSheets(doc *Document, opts RenderOptions, format string) ([]SheetResult, error) {
	return render.RenderAllSheets(doc, opts, format)
}

// DetectSheets 识别模型空间中的图框并返回其名称和世界坐标范围。
// 未识别到图框时回退为整图范围；doc 为 nil 时返回 nil。
func DetectSheets(doc *Document) []Sheet { return render.DetectSheets(doc) }

// SheetTexts 收集模型空间及其块引用中的文字，并按识别出的图框归属。
// 图框序号从 1 开始，0 表示框外。DetectSheets 返回的“整图”回退范围
// 不作为真实图框参与归属，此时所有文字的 Sheet 均为 0。
func SheetTexts(doc *Document) []SheetText {
	sheets := DetectSheets(doc)
	if len(sheets) == 1 && sheets[0].Name == "整图" {
		sheets = nil
	}
	return render.SheetTexts(doc, sheets)
}

// SanitizeSheetName 将图纸名称转换为可用作文件名的名称。
func SanitizeSheetName(name string) string { return render.SanitizeSheetName(name) }

// WriteDwg 按文档携带的容器素材写出 DWG（R2000/R2004/R2007 家族自适应）。
func WriteDwg(doc *Document, w io.Writer) error { return writer.WriteDwg(doc, w) }

// WriteDwgR2000 写出 R2000（AC1015）家族 DWG（含同容器 R13/R14）。
func WriteDwgR2000(doc *Document, w io.Writer) error { return writer.WriteDwgR2000(doc, w) }

// WriteDwgR2004 写出 R2004（AC1018）家族 DWG。
func WriteDwgR2004(doc *Document, w io.Writer) error { return writer.WriteDwgR2004(doc, w) }

// WriteDwgR2007 写出 R2007（AC1021）DWG。
func WriteDwgR2007(doc *Document, w io.Writer) error { return writer.WriteDwgR2007(doc, w) }

// Parse 解析 DWG 字节流（R13~R2018 主流容器 + R9/R10/R11 pre-R13）。
func Parse(data []byte) (*Document, error) { return drawing.Parse(data) }

// ParseDXF 自动识别并解析 ASCII 或二进制 DXF，返回可用于文本提取和渲染的文档。
// 输入格式无效或段解析失败时返回错误。
func ParseDXF(data []byte) (*Document, error) { return drawing.ParseDXF(data) }

// ParseJSON 解析 dwgread -O JSON 结构（导入路径）。
func ParseJSON(data []byte) (*Document, error) { return drawing.ParseJSON(data) }

// WriteDXF 将文档写出为 DXF 字节流。
func WriteDXF(doc *Document, w io.Writer) error { return drawing.WriteDXF(doc, w) }

// DumpEntities 输出模型空间全部实体的对照 JSON（handle/type/字段），
// 用于与参考实现输出做逐字段一致性对比。
func DumpEntities(data []byte) (string, error) { return drawing.DumpEntities(data) }

// DumpEntitiesDoc 导出已构造文档中的实体字段，支持 DWG、DXF 和 JSON 解析结果。
func DumpEntitiesDoc(doc *Document) (string, error) { return drawing.DumpEntitiesDoc(doc) }

// DebugObjectIndexExport 导出 DWG 对象索引中的句柄及偏移，供解析诊断使用。
// 对象索引读取失败时返回错误。
func DebugObjectIndexExport(data []byte) ([]struct {
	Handle uint64
	Offset uint32
}, error) {
	return drawing.DebugObjectIndexExport(data)
}

// DebugRecord2 调试用：解析记录并返回 body。
func DebugRecord2(objectsData []byte, ref struct {
	Handle uint64
	Offset uint32
}, r2010Plus bool) (body []byte, bitOff uint64, size uint32, err error) {
	return drawing.DebugRecord2(objectsData, ref, r2010Plus)
}

// LoadNamedSectionDebug2 按 name 读取 DWG 数据段，供解析诊断使用。
// 返回解码后的段数据，或段查找、读取与解码过程中产生的错误。
func LoadNamedSectionDebug2(data []byte, name string) ([]byte, error) {
	return drawing.LoadNamedSectionDebug2(data, name)
}

// DebugLines 调试用：输出全部 LINE 几何（handle → 6 坐标），用于与参考实现对照。
func DebugLines(data []byte) map[uint64][6]float64 { return drawing.DebugLines(data) }

// DebugScanGoldLines 扫描原始对象记录，查找坐标与给定参考线段匹配的 LINE。
// 返回匹配记录的句柄和偏移，供解析诊断使用。
func DebugScanGoldLines(data []byte, gold map[uint64][6]float64) []string {
	return drawing.DebugScanGoldLines(data, gold)
}

// DebugObjectBody 调试用：导出指定句柄对象的原始 body 位流与起始位偏移。
func DebugObjectBody(data []byte, handle uint64) (body []byte, bitOffset uint64, err error) {
	return drawing.DebugObjectBody(data, handle)
}

// PreparedDrawing shares geometry, text layout and viewport between SVG and PNG.
type PreparedDrawing = render.PreparedDrawing
type RenderAudit = render.RenderAudit

// PrepareDrawing 构建供 SVG 和 PNG 共用的几何、文字布局及渲染诊断。
// 渲染选项无效或指定布局无法解析时返回错误；可恢复的图元问题记录在 Audit 中。
func PrepareDrawing(doc *Document, opts RenderOptions) (*PreparedDrawing, error) {
	return render.PrepareDrawing(doc, opts)
}

type Box2 = drawing.Box2
