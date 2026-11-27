<div align="center">

# Go-CAD

**纯 Go 实现的 2D CAD 文件处理库，支持 DWG / DXF 解析、文本提取、SVG / PNG 渲染与图纸导出。**

[![CI](https://github.com/boyunsz/go-cad/actions/workflows/ci.yml/badge.svg)](https://github.com/boyunsz/go-cad/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/boyunsz/go-cad.svg)](https://pkg.go.dev/github.com/boyunsz/go-cad)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

[English](README.md) | **简体中文**

[功能概览](#功能概览) · [快速开始](#快速开始) · [Go API](#go-api) · [兼容性与边界](#兼容性与渲染边界) · [命令行指南](example/README.md) · [开发指南](CONTRIBUTING.md)

</div>

---

## 简介

**Go-CAD** 是一个零 CGO、纯 Go 实现的 AutoCAD DWG / DXF 处理库。它将图纸容器、实体、符号表、块定义与纸张空间布局解析为统一的 `Document` 模型，并提供高保真的矢量 SVG 与光栅 PNG 渲染能力及完整的转换诊断信息。

适用于**在线图纸预览服务**、**工程文档自动化处理**以及 **CAD 数据集成**场景。

## 功能概览

| 能力 | 说明 |
| --- | --- |
| **图纸解析** | 纯 Go 读取 DWG（R9–R11、R13–R2018）、ASCII / 二进制 DXF，以及 `dwgread -O JSON` |
| **文本与编码** | 提取 TEXT / MTEXT / ATTRIB，支持 `\U+`、`\M+`（GBK / Big5 / Shift-JIS / EUC-KR）、拆分 MIF 及符号表名转义解码 |
| **矢量与光栅渲染** | 通过 `PreparedDrawing` 共享几何、文字排版与视口计算，统一输出 SVG 和 PNG |
| **布局与视口** | 渲染模型空间或指定纸张布局，支持正交视口变换与边界裁剪 |
| **多页合并** | 示例 CLI 按布局页签顺序，将各页纵向组合为单个多页 SVG |
| **外部资源解析** | 通过显式映射加载外部参照 DWG（`XrefFiles`）、TTF / OTF / TTC 字体（`FontMap` / `FontDir`）及外部位图（`ResourceDir`） |
| **图纸导出** | 提供 DXF 与 DWG 写出接口，在支持的范围内保留原始符号名与容器元数据 |
| **转换诊断与审计** | 记录字体替换、缺字统计、外参绑定、跳过记录及实体到输出图元的映射关系 |

## 环境要求

- **Go 1.25 或更新版本**（无需 CGO）。
- 中文等多语言文字渲染需通过 `FontMap` 或 `FontDir` 配置相应的 TTF / OTF / TTC 字体。
- 含外部参照（Xref）的图纸需通过 `XrefFiles` 提供本地可访问的外参 DWG 路径（库本身不会隐式发起网络请求）。

## 快速开始

### 作为 Go 库使用

```console
go get github.com/boyunsz/go-cad
```

### 运行转换示例 CLI

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

将 `/path/to/drawing.dwg` 替换为实际 DWG 文件。上述命令会在 `example/output/` 下输出模型空间、各纸张布局、多页合并 SVG、PNG 预览及诊断 JSON 文件。

- `-combine-svg` 自动包含全部纸张布局并按页签顺序排列；无纸张布局时回退至模型空间。
- `-style clear` 使用黑白线条输出；`-fill-mode outline` 将实心 HATCH 输出为轮廓并保留箭头和图案填充。如需保留原始颜色与实心填充，使用 `-style original -fill-mode source`。
- `-render-only` 跳过 DXF 往返校验，保留渲染与诊断输出。

单页导出、字体与外参映射文件及完整参数说明见 [命令行指南](example/README.md)。

## Go API

以下示例读取 DWG 文件并将模型空间渲染为矢量 SVG：

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

在应用模块中运行：

```console
go run . "/path/to/drawing.dwg" drawing.svg
```

- 调用 `prepared.PNG()` 可复用同一次场景准备结果直接生成 PNG。
- 通过 `prepared.Audit` 查看渲染诊断信息，`doc.Skipped()` 查看解析阶段跳过的记录数。
- 设置 `Plot: true` 可按图层打印可见性过滤内容；通过 `FontMap`、`FontDir` 和 `XrefFiles` 配置字体与外部参照。

### 常用接口

| 接口 | 用途 |
| --- | --- |
| `cad.Parse(data)` | 解析 DWG 字节流 |
| `cad.ParseDXF(data)` | 解析 ASCII 或二进制 DXF |
| `cad.ParseJSON(data)` | 导入 `dwgread -O JSON` 结构 |
| `doc.Texts()` | 提取模型空间及块引用中的文字 |
| `doc.Layouts()` | 查询布局名称、页签顺序及空间信息 |
| `cad.PrepareDrawing(doc, opts)` | 构建共享几何、文字排版、视口和诊断结果 |
| `prepared.SVG()` / `prepared.PNG()` | 从准备结果导出矢量 SVG 或光栅 PNG |
| `cad.DetectSheets(doc)` | 在模型空间中启发式识别图框 |
| `cad.SheetTexts(doc)` | 提取文字并关联所属图框编号 |
| `cad.WriteDXF(doc, writer)` | 写出 DXF |
| `cad.WriteDwg(doc, writer)` | 根据文档携带的容器数据写出 DWG |

> [!NOTE]
> 直接调用 `cad.RenderSVG` 或 `cad.RenderPNG` 时，需设置 `RenderOptions.Fidelity = "faithful"` 以启用共享的 `PrepareDrawing` 渲染管线；未设置时走旧版预览管线。直接调用 `cad.PrepareDrawing` 默认使用高保真管线。

## 兼容性与渲染边界

DWG 读取覆盖 **R9、R10、R11** 以及 **R13、R14、R2000、R2004、R2007、R2010、R2013、R2018** 文件标识。实体、非实体对象与写出能力的覆盖范围随版本而异，建议结合实际样本与诊断报告进行验收。

| 场景 | 当前行为 |
| --- | --- |
| **字体** | 支持 TTF / OTF / TTC 字体；SHX 与 Big Font 需要配置替代字体，字形可能存在差异 |
| **打印样式** | 支持图层打印可见性及已解析线型、线宽；暂未复现 CTB / STB 打印样式表 |
| **文字排版** | 支持常见 TEXT / MTEXT 格式；高级段落制表、缩进和复杂文字塑形仍有限制 |
| **曲线** | 按 `CurveTolerance` 细分；对任意有理样条曲线不保证全局误差严格受限 |
| **外部资源** | 依赖本地可访问的原文件；已匹配外参读取失败会中止转换，其他缺失资源记录在诊断中 |
| **空间选择** | 纸张布局按源视口裁剪渲染；模型空间图框识别为启发式分幅，两者语义不同 |

## 项目结构

```text
go-cad/
├── cad.go                 # 公共库 API
├── go.mod                 # 根模块（零 CGO）
├── example/               # 独立命令行示例模块
├── internal/
│   ├── bitstream/         # 位流读写与校验
│   ├── container/         # DWG 容器、分页与压缩
│   ├── objrec/            # 对象记录与类型码
│   ├── entity/            # 实体模型与字段解析
│   ├── object/            # 非实体对象与符号表
│   ├── drawing/           # 文档模型、格式导入、布局与几何展开
│   ├── render/            # 字体、外部资源、SVG / PNG 渲染
│   ├── writer/            # DWG 编码与写出
│   └── testfixtures/      # 可选样本测试配置
├── .github/workflows/     # GitHub Actions 持续集成
├── CONTRIBUTING.md        # 开发与验证指南
├── README.md              # 英文说明文档
├── README.zh-CN.md        # 简体中文说明文档
└── LICENSE                # Apache License 2.0
```

根模块对外提供库接口（`github.com/boyunsz/go-cad`），`example/` 作为独立模块通过本地 `replace` 引用父目录源码。

## 开发与验证

在仓库根目录执行：

```console
go test ./...
go -C example test ./...
go build ./...
go -C example build -o ../bin/cad-example .
```

CI 会在每次 push 与 pull request 时分别测试和构建两个模块。依赖私有样本的回归测试在未配置样本时自动跳过；样本配置、编码规范与问题反馈要求请参见 [开发指南](CONTRIBUTING.md)。

## 项目来源与许可证

本项目基于 [unitedrhino/go-cad](https://github.com/unitedrhino/go-cad) 的 `ddd91eea9618cc1a2feec32ab6397239d1f9f315` 提交持续维护。

采用 [Apache License 2.0](LICENSE) 开源许可证。
