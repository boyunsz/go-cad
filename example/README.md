# 命令行使用指南

本示例将 DWG 转换为 SVG 和 PNG，并生成布局、字体、资源及实体覆盖诊断。它是独立 Go 模块，通过 `replace github.com/boyunsz/go-cad => ..` 使用仓库中的库源码。

[返回项目首页](../README.md) · [开发指南](../CONTRIBUTING.md)

## 执行方式

以下命令均在仓库根目录运行，要求 Go 1.25 或更新版本。`go -C example` 会切换到 `example/`，因此输入、输出和配置文件的相对路径均以该目录为基准。路径含空格时使用引号。

查看全部参数：

```console
go -C example run . -h
```

也可构建后直接运行：

```console
go -C example build -o ../bin/cad-example .
./bin/cad-example -h
```

运行可执行文件时，相对路径以当前工作目录为基准。

## 常用任务

### 转换模型空间

```console
go -C example run . \
  -f "/path/to/drawing.dwg" \
  -o ./output/model \
  -layout model \
  -style clear \
  -text-mode path \
  -fill-mode outline \
  -render-only
```

### 转换指定布局

将 `Layout1` 替换为 DWG 中的实际布局名称：

```console
go -C example run . \
  -f "/path/to/drawing.dwg" \
  -o ./output/layout \
  -layout "Layout1" \
  -plot \
  -style clear \
  -render-only
```

`-plot` 按打印可见性过滤辅助内容。需要检查完整的显示内容时可省略该参数。

### 输出全部布局并合并

```console
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

合并规则：

1. `-combine-svg` 自动启用 `-layout all`，生成模型空间和各纸张布局的独立输出。
2. 按 DWG 页签顺序，将纸张布局纵向排入同一个 SVG；每页保留自己的坐标变换与裁剪。
3. 合并时优先使用通过验证的纸张范围，无法验证时保留该页完整 SVG 范围。
4. 有纸张布局时，模型空间默认只作为独立产物。加 `-combine-model` 可将模型总览追加到合并结果。
5. 没有纸张布局时，合并结果使用模型空间。

仅输出全部布局而不生成合并文件时，使用 `-layout all` 替代 `-combine-svg`。

### 合并已有结果

```console
go -C example run . \
  -f "/path/to/drawing.dwg" \
  -o ./output \
  -style clear \
  -combine-only
```

`-combine-only` 不重新渲染，但仍需读取 DWG 的布局顺序。输出目录和样式须与上次转换一致，并保留单页 SVG 及 `render-audit.json`；缺少所需文件时返回错误。

## 参数参考

下表列出 DWG 转换路径的常用参数。默认值以当前命令行帮助为准。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-f` | 必填 | 输入 DWG 文件 |
| `-o` | `.` | 输出目录 |
| `-layout` | `model` | `model`、实际布局名或 `all` |
| `-fidelity` | `faithful` | 共享渲染流程；`preview` 为受限的模型预览流程 |
| `-style` | `soft` | `soft` 柔和灰色、`original` 原始颜色、`clear` 黑白；`both` 输出 original 和 soft 两版 |
| `-plot` | `false` | 按 CAD 打印可见性过滤内容 |
| `-text-mode` | `path` | `path` 输出字形轮廓，`text` 保留文本节点 |
| `-fill-mode` | `outline` | `outline` 将实心 HATCH 显示为轮廓；`source` 使用源填充 |
| `-width` | `4096` | PNG 和 SVG 画布宽度，范围 1–16384；曲线精度由容差另行控制 |
| `-curve-tolerance` | `0.01` | 曲线细分容差，单位与图纸坐标一致 |
| `-line-scale` | `0` | 线宽倍率，范围 0–4；0 使用样式默认值 |
| `-units-per-mm` | `1` | 将物理线宽转换到图纸单位的系数，不代表已自动识别图纸单位 |
| `-bounds` | `full` | `full` 使用完整渲染范围，`explicit` 需要指定视口 |
| `-view` | 空 | `xmin,ymin,xmax,ymax`，设置后自动使用 explicit 范围 |
| `-svg` | `true` | 是否同时输出 SVG；禁用时使用 `-svg=false` |
| `-render-only` | `false` | 跳过 DXF 往返检查，仍输出图片和诊断 |
| `-combine-svg` | `false` | 转换全部布局后生成合并 SVG |
| `-combine-model` | `false` | 向合并文件追加模型空间 |
| `-combine-only` | `false` | 仅组合已有的单页结果 |
| `-xref-map` | 空 | 外部 DWG 映射文件 |
| `-font-map` | 空 | 字体映射文件 |
| `-font-dir` | 空 | 字体搜索目录 |
| `-resource-dir` | 空 | 外部位图目录，按有效 IMAGEDEF 中的文件名匹配 |

