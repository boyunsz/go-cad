package render

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/object"
)

// TestFontCacheBoundariesAndInvalidation 验证字体缓存对缺失、目录、损坏文件的安全处理以及文件更新自动失效。
func TestFontCacheBoundariesAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	if f := parseFontFile(filepath.Join(dir, "missing.ttf")); f != nil {
		t.Fatalf("expected nil for missing font, got %v", f)
	}
	if f := parseFontFile(dir); f != nil {
		t.Fatalf("expected nil for directory path, got %v", f)
	}
	if h := cachedFontSHA256(filepath.Join(dir, "missing.ttf")); h != "" {
		t.Fatalf("expected empty sha256 for missing file, got %q", h)
	}

	// Corrupted file should be safely ignored and refreshed if overwritten later.
	p := filepath.Join(dir, "mutable.ttf")
	if err := os.WriteFile(p, []byte("not-a-font"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := cachedFontSHA256(p)
	if h1 == "" || parseFontFile(p) != nil {
		t.Fatalf("corrupted file handling failed: sha=%q", h1)
	}

	// Copy a real system font over the same path with a newer mtime/size to verify invalidation.
	realFont := testFont(t)
	realPath, _, _ := strings.Cut(realFont, "#")
	raw, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, future, future)

	h2 := cachedFontSHA256(p)
	if h2 == "" || h2 == h1 {
		t.Fatalf("cache did not invalidate after overwrite: h1=%q h2=%q", h1, h2)
	}
	if f := parseFontFile(p); f == nil {
		t.Fatal("expected valid font after overwriting corrupted file")
	}
}

// TestConcurrentPrepareDrawingAndSVG 验证多协程并发调用 PrepareDrawing 与 SVG 无数据竞争且输出一致。
func TestConcurrentPrepareDrawingAndSVG(t *testing.T) {
	f := testFont(t)
	doc := &drawing.Document{
		LayerColors: map[uint64]drawing.LayerColor{1: {Name: "Main", Index: 7}},
		ModelSpace: []any{
			&entity.EntLine{BaseEntity: entity.BaseEntity{Handle: 1, Layer: 1}, Start: entity.Point3{X: 0, Y: 0}, End: entity.Point3{X: 100, Y: 50}},
			&entity.EntText{BaseEntity: entity.BaseEntity{Handle: 2, Layer: 1}, Text: "0123456789 ABC <test> & \"quote\"", Height: 5, Insertion: entity.Point3{X: 10, Y: 20}},
			&entity.EntMText{BaseEntity: entity.BaseEntity{Handle: 3, Layer: 1}, Text: `Line1\PLine2 \S1/2;`, TextHeight: 4, Attachment: 1, Insertion: entity.Point3{X: 10, Y: 40}},
		},
	}
	opts := RenderOptions{Width: 1024, FontPath: f, TextMode: "path", Monochrome: true}

	const workers = 8
	results := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			p, err := PrepareDrawing(doc, opts)
			if err != nil {
				errs[id] = err
				return
			}
			results[id], errs[id] = p.SVG()
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d failed: %v", i, errs[i])
		}
		if i > 0 && !bytes.Equal(results[i], results[0]) {
			t.Fatalf("worker %d produced different SVG output", i)
		}
	}
	var root any
	if err := xml.Unmarshal(results[0], &root); err != nil {
		t.Fatalf("SVG is not valid XML: %v", err)
	}
}

// TestSVGSerializationBoundaries 验证零值、负零、XML 特殊字符及 text 模式下的 SVG 序列化边界。
func TestSVGSerializationBoundaries(t *testing.T) {
	if got := num(-1e-9); got != "0.000000" {
		t.Fatalf("num(-1e-9) = %q, want 0.000000", got)
	}
	if got := num(1e-9); got != "0.000000" {
		t.Fatalf("num(1e-9) = %q, want 0.000000", got)
	}
	if got := esc("plain-ASCII_123 中文"); got != "plain-ASCII_123 中文" {
		t.Fatalf("esc fast path altered string: %q", got)
	}
	if got := esc(`a&b<c>"d"'e'`); got != "a&amp;b&lt;c&gt;&#34;d&#34;&#39;e&#39;" {
		t.Fatalf("esc special chars = %q", got)
	}

	// TextMode="text" with XML special characters must still yield valid XML.
	doc := &drawing.Document{
		LayerColors: map[uint64]drawing.LayerColor{1: {Name: `Layer&<1>`, Index: 7}},
		ModelSpace: []any{
			&entity.EntText{BaseEntity: entity.BaseEntity{Handle: 10, Layer: 1}, Text: `<A & B>`, Height: 3},
		},
	}
	p, err := PrepareDrawing(doc, RenderOptions{Width: 400, FontPath: testFont(t), TextMode: "text"})
	if err != nil {
		t.Fatal(err)
	}
	svg, err := p.SVG()
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := xml.Unmarshal(svg, &parsed); err != nil {
		t.Fatalf("text-mode SVG invalid XML: %v", err)
	}
	if !bytes.Contains(svg, []byte(`data-layer="Layer&amp;&lt;1&gt;"`)) {
		t.Fatalf("escaped layer attribute missing in SVG")
	}
}

// TestXrefLoaderBoundaries 验证 XrefLoader 跳过、报错与无效字节时的边界行为。
func TestXrefLoaderBoundaries(t *testing.T) {
	doc := &drawing.Document{
		InternalObjs: map[uint64]*object.ObjGeneric{
			100: {Name: "BLOCK_HEADER", Fields: []object.ObjField{
				{Key: "name", Val: "part"},
				{Key: "xref_pname", Val: `C:\remote\part.dwg`},
			}},
		},
	}

	// 1. Returning (nil, nil) skips cleanly.
	called := 0
	if _, err := PrepareDrawing(doc, RenderOptions{
		XrefLoader: func(name, declared string) ([]byte, error) {
			called++
			if name != "part" || declared != `C:\remote\part.dwg` {
				t.Errorf("unexpected loader args: name=%q declared=%q", name, declared)
			}
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("skip loader failed: %v", err)
	}
	if called != 1 {
		t.Fatalf("loader called %d times, want 1", called)
	}

	// 2. Returning an error propagates cleanly.
	wantErr := errors.New("network timeout")
	if _, err := PrepareDrawing(doc, RenderOptions{
		XrefLoader: func(name, declared string) ([]byte, error) {
			return nil, wantErr
		},
	}); !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped loader error, got %v", err)
	}

	// 3. Returning invalid DWG bytes fails with parse error.
	if _, err := PrepareDrawing(doc, RenderOptions{
		XrefLoader: func(name, declared string) ([]byte, error) {
			return []byte("not-a-valid-dwg"), nil
		},
	}); err == nil {
		t.Fatal("expected parse error for invalid Xref bytes")
	}
}
