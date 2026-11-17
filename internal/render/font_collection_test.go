package render

import (
	"golang.org/x/image/font/sfnt"
	"os"
	"strings"
	"testing"
)

// TestLocalCollectionDefaultRegular 验证字体集的默认成员选择常规字形，避免误选粗体。
func TestLocalCollectionDefaultRegular(t *testing.T) {
	p := "/System/Library/Fonts/Supplemental/Songti.ttc"
	if _, e := os.Stat(p); os.IsNotExist(e) {
		t.Skip("local collection unavailable")
	}
	f := parseFontFile(p)
	if f == nil {
		t.Fatal("no face")
	}
	s, _ := f.Name(nil, sfnt.NameIDSubfamily)
	if !strings.EqualFold(s, "Regular") {
		t.Fatalf("default face must be Regular, got %s", s)
	}
}

// TestFontFaceSelectorAndAudit 验证显式字体成员选择和字体诊断信息，并拒绝损坏的字体文件。
func TestFontFaceSelectorAndAudit(t *testing.T) {
	p := testFont(t)
	f := parseFontFile(p)
	name, _ := f.Name(nil, sfnt.NameIDPostScript)
	if parseFontFile(p+"#"+name) == nil || parseFontFile(p+"#nonexistent-face") != nil {
		t.Fatal("explicit face selector ignored")
	}
	r := newFontResolver(RenderOptions{})
	a := r.face(f, p)
	if len(a.SHA256) != 64 || a.Family == "" || a.PostScript != name || a.Subfamily != "Regular" {
		t.Fatalf("incomplete face audit: %+v", a)
	}
	if e := os.WriteFile(p, []byte("broken font"), 0600); e != nil {
		t.Fatal(e)
	}
	if parseFontFile(p) != nil {
		t.Fatal("malformed font accepted")
	}
}
