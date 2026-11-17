// Package testfixtures 加载可选的私有回归测试配置。
//
// 测试通过匿名标识查询样本路径或参数，避免将本机路径及图纸名称
// 写入源码。未配置样本时，由依赖该样本的测试决定跳过行为。
package testfixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// config loads a private JSON map. Relative file entries resolve against
// its directory; non-file entries, such as layout names, are returned unchanged.
// GO_CAD_FIXTURES overrides the repository's ignored .local/fixtures.json.
func config(t testing.TB) (map[string]string, string) {
	t.Helper()
	path := os.Getenv("GO_CAD_FIXTURES")
	if path == "" {
		dir, err := os.Getwd()
		if err != nil {
			t.Fatal("cannot locate optional regression configuration")
		}
		for {
			candidate := filepath.Join(dir, ".local", "fixtures.json")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			} else if !os.IsNotExist(err) {
				t.Fatal("cannot access optional regression configuration")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				t.Skip("private regression fixtures are not configured")
			}
			dir = parent
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read private regression configuration")
	}
	var values map[string]string
	if err := json.Unmarshal(b, &values); err != nil {
		t.Fatal("private regression configuration must be a JSON object of strings")
	}
	return values, filepath.Dir(path)
}

// Value returns a fixture setting, or skips the test when that alias is absent.
func Value(t testing.TB, alias string) string {
	t.Helper()
	values, _ := config(t)
	value := values[alias]
	if value == "" {
		t.Skipf("private fixture setting %q is not configured", alias)
	}
	return value
}

// Path resolves an optional fixture file. Missing files skip the test; other
// access failures fail it. Diagnostics identify the alias rather than the path.
func Path(t testing.TB, alias string) string {
	t.Helper()
	values, base := config(t)
	path := values[alias]
	if path == "" {
		t.Skipf("private fixture %q is not configured", alias)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Skipf("private fixture %q is unavailable", alias)
	}
	if err != nil || info.IsDir() {
		t.Fatalf("private fixture %q is not a readable file", alias)
	}
	return path
}
