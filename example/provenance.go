package main

import (
	"crypto/sha256"
	"fmt"
	cad "github.com/boyunsz/go-cad"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

// sourceProvenance records input hashes, render options, and build information.
// Its local diagnostic report may include private paths; it is not a release file.
func sourceProvenance(input string, data []byte, p *cad.PreparedDrawing, outputs map[string]string) map[string]any {
	abs, _ := filepath.Abs(input)
	result := map[string]any{"input": abs, "input_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "layout": p.Audit.SelectedSpace, "config": p.Options, "output_sha256": outputs, "fonts": p.Audit.Fonts, "go_version": runtime.Version(), "production_pipeline": "DWG -> Go-CAD -> SVG glyph/geometry paths", "external_inputs": p.Audit.ExternalDrawings, "pdf_used_for_production": false}
	if build, ok := debug.ReadBuildInfo(); ok {
		result["build"] = build.String()
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		result["source_manifest_error"] = "compiled source location unavailable"
		return result
	}
	root := filepath.Dir(file)
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".mod", ".sum", ".sh", ".json", ".md":
		default:
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(b))
		return nil
	})
	if err != nil {
		result["source_manifest_error"] = err.Error()
	} else {
		keys := make([]string, 0, len(files))
		for k := range files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		h := sha256.New()
		for _, k := range keys {
			fmt.Fprintf(h, "%s  %s\n", files[k], k)
		}
		result["source_directory"] = root
		result["source_sha256"] = fmt.Sprintf("%x", h.Sum(nil))
		result["source_files"] = files
	}
	return result
}
