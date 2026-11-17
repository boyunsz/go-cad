# Go-CAD

Go 语言 CAD 文件处理库，提供 DWG / DXF 解析、文本提取、SVG / PNG 渲染及图纸导出能力。适用于图纸预览、文档处理和 CAD 数据集成。

[快速开始](#快速开始) · [Go API](#go-api) · [命令行指南](example/README.md) · [开发指南](CONTRIBUTING.md) · [许可证](LICENSE)

## 功能概览

| 能力 | 说明 |
| --- | --- |
| 图纸解析 | 读取 DWG、ASCII / 二进制 DXF，以及 `dwgread -O JSON` 格式的数据 |
| 文本与结构 | 查询文字、实体、块引用、图层和布局信息 |
| 矢量与预览 | 输出 SVG 和 PNG，复用几何、文字布局与视口计算结果 |
| 布局输出 | 渲染模型空间或指定纸张布局，处理正交视口及裁剪 |
| 多页合并 | 示例程序按布局页签顺序，将各页纵向组合为一个 SVG |
| 资源解析 | 通过显式映射加载外部 DWG 和字体，读取关联的外部位图 |
| 图纸导出 | 提供 DXF 和 DWG 写出接口；覆盖范围取决于输入版本及已支持的数据类型 |
| 转换诊断 | 记录字体替换、缺失资源、近似处理及实体到输出图元的对应关系 |

## 环境要求

- **Go 1.25 或更新版本**。
- 中文等多语言文字需要相应字体，可通过字体映射或字体目录配置。
- 含外部引用的图纸需要可访问的配件文件；使用网络共享时，应先完成系统挂载。

## 快速开始

### 作为 Go 库使用

在已有 Go 模块中添加依赖：

```console
go get github.com/boyunsz/go-cad
```

### 运行转换示例

```console
git clone https://github.com/boyunsz/go-cad.git
cd go-cad
go -C example run . \
  -f "/path/to/drawing.dwg" \
  -o ./output \
  -plot \
  -style clear \
  -text-mode path \
  -fill-mode outline \
  -render-only \
  -combine-svg
```

将输入路径替换为实际文件。上述命令输出模型空间、各纸张布局和合并 SVG，同时生成 PNG 预览及诊断记录。

- 输出目录为 `example/output/`；`go -C example` 使相对路径以 `example/` 为基准。
- `-combine-svg` 自动选择全部布局。合并文件按页签顺序排列纸张；没有纸张布局时使用模型空间。
- `-style clear` 使用黑白显示；`-fill-mode outline` 将实心 HATCH 显示为轮廓，保留箭头和图案填充。需要原始颜色与填充时，使用 `-style original -fill-mode source`。
- `-render-only` 跳过 DXF 往返校验，保留渲染和诊断输出。

单页导出、字体、外部引用、输出文件和参数说明见 [命令行指南](example/README.md)。命令行转换入口接收 DWG；DXF 和 JSON 通过 Go API 读取。

## Go API

以下程序读取 DWG 并生成模型空间 SVG。将代码保存为应用模块中的 `main.go`：

```go
package main

import (
	"log"
	"os"
	"path/filepath"

	cad "github.com/boyunsz/go-cad"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: cad-demo input.dwg output.svg")
	}
	input, output := os.Args[1], os.Args[2]
	data, err := os.ReadFile(input)
	if err != nil {
		log.Fatal(err)
	}
	doc, err := cad.Parse(data)
	if err != nil {
		log.Fatal(err)
	}
	prepared, err := cad.PrepareDrawing(doc, cad.RenderOptions{
		Layout:         "model",
		SourceDir:      filepath.Dir(input),
		Monochrome:     true,
		TextMode:       "path",
		FillMode:       "outline",
		CurveTolerance: 0.01,
		Width:          4096,
	})
	if err != nil {
		log.Fatal(err)
	}
	svg, err := prepared.SVG()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(output, svg, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("version=%s entities=%d skipped=%d diagnostics=%d",
		doc.Version(), doc.EntityCount(), doc.Skipped(), len(prepared.Audit.Diagnostics))
}
```

添加依赖后，在应用模块中运行：

```console
go run . "/path/to/drawing.dwg" drawing.svg
```

`prepared.PNG()` 可复用同一次准备结果生成 PNG。`prepared.Audit` 提供渲染诊断；`doc.Skipped()` 表示解析时跳过的记录数量，两者用于检查不同阶段的问题。

需要按打印可见性过滤内容时，在选项中设置 `Plot: true`；字体和外参可通过 `FontMap`、`FontDir` 和 `XrefFiles` 配置。

### 常用接口

| 接口 | 用途 |
| --- | --- |
| `cad.Parse(data)` | 解析 DWG 字节 |
| `cad.ParseDXF(data)` | 解析 ASCII 或二进制 DXF |
| `cad.ParseJSON(data)` | 导入 `dwgread -O JSON` 结构 |
| `doc.Texts()` | 提取模型空间及块引用中的文字 |
| `doc.Layouts()` | 查询布局名称、页签顺序及空间信息 |
| `cad.PrepareDrawing(doc, opts)` | 准备指定空间的几何、文字、视口和诊断 |
| `prepared.SVG()` / `prepared.PNG()` | 输出共享渲染结果 |
| `cad.DetectSheets(doc)` | 按模型空间内容启发式识别图框 |
| `cad.WriteDXF(doc, writer)` | 写出 DXF |
| `cad.WriteDwg(doc, writer)` | 根据文档携带的数据选择 DWG 写出路径 |

调用 `cad.RenderSVG` 或 `cad.RenderPNG` 时，应设置 `RenderOptions.Fidelity = "faithful"` 以使用共享渲染流程；未设置时使用预览流程。直接调用 `PrepareDrawing` 无需额外设置该字段。

## 兼容性与渲染边界

DWG 读取路径覆盖 R9、R10、R11，以及 R13、R14、R2000、R2004、R2007、R2010、R2013 和 R2018 文件标识。实体、专用对象及写出能力的覆盖程度随版本而异，应结合实际样本和诊断信息验收。

| 场景 | 当前行为 |
| --- | --- |
| 字体 | 支持可用的 TTF / OTF / TTC 字体；SHX、Big Font 需要替代字体，字形可能不同 |
| 打印样式 | 支持图层打印可见性及已解析的线型、线宽；尚未复现 CTB / STB 打印样式表 |
| 文字排版 | 支持常用 TEXT / MTEXT 排版；高级段落制表、缩进和复杂文字塑形仍有限制 |
| 曲线 | 按容差细分；任意有理曲线的全局误差不保证均受该容差约束 |
| 外部资源 | 依赖可访问的原文件；已匹配的外参读取失败可能终止转换，其他缺失资源由诊断记录 |
| 空间选择 | 纸张布局使用源视口；模型空间图框识别为启发式分幅，两者含义不同 |

SVG 中的矢量几何可缩放，嵌入位图仍受原始像素限制。文字外观还取决于字体和排版结果；建议在查看器中直接检查 SVG，并同步检查转换诊断。

## 项目结构

```text
go-cad/
├── cad.go                 # 公共 API
├── go.mod                 # 库模块及依赖
├── example/               # 独立命令行示例模块
├── internal/
│   ├── bitstream/         # 位流读写与校验
│   ├── container/         # DWG 容器、分页与压缩
│   ├── objrec/            # 对象记录与类型码
│   ├── entity/            # 实体模型及字段解析
│   ├── object/            # 非实体对象与符号表
│   ├── drawing/           # 文档模型、格式导入、布局与几何展开
│   ├── render/            # 字体、资源、SVG / PNG 渲染
│   ├── writer/            # DWG 编码与写出
│   └── testfixtures/      # 可选样本测试配置
├── .github/workflows/     # 持续集成
├── CONTRIBUTING.md        # 开发与验证指南
└── LICENSE
```

根模块提供库接口，`example/` 通过独立 `go.mod` 和本地 `replace` 使用父目录源码。业务集成应从根包 `github.com/boyunsz/go-cad` 导入。

## 开发与验证

在仓库根目录运行：

```console
go test ./...
go -C example test ./...
go build ./...
go -C example build -o ../bin/cad-example .
```

CI 对库模块和示例模块分别执行测试与构建。依赖额外样本的回归测试在未配置样本时跳过；配置方法、代码规范和问题报告要求见 [开发指南](CONTRIBUTING.md)。

## 项目来源与许可证

本项目基于 [unitedrhino/go-cad](https://github.com/unitedrhino/go-cad) 的 `ddd91eea9618cc1a2feec32ab6397239d1f9f315` 版本维护，采用 [Apache License 2.0](LICENSE)。
