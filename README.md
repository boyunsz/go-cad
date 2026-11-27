<div align="center">

# Go-CAD

**A pure-Go 2D CAD processing library for DWG / DXF parsing, text extraction, SVG / PNG rendering, and drawing export.**

[![CI](https://github.com/boyunsz/go-cad/actions/workflows/ci.yml/badge.svg)](https://github.com/boyunsz/go-cad/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/boyunsz/go-cad.svg)](https://pkg.go.dev/github.com/boyunsz/go-cad)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**English** | [简体中文](README.zh-CN.md)

[Features](#features) · [Quick Start](#quick-start) · [Go API](#go-api) · [Compatibility](#compatibility--rendering-scope) · [CLI Guide](example/README.md) · [Contributing](CONTRIBUTING.md)

</div>

---

## Overview

**Go-CAD** is a zero-CGO, pure-Go library for working with AutoCAD DWG and DXF drawings. It parses drawing containers, entities, symbol tables, blocks, and paper-space layouts into a unified `Document` model, and renders faithful vector SVG and raster PNG outputs with full diagnostic metadata.

Designed for **drawing preview services**, **engineering document pipelines**, and **CAD data integration**.

## Features

| Capability | Description |
| --- | --- |
| **Drawing Parsing** | Pure-Go parser for DWG (R9–R11, R13–R2018), ASCII / binary DXF, and `dwgread -O JSON` |
| **Text & Encoding** | Extracts TEXT / MTEXT / ATTRIB with full `\U+`, `\M+` (GBK / Big5 / Shift-JIS / EUC-KR), split-MIF, and symbol-name escape decoding |
| **Vector & Raster Rendering** | Shared `PreparedDrawing` pipeline generating both vector SVG and raster PNG with consistent geometry, typography, and viewports |
| **Layouts & Viewports** | Renders model space or named paper-space layouts with orthographic viewport transforms and clipping |
| **Multi-Page SVG** | Example CLI merges paper layouts in tab order into a single vertical SVG |
| **External Resources** | Explicit mapping for external reference DWGs (`XrefFiles`), TrueType/OpenType fonts (`FontMap` / `FontDir`), and raster images (`ResourceDir`) |
| **Drawing Export** | DXF and DWG writers preserving original symbol names and container metadata where supported |
| **Diagnostics & Audit** | Structured audit reports for font substitutions, missing glyphs, external references, skipped records, and entity-to-primitive provenance |

## Requirements

- **Go 1.25 or later** (zero CGO required).
- For CJK and multilingual text rendering, configure appropriate system or local TTF / OTF / TTC fonts via `FontMap` or `FontDir`.
- Drawings with external references (Xrefs) require local access to referenced DWG files via explicit mapping (`XrefFiles`); no implicit network lookups are performed.

## Quick Start

### Install as a Go Library

```console
go get github.com/boyunsz/go-cad
```

### Run the Example CLI

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

Replace `/path/to/drawing.dwg` with an actual DWG file. The command outputs model space, each paper layout, a combined multi-page SVG, PNG previews, and diagnostic JSON reports under `example/output/`.

- `-combine-svg` automatically includes all paper layouts in tab order (falling back to model space if none exist).
- `-style clear` renders monochrome black ink; `-fill-mode outline` outlines solid HATCH fills while preserving arrows and pattern hatches. Use `-style original -fill-mode source` to keep original colors and solid fills.
- `-render-only` skips DXF round-trip verification while keeping rendering and diagnostic output.

For single-layout export, font/Xref mapping files, and all CLI flags, see the [CLI Guide](example/README.md).

## Go API

The following example parses a DWG file and renders model space to a vector SVG:

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

Run it inside your module:

```console
go run . "/path/to/drawing.dwg" drawing.svg
```

- Call `prepared.PNG()` to reuse the exact same prepared geometry and typography for raster output.
- Inspect `prepared.Audit` for rendering diagnostics and `doc.Skipped()` for records skipped during parsing.
- Enable `Plot: true` to respect layer plot visibility, and configure `FontMap`, `FontDir`, and `XrefFiles` for custom fonts and external references.

### Key APIs

| API | Purpose |
| --- | --- |
| `cad.Parse(data)` | Parse a DWG byte slice |
| `cad.ParseDXF(data)` | Parse ASCII or binary DXF |
| `cad.ParseJSON(data)` | Import `dwgread -O JSON` data |
| `doc.Texts()` | Extract texts from model space and block references |
| `doc.Layouts()` | Query layout names, tab order, and space metadata |
| `cad.PrepareDrawing(doc, opts)` | Build shared geometry, typography, viewport, and diagnostics |
| `prepared.SVG()` / `prepared.PNG()` | Export vector SVG or raster PNG from the prepared scene |
| `cad.DetectSheets(doc)` | Heuristically detect drawing frames in model space |
| `cad.SheetTexts(doc)` | Extract texts with sheet frame attribution |
| `cad.WriteDXF(doc, writer)` | Export document to DXF |
| `cad.WriteDwg(doc, writer)` | Export document to DWG based on available container data |

> [!NOTE]
> When calling `cad.RenderSVG` or `cad.RenderPNG` directly, set `RenderOptions.Fidelity = "faithful"` to use the shared `PrepareDrawing` pipeline; omitting it uses the legacy preview pipeline. Calling `cad.PrepareDrawing` directly always uses the faithful pipeline.

## Compatibility & Rendering Scope

DWG reading covers file signatures for **R9, R10, R11**, and **R13, R14, R2000, R2004, R2007, R2010, R2013, R2018**. Entity, object, and export coverage varies by version—validate against representative drawings and audit diagnostics.

| Area | Current Behavior |
| --- | --- |
| **Fonts** | Supports TTF / OTF / TTC fonts; SHX and Big Fonts require substitute fonts and may differ in glyph shape |
| **Plot Styles** | Supports layer plot visibility and parsed linetypes/lineweights; CTB / STB plot style tables are not yet emulated |
| **Text Layout** | Supports common TEXT / MTEXT formatting; advanced paragraph tabs, indentation, and complex shaping remain limited |
| **Curves** | Tessellated by `CurveTolerance`; global error bounds are not guaranteed for arbitrary rational splines |
| **External Resources** | Requires accessible local files; matched Xref read failures terminate conversion, while other missing resources are recorded in diagnostics |
| **Space Selection** | Paper layouts use source viewports; model-space sheet detection is heuristic frame segmentation |

## Project Structure

```text
go-cad/
├── cad.go                 # Public library API
├── go.mod                 # Root Go module (zero CGO)
├── example/               # Standalone CLI module
├── internal/
│   ├── bitstream/         # Bitstream I/O and checksums
│   ├── container/         # DWG containers, pages, and compression
│   ├── objrec/            # Object records and type codes
│   ├── entity/            # Entity models and field decoders
│   ├── object/            # Non-entity objects and symbol tables
│   ├── drawing/           # Document model, importers, layouts, and expansion
│   ├── render/            # Fonts, external resources, SVG / PNG rendering
│   ├── writer/            # DWG encoding and writing
│   └── testfixtures/      # Optional fixture test configuration
├── .github/workflows/     # GitHub Actions CI
├── CONTRIBUTING.md        # Development and verification guide
├── README.md              # English documentation
├── README.zh-CN.md        # Simplified Chinese documentation
└── LICENSE                # Apache License 2.0
```

The root module exposes the library API (`github.com/boyunsz/go-cad`). The `example/` directory is a separate module referencing the parent via a local `replace` directive.

## Development & Verification

Run the test and build checks from the repository root:

```console
go test ./...
go -C example test ./...
go build ./...
go -C example build -o ../bin/cad-example .
```

CI runs tests and builds for both modules on every push and pull request. Optional fixture-based regression tests are automatically skipped when fixtures are not configured. See [CONTRIBUTING.md](CONTRIBUTING.md) for fixture setup, coding conventions, and issue reporting guidelines.

## Provenance & License

Maintained from [unitedrhino/go-cad](https://github.com/unitedrhino/go-cad) at commit `ddd91eea9618cc1a2feec32ab6397239d1f9f315`.

Licensed under the [Apache License 2.0](LICENSE).
