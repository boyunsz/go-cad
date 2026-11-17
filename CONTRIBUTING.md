# 开发指南

本指南说明 Go-CAD 的模块组织、验证方式和代码文档约定。安装和功能介绍见 [README](README.md)，转换参数见 [命令行指南](example/README.md)。

## 开发环境

使用 Go 1.25 或更新版本。在仓库根目录下载两个模块的依赖：

```console
go mod download
go -C example mod download
```

根目录是库模块，`example/` 是独立模块，使用 `replace` 指向父目录。根目录的 `go test ./...` 不包含示例模块，需要分别验证。

## 验证流程

```console
go test ./...
go -C example test ./...
go build ./...
go -C example build -o ../bin/cad-example .
```

[持续集成](.github/workflows/ci.yml) 在 push 和 pull request 时执行两个模块的测试与构建。修改 Go 文件后使用 `gofmt` 格式化；新增格式分支或修复解析、渲染问题时，补充能区分修复前后行为的回归测试。

解析测试应覆盖字段值、记录边界及错误处理。渲染测试应同时关注几何、文字布局、裁剪和诊断结果；涉及显示变化时，核对实际 SVG，PNG 可用于辅助预览。

## 可选样本测试

基础测试使用程序构造的数据。部分回归测试依赖特定图纸，通过 `GO_CAD_FIXTURES` 指定配置，或读取仓库根目录的 `.local/fixtures.json`。

配置示例：

```json
{
  "drawing-a": "../samples/drawing-a.dwg",
  "drawing-a.layout": "Layout1"
}
```

文件路径相对配置文件目录解析；布局名等非文件值保持原样。样本编号与测试中的固定断言对应，配置时须使用匹配的样本，不能替换成任意 DWG。

```console
GO_CAD_FIXTURES="/path/to/fixtures.json" go test ./...
GO_CAD_FIXTURES="/path/to/fixtures.json" go -C example test ./...
```

未配置某项或样本文件不存在时，相关测试跳过；配置格式错误或资源不可读取会使测试失败。记录验证结果时，应区分通过、失败和跳过的测试。

## 模块职责

- `cad.go` 保持公共 API 集中，供应用接入。
- `internal/bitstream`、`container`、`objrec` 处理文件和记录格式。
- `internal/entity`、`object` 解析实体与非实体对象的字段。
- `internal/drawing` 统一文档模型、布局、引用及几何展开。
- `internal/render` 处理字体、资源、场景准备及 SVG / PNG 输出。
- `internal/writer` 处理 DWG 编码；DXF 导出由 drawing 模块提供。
- `example/` 负责参数、文件输出、多页合并和诊断组织。

格式修复应放在对应解析或模型层；CLI 负责配置与流程组织，避免把针对某个图纸的坐标或文件名条件写入通用转换逻辑。

## 注释与文档

- 具名函数和方法，包括私有实现与测试，使用紧邻声明的 GoDoc 注释，以准确名称开头。首句说明行为，必要时补充单位、错误、回退、输入修改和资源限制。
- 包注释以 `Package 包名` 开头，每个包保留一处。文件说明概述职责及适用版本，具体位偏移和字段顺序放在实现附近。
- 类型和字段注释解释数据含义、单位、有效条件、所有权及缺省值。行内注释说明处理原因或边界。
- 注释使用当前代码中的名称。格式依据单独注明，避免将单次样本结果描述成通用兼容性保证。
- 编译指令、工具指令及许可证声明按原语义保留。修改公共 API 或命令行行为时，同步更新使用示例。

## 问题报告与变更说明

报告问题时提供 Go 版本、操作系统、提交版本、复现命令、DWG 版本和所选布局，并说明预期结果与实际结果。显示问题可附同一范围、相近缩放比例的对比图，以及相关诊断条目。

变更说明应包含具体问题、修改后的行为、验证方式和仍存在的限制。若错误涉及外部资源或字体，应说明复现所需的配置，便于区分输入缺失与转换问题。
