# Go-CAD

使用 Go 解析 DWG/DXF、提取文本，并输出 SVG 和 PNG。矢量输出支持模型空间、纸张布局、视口裁切、字体轮廓和显式配置的外部引用。

本仓库在 [unitedrhino/go-cad](https://github.com/unitedrhino/go-cad) 的 `ddd91eea9618cc1a2feec32ab6397239d1f9f315` 版本基础上维护。许可证见 [LICENSE](LICENSE)。

## 环境

- Go 1.25 或更新版本。
- 中文图纸需要可用的中文字体；可通过 `-font-map` 或 `-font-dir` 指定。
- 外部引用需提供可读取的原文件路径。程序通过映射直接读取，不管理云盘挂载，也不复制或移动外部 DWG。

## 转换图纸

在仓库根目录执行：

```console
go -C example run . -f /path/to/drawing.dwg -o ./output -layout all -plot -style clear -text-mode path -fill-mode outline -render-only -combine-svg
```

`-o` 的相对路径以 `example/` 为基准。输出包括各布局的 SVG/PNG、合并 SVG 及诊断记录。`-combine-svg` 按 DWG 布局顺序合并纸张；没有纸张布局时使用模型空间。可加 `-combine-model` 附上模型总览。

只导出指定布局：

```console
go -C example run . -f /path/to/drawing.dwg -o ./output -layout Main -plot -style clear -text-mode path -render-only
```

`Main` 应替换成输入 DWG 中的布局名称。详细参数、外参配置和输出说明见 [example/README.md](example/README.md)。

## Go API

```go
import cad "github.com/boyunsz/go-cad"

// data 是调用方读取的 DWG 字节。
doc, err := cad.Parse(data)
if err != nil {
    return err
}
prepared, err := cad.PrepareDrawing(doc, cad.RenderOptions{
    Layout: "model", Plot: true, Monochrome: true,
    TextMode: "path", FillMode: "outline", Width: 4096,
})
if err != nil {
    return err
}
svg, err := prepared.SVG()
```

`PreparedDrawing` 让 SVG 和 PNG 共享几何、文字布局和视口。`path` 模式输出字体轮廓，可减少查看端字体替换造成的变化。缺失字体、外参或不支持的实体会影响还原程度，请同时检查渲染诊断。

## 验证

```console
go test ./...
go -C example test ./...
```

基础测试使用内存构造的数据。依赖私有图纸的回归测试默认跳过；可通过 `GO_CAD_FIXTURES` 指向本地 JSON 映射，或在仓库根目录创建 `.local/fixtures.json`。映射使用匿名编号，文件路径相对映射文件解析；布局等非文件值保持原样。示意：

```json
{
  "drawing-a": "../dwg/drawing-a.dwg",
  "drawing-a.layout": "Main"
}
```

这些测试验证固定样本的句柄、实体数量和排版，不能把任意 DWG 当作对应样本。未配置某个样本时只跳过依赖它的测试。

## 提交范围

仓库只维护源码、模块依赖、文档和测试逻辑。`.gitignore` 排除整个 `dwg/`、生成目录、所有 `.sh`、JSON 配置/记录、DWG/DXF/SVG/PNG/PDF，以及编辑器和本机配置。样本、字体和外参路径由使用者自行配置。输出诊断可能包含输入路径与图纸文字，应留在本地。

## 函数注释约定

所有具名函数和方法（含私有实现及测试）使用紧邻声明的 GoDoc 注释，以准确的函数名开头。首句说明行为；必要时补充参数单位、返回值、错误或回退行为、输入修改和资源限制。测试注释说明被验证的约束。简单访问器保持简短，复杂解析与渲染函数保留格式依据和限制，不使用仅重复函数名的模板说明。