合并需要 `faithful` 流程和 SVG 输出。`preview` 不支持本指南中的布局合并、外参映射、打印过滤和 clear 样式；相关任务应使用默认的 `faithful` 流程。

`path` 模式固定输出时选用的字形，但字体替换仍可能改变外观。`outline` 只影响实心 HATCH，尺寸箭头和剖面图案继续保留。

## 外部引用

创建外参映射文件，例如 `xref-map.json`：

```json
{
  "part-a.dwg": "/path/to/shared/parts/part-a.dwg",
  "part-b.dwg": "../parts/part-b.dwg"
}
```

键为主图声明的外参文件名或块名，值为可读取的 DWG 路径。映射中的相对路径以映射文件所在目录为基准。

在转换命令中加入：

```console
go -C example run . \
  -f "/path/to/drawing.dwg" \
  -o ./output \
  -xref-map "/path/to/xref-map.json" \
  -plot -style clear -render-only -combine-svg
```

外参按主图中的插入点、块基点、缩放、旋转和视口裁剪参与渲染。网络共享需事先挂载并可读取。已匹配映射的文件读取或解析失败会返回错误；未加载资源可结合诊断文件排查。

## 字体与外部图片

字体映射示例：

```json
{
  "SourceFont": "/path/to/fonts/cjk-font.ttc#FontName"
}
```

`SourceFont` 替换为图纸中声明的字体名称，`FontName` 为 TTC 集合内的字体名称。使用 `-font-map "/path/to/font-map.json"` 加载，也可用 `-font-dir "/path/to/fonts"` 指定搜索目录。字体映射中的相对路径以映射文件目录为基准。

外部图片通过 `-resource-dir "/path/to/images"` 提供；有效的 IMAGE / IMAGEDEF 关联决定插入位置和文件匹配。图片会作为位图内容参与输出。

## 输出文件

以下以 `drawing.dwg`、`-style clear -combine-svg` 为例。`<布局标识>` 由句柄和布局名称组成；实际文件前缀来自输入文件名。

```text
output/
├── drawing.dwg.combined-clear.svg       # 多页合并结果
├── drawing.dwg.faithful-clear.svg       # 模型空间
├── drawing.dwg.faithful-clear.png       # 模型空间预览
├── render-audit.json
├── text-font-audit.json
├── loaded-xrefs.json
├── ...                                 # 其他诊断记录
└── layouts/
    └── <布局标识>/
        ├── drawing.dwg.faithful-clear.svg
        ├── drawing.dwg.faithful-clear.png
        ├── render-audit.json
        └── ...
```

| 诊断文件 | 用途 |
| --- | --- |
| `render-audit.json` | 当前空间、渲染范围、字体及处理限制 |
| `text-font-audit.json` | 原始与显示文本、字体选择及替换信息 |
| `external-resources.json` / `loaded-xrefs.json` | 声明的资源与实际绑定的外部 DWG |
| `entity-render-map.json` | 源实体与渲染实例的对应关系 |
| `coverage-report.json` | 按类型统计源记录、渲染实例及缺失或近似处理 |
| `attribute-audit.json` / `revision-log-audit.json` | 属性值及更改记录相关属性的渲染情况 |
| `layout-inventory.json` | 布局、视口和块控制对象信息 |
| `source-provenance.json` | 输入和产物的来源与校验信息 |
| `dxf-diff.json` | 未设置 `-render-only` 时生成的 DXF 往返差异 |

源记录数与渲染实例数的统计单位不同：一个块可能重复插入，也可能未被引用，不能仅用数量差值判断图形丢失。

## 常见问题

| 现象 | 检查方式 |
| --- | --- |
| 配件或图片缺失 | 检查资源映射、文件可读性及外参诊断；核对所选布局中的可见性 |
| 中文缺字或文字样式不同 | 检查 `text-font-audit.json`，提供覆盖目标文字的字体映射 |
| 模型空间与纸张预览不同 | 选择源 DWG 的实际布局名称，核对 `-plot` 和视口范围 |
| 合并文件缺少某一页 | 确认该页存在于布局列表，并已输出对应样式的单页 SVG 和诊断 |
| 线条或填充与源图不同 | 核对 style、fill-mode、线宽系数、图层打印状态及渲染限制 |
| SVG 放大效果与 PNG 不同 | 直接打开 SVG 检查矢量部分；PNG 和嵌入图片受固定像素尺寸限制 |

测试配置与代码维护方式见 [开发指南](../CONTRIBUTING.md)。
