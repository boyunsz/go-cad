# DWG 转 SVG 示例

示例是独立 Go 模块，通过 `replace ... => ..` 使用父目录源码。以下命令均从仓库根目录运行。

## 输出全部布局并合并

```console
go -C example run . -f /path/to/drawing.dwg -o ./output -layout all -plot -style clear -text-mode path -fill-mode outline -render-only -combine-svg
```

`-combine-svg` 会自动选择 `-layout all`。每个布局保留自己的视口与裁切，再按 DWG 页签顺序排列在一个 SVG 中；默认不重复附加模型总览。只有模型空间时自动合并模型输出。

生成文件位于 `example/output/`：

- `drawing.dwg.combined-clear.svg`：合并图纸。
- `layouts/<布局标识>/drawing.dwg.faithful-clear.svg`：单张布局。
- 根目录的 `drawing.dwg.faithful-clear.svg`：模型空间。
- PNG 预览以及渲染、字体、外部引用和来源诊断 JSON。

文件前缀来自实际输入文件名，以上均为通用示例。需要固定输出位置时，给 `-o` 传入绝对路径。

## 常用参数

| 参数 | 作用 |
| --- | --- |
| `-f` | 输入 DWG，必填 |
| `-o` | 输出目录 |
| `-layout model` | 输出模型空间；也可传布局名或 `all` |
| `-plot` | 按打印可见性过滤辅助图层 |
| `-style clear` | 黑白输出；另有 `soft`、`original`、`both` |
| `-text-mode path` | 将文字转为固定字体轮廓；`text` 保留可编辑文本 |
| `-fill-mode outline` | 实心 HATCH 显示轮廓，保留尺寸箭头和剖面线；`source` 使用源填充 |
| `-width 4096` | PNG 宽度及输出画布宽度设置，不是 SVG 曲线分辨率 |
| `-curve-tolerance 0.01` | 曲线离散容差，单位与图纸一致 |
| `-view xmin,ymin,xmax,ymax` | 指定查看范围 |
| `-combine-svg` | 渲染所有布局并合并 |
| `-combine-model` | 合并时追加模型总览 |
| `-combine-only` | 合并输出目录中已有的 SVG；仍需 `-f` 读取布局顺序 |
| `-render-only` | 跳过 DXF 往返校验，仍输出 PNG/SVG 和诊断 |

完整参数：

```console
go -C example run . -h
```

SVG 可缩放，但文字度量、线型与源文件的打印设置仍影响显示效果。检查矢量细节时应直接打开 SVG；PNG 是固定像素预览。这里不强制把图纸单位映射为物理毫米。

## 外部引用

在本地创建 JSON 映射，将 DWG 声明的外参文件名或块名映射为可读取的文件路径：

```json
{
  "part-a.dwg": "/path/to/mounted-share/parts/part-a.dwg",
  "part-b.dwg": "../parts/part-b.dwg"
}
```

```console
go -C example run . -f /path/to/drawing.dwg -o ./output -plot -style clear -text-mode path -render-only -combine-svg -xref-map /path/to/xref-map.json
```

相对路径以映射文件所在目录为基准。读取外参后，按主图插入点、块基点、缩放、旋转及视口裁切绘制。映射必须对应实际声明的名称，缺失外参会记录诊断。使用网络共享路径时，需先由操作系统挂载并确认文件可访问。

## 字体

字体映射的键为源字体名称，值为字体文件路径；TTC 字体集可使用 `#字体名称` 指定成员：

```json
{
  "SourceFont": "/path/to/fonts/cjk-font.ttc#FontName"
}
```

通过 `-font-map /path/to/font-map.json` 加载，或使用 `-font-dir /path/to/fonts`。请检查 `text-font-audit.json` 中的字体替换和缺失字形记录。字体文件不随仓库分发。

## 测试

```console
go test ./...
go -C example test ./...
```

图纸回归测试使用样本编号，读取根目录 `.local/fixtures.json` 或 `GO_CAD_FIXTURES` 指定的配置。没有对应样本时自动跳过。
