package render

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pathCommand struct {
	Op byte
	P  [3]entity.Point2
}
type laidGlyph struct {
	ColorIndex int
	TrueColor  int64
	Rune       rune
	Path       []pathCommand
	Matrix     [6]float64
	Font       string
	Size       float64
}
type textLayoutPlan struct {
	Glyphs []laidGlyph
	Decor  []drawing.Stroke
	Bounds drawing.Box2
}
type FontUse struct {
	SourceFile                      string `json:",omitempty"`
	Face                            FontFace
	RequestedBold, RequestedItalic  bool
	UsedCharacters                  string
	Source, BigFont, Actual, Reason string
	Handle                          uint64
	Missing                         string
}
type fontResolver struct {
	faces     map[*opentype.Font]FontFace
	hashes    map[string]string
	opts      RenderOptions
	cache     map[string]*opentype.Font
	path      map[string]string
	fallbacks []string
}

// newFontResolver 创建单次渲染使用的字体解析器，初始化字体、路径和诊断缓存。
func newFontResolver(opts RenderOptions) *fontResolver {
	return &fontResolver{faces: map[*opentype.Font]FontFace{}, hashes: map[string]string{}, opts: opts, cache: map[string]*opentype.Font{}, path: map[string]string{}, fallbacks: systemFontCandidates}
}

// choose 按常规字重和非斜体要求选择字体，返回字体、实际路径及替换原因。
func (r *fontResolver) choose(name string) (*opentype.Font, string, string) {
	return r.chooseStyled(name, false, false)
}

// chooseStyled 按显式映射、指定字体路径、字体目录和系统候选顺序查找字体。
// 按名称及粗斜体要求缓存结果；无可用字体时返回 nil、空路径和原因说明。
func (r *fontResolver) chooseStyled(name string, bold, italic bool) (*opentype.Font, string, string) {
	key := fmt.Sprintf("%s|b%t|i%t", name, bold, italic)
	if f, ok := r.cache[key]; ok {
		return f, r.path[key], r.reason(name, r.path[key])
	}
	var paths []string
	for source, path := range r.opts.FontMap {
		if strings.EqualFold(source, name) {
			paths = append(paths, path)
			break
		}
	}
	if r.opts.FontPath != "" {
		paths = append(paths, r.opts.FontPath)
	}
	if name != "" && !strings.EqualFold(filepath.Ext(name), ".shx") {
		if filepath.IsAbs(name) {
			paths = append(paths, name)
		}
		for _, d := range []string{r.opts.FontDir, "/System/Library/Fonts", "/System/Library/Fonts/Supplemental", "/Library/Fonts", "/usr/share/fonts/truetype"} {
			if d != "" {
				variant := name
				if bold {
					variant += " Bold"
				}
				if italic {
					variant += " Italic"
				}
				for _, n := range []string{variant, variant + ".ttf", variant + ".otf", name, name + ".ttf", name + ".otf", name + ".ttc"} {
					paths = append(paths, filepath.Join(d, n))
				}
			}
		}
	}
	paths = append(paths, r.fallbacks...)
	for _, p := range paths {
		file, _, _ := strings.Cut(p, "#")
		if _, e := os.Stat(file); e != nil {
			continue
		}
		if f := parseFontFileStyled(p, bold, italic); f != nil {
			r.cache[key] = f
			r.path[key] = p
			return f, p, r.reason(name, p)
		}
	}
	r.cache[key] = nil
	return nil, "", "no usable font"
}

// reason 结合显式映射与实际选中路径说明字体匹配或替换原因。
func (r *fontResolver) reason(name, path string) string {
	for source, p := range r.opts.FontMap {
		if strings.EqualFold(source, name) {
			if p == path {
				return "explicit font mapping; source glyph equivalence is not assumed"
			}
			return "explicit mapping could not be loaded/selected; system font substitution"
		}
	}
	return fontReason(name, path)
}

// cadFontScale 根据字体大写字高计算 CAD 字高到字形度量的比例。
// 优先使用 CapHeight，再尝试 H 的字形边界；均不可用时返回 1。
func cadFontScale(f *opentype.Font) float64 {
	if f != nil {
		m, err := f.Metrics(nil, fixed.I(1024), font.HintingNone)
		if err == nil && m.CapHeight > 0 {
			return 65536 / float64(m.CapHeight)
		}
		if idx, e := f.GlyphIndex(nil, 'H'); e == nil && idx != 0 {
			if bounds, _, e := f.GlyphBounds(nil, idx, fixed.I(1024), font.HintingNone); e == nil && bounds.Max.Y > bounds.Min.Y {
				return 65536 / float64(bounds.Max.Y-bounds.Min.Y)
			}
		}
	}
	return 1
}

// glyphKern 读取两个字符间的 kerning，并换算为归一化字形单位。
// 字体为空、字形查询失败或字体不提供 kerning 时返回 0。
func glyphKern(f *opentype.Font, a, b rune) float64 {
	if f == nil {
		return 0
	}
	ia, ea := f.GlyphIndex(nil, a)
	ib, eb := f.GlyphIndex(nil, b)
	if ea != nil || eb != nil {
		return 0
	}
	k, e := f.Kern(nil, ia, ib, fixed.I(1024), font.HintingNone)
	if e != nil {
		return 0
	}
	return float64(k) / 65536
}

// fontReason 比较请求字体与实际文件的基名，区分匹配、SHX 替代和系统字体替换。
func fontReason(name, path string) string {
	base := func(s string) string { return strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)) }
	if strings.EqualFold(base(name), base(path)) {
		return "matched font"
	}
	if strings.EqualFold(filepath.Ext(name), ".shx") {
		return "SHX outlines unavailable; system outline substitution"
	}
	return "system font substitution"
}

type localGlyph struct {
	colorIndex          int
	trueColor           int64
	r                   rune
	f                   *opentype.Font
	path                string
	x, y, h, w, slant   float64
	line                int
	under, over, strike bool
}

// layoutFaithfulText 将文字图元解析为带字体轮廓、仿射矩阵和装饰线的排版计划。
// 处理宽度、换行、附着点及内联格式，同时返回字体使用和缺字诊断；无有效字高时返回空计划。
func layoutFaithfulText(p drawing.Primitive, res *fontResolver) (textLayoutPlan, []FontUse) {
	plan := textLayoutPlan{Bounds: emptyBounds()}
	tx := p.Lb.Tx
	if tx == nil || tx.HWorld <= 0 {
		return plan, nil
	}
	runs := drawing.ParseTextRuns(tx.Raw, p.Kind0 == "MTEXT")
	if tx.Raw == "" {
		runs = drawing.ParseTextRuns(p.Lb.Text, false)
	}
	var local []localGlyph
	type localDecor struct {
		a, b entity.Point2
		line int
	}
	var stackDecor []localDecor
	var uses []FontUse
	x, y := 0.0, 0.0
	line := 0
	lineInkLeft := 0.0
	lineFirst := true
	heights := []float64{tx.HWorld}
	baselines := []float64{0}
	widths := []float64{0}
	h := tx.HWorld
	wf := tx.WidthFactor
	if wf <= 0 {
		wf = 1
	}
	effectiveWidth := func(run drawing.TextRun) float64 {
		w := run.Width
		if w <= 0 {
			w = 1
		}
		if run.AbsoluteWidth {
			return w
		}
		return wf * w
	}
	lineFactor := tx.LineFactor
	if lineFactor <= 0 {
		lineFactor = 1
	}
	lineFactor *= 5.0 / 3.0
	newline := func() {
		lineInkLeft = 0
		lineFirst = true
		widths[line] = x
		x = 0
		lineHeight := heights[line]
		if tx.ExactLineSpacing {
			lineHeight = h
		}
		y -= lineHeight * lineFactor
		line++
		heights = append(heights, h)
		baselines = append(baselines, y)
		widths = append(widths, 0)
	}
	advance := func(f *opentype.Font, ch rune) float64 {
		if f == nil {
			return .6
		}
		idx, e := f.GlyphIndex(nil, ch)
		if e != nil {
			return .6
		}
		v, e := f.GlyphAdvance(nil, idx, fixed.I(1024), font.HintingNone)
		if e != nil {
			return .6
		}
		return float64(v) / 65536
	}
	inkRight := func(f *opentype.Font, ch rune) float64 {
		if f == nil {
			return .6
		}
		idx, e := f.GlyphIndex(nil, ch)
		if e != nil {
			return advance(f, ch)
		}
		b, _, e := f.GlyphBounds(nil, idx, fixed.I(1024), font.HintingNone)
		if e != nil {
			return advance(f, ch)
		}
		return float64(b.Max.X) / 65536
	}
	inkLeft := func(f *opentype.Font, ch rune) float64 {
		if f == nil {
			return 0
		}
		idx, e := f.GlyphIndex(nil, ch)
		if e != nil {
			return 0
		}
		b, _, e := f.GlyphBounds(nil, idx, fixed.I(1024), font.HintingNone)
		if e != nil {
			return 0
		}
		return float64(b.Min.X) / 65536
	}
	add := func(text string, run drawing.TextRun, atX, atY, sz float64, move bool) {
		name := tx.FontName
		if run.Font != "" {
			name = run.Font
		}
		f, path, reason := res.chooseStyled(name, run.Bold, run.Italic)
		missing := ""
		fallbackUses := map[string]*FontUse{}
		var previous rune
		var previousFont *opentype.Font
		cursor := atX
		chars := []rune(text)
		wordRune := func(c rune) bool {
			return c < 128 && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') || c == '\u00a0'
		}
		for ci, ch := range chars {
			if ch == '\n' {
				previousFont = nil
				x = cursor
				newline()
				cursor = x
				atY = y
				continue
			}
			gf, gpath := f, path
			if gf != nil {
				idx, e := gf.GlyphIndex(nil, ch)
				if e != nil || idx == 0 {
					for _, alt := range res.fallbacks {
						af := res.fallback(alt)
						if af == nil {
							continue
						}
						ii, ee := af.GlyphIndex(nil, ch)
						if ee == nil && ii != 0 {
							gf = af
							gpath = alt
							break
						}
					}
					ii, _ := gf.GlyphIndex(nil, ch)
					if ii == 0 {
						missing += string(ch)
					}
				}
			} else {
				missing += string(ch)
			}
			if gpath != path {
				u := fallbackUses[gpath]
				if u == nil {
					u = &FontUse{SourceFile: p.Source.File, Source: name, BigFont: tx.BigFont, Actual: gpath, Face: res.face(gf, gpath), Reason: "per-glyph fallback", Handle: p.Source.Handle, RequestedBold: run.Bold, RequestedItalic: run.Italic}
					fallbackUses[gpath] = u
				}
				u.UsedCharacters += string(ch)
			}
			track := run.Tracking
			if track <= 0 {
				track = 1
			}
			emSize := sz * cadFontScale(gf)
			aw := advance(gf, ch) * emSize * effectiveWidth(run) * track
			kern := 0.0
			if previousFont == gf && gf != nil {
				kern = glyphKern(gf, previous, ch) * emSize * effectiveWidth(run)
			}
			cursor += kern
			if ch == '\t' {
				step := 4 * advance(gf, ' ') * emSize * effectiveWidth(run)
				if step > 0 {
					cursor = (math.Floor(cursor/step) + 1) * step
				}
				continue
			}
			// Keep Latin words together. A substituted font may exceed the original
			// rectangle; do not silently split a word or alter its horizontal scale.
			// Compare visible ink against the rectangle; the last advance also
			// includes a blank right bearing, which must not create a false wrap.
			if move && lineFirst {
				if ch != ' ' && ch != '\t' {
					lineInkLeft = inkLeft(gf, ch) * emSize * effectiveWidth(run)
				}
				lineFirst = false
			}
			wrapWidth := inkRight(gf, ch) * emSize * effectiveWidth(run)
			startsWord := wordRune(ch) && (ci == 0 || !wordRune(chars[ci-1]))
			if startsWord {
				wrapWidth = 0
				for j := ci; j < len(chars) && wordRune(chars[j]); j++ {
					last := j+1 == len(chars) || !wordRune(chars[j+1])
					if last {
						wrapWidth += inkRight(gf, chars[j]) * emSize * effectiveWidth(run)
					} else {
						wrapWidth += advance(gf, chars[j]) * emSize * effectiveWidth(run) * track
					}
					if j > ci {
						wrapWidth += glyphKern(gf, chars[j-1], chars[j]) * emSize * effectiveWidth(run)
					}
				}
			}
			canBreak := !wordRune(ch) || startsWord
			if move && canBreak && ch != ' ' && tx.RectWidth > 0 && cursor > 0 && cursor+wrapWidth-lineInkLeft > tx.RectWidth {
				x = cursor
				newline()
				cursor = 0
				previousFont = nil
				lineInkLeft = inkLeft(gf, ch) * emSize * effectiveWidth(run)
				lineFirst = false
				atY = y
			}
			local = append(local, localGlyph{colorIndex: run.ColorIndex, trueColor: run.TrueColor, r: ch, f: gf, path: gpath, x: cursor, y: atY + inlineTextShift(run.VerticalAlign, h, sz), h: emSize, w: effectiveWidth(run), slant: tx.Oblique + run.Oblique, line: line, under: run.Under, over: run.Over, strike: run.Strike})
			cursor += aw
			previous = ch
			previousFont = gf
			if sz > heights[line] {
				heights[line] = sz
			}
		}
		if move {
			x = cursor
			widths[line] = math.Max(widths[line], x)
		}
		uses = append(uses, FontUse{SourceFile: p.Source.File, Source: name, BigFont: tx.BigFont, Actual: path, Reason: reason, Handle: p.Source.Handle, Missing: missing, Face: res.face(f, path), RequestedBold: run.Bold, RequestedItalic: run.Italic})
		keys := make([]string, 0, len(fallbackUses))
		for k := range fallbackUses {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			uses = append(uses, *fallbackUses[k])
		}
	}
	for _, run := range runs {
		if run.Newline {
			newline()
			continue
		}
		rh := run.Height
		if run.AbsoluteHeight {
			rh /= h
		}
		if rh <= 0 {
			rh = 1
		}
		if run.Width <= 0 {
			run.Width = 1
		}
		if run.Top != "" || run.Bottom != "" {
			f, _, _ := res.choose(tx.FontName)
			w1, w2 := 0.0, 0.0
			for _, c := range run.Top {
				w1 += advance(f, c) * cadFontScale(f) * h * rh * .7 * effectiveWidth(run)
			}
			for _, c := range run.Bottom {
				w2 += advance(f, c) * cadFontScale(f) * h * rh * .7 * effectiveWidth(run)
			}
			w := math.Max(w1, w2)
			if run.StackMode == '/' {
				stackDecor = append(stackDecor, localDecor{point(x, y+h*rh*.18), point(x+w, y+h*rh*.18), line})
			}
			if run.StackMode == '#' {
				stackDecor = append(stackDecor, localDecor{point(x+w*.4, y-h*rh*.2), point(x+w*.6, y+h*rh*.8), line})
			}
			add(run.Top, run, x+(w-w1)/2, y+h*rh*.45, h*rh*.7, false)
			add(run.Bottom, run, x+(w-w2)/2, y-h*rh*.3, h*rh*.7, false)
			x += w
			widths[line] = math.Max(widths[line], x)
			continue
		}
		add(run.Text, run, x, y, h*rh, true)
	}
	if tx.FitWidth > 0 && len(widths) == 1 && widths[0] > 0 {
		factor := tx.FitWidth / widths[0]
		for i := range local {
			local[i].x *= factor
			local[i].w *= factor
			if tx.FitScaleHeight {
				local[i].h *= factor
				local[i].w /= factor
				local[i].y *= factor
			}
		}
		widths[0] = tx.FitWidth
		if tx.FitScaleHeight {
			h *= factor
		}
	}
	// Layout bounds in the local text frame, including actual glyph bearings.
	minY, maxY := 0.0, h*.8
	if len(local) > 0 {
		minY = math.Inf(1)
		maxY = math.Inf(-1)
	}
	type outline struct {
		g   localGlyph
		seg sfnt.Segments
	}
	var outlines []outline
	firstInkLine, lastInkLine := line+1, -1
	for _, g := range local {
		if g.f == nil {
			continue
		}
		idx, _ := g.f.GlyphIndex(nil, g.r)
		seg, e := g.f.LoadGlyph(nil, idx, fixed.I(1024), nil)
		if e != nil {
			continue
		}
		copySeg := append(sfnt.Segments(nil), seg...)
		if len(copySeg) > 0 {
			if g.line < firstInkLine {
				firstInkLine = g.line
			}
			if g.line > lastInkLine {
				lastInkLine = g.line
			}
		}
		outlines = append(outlines, outline{g, copySeg})
		for _, s := range copySeg {
			n := 1
			if s.Op == sfnt.SegmentOpQuadTo {
				n = 2
			}
			if s.Op == sfnt.SegmentOpCubeTo {
				n = 3
			}
			for j := 0; j < n; j++ {
				yy := g.y - float64(s.Args[j].Y)/65536*g.h
				minY = math.Min(minY, yy)
				maxY = math.Max(maxY, yy)
			}
		}
	}
	if math.IsInf(minY, 0) {
		minY = 0
		maxY = h
	}
	// Attachment includes explicit empty paragraphs. Ink-only bounds would
	// otherwise pull the first printed row up and erase a leading \P.
	if tx.Attachment > 0 && lastInkLine >= 0 {
		maxY += baselines[0] - baselines[firstInkLine]
		minY += baselines[line] - baselines[lastInkLine]
	}
	yoff := 0.0
	if tx.Attachment > 0 {
		switch (tx.Attachment - 1) / 3 {
		case 0:
			yoff = -maxY
		case 1:
			yoff = -(minY + maxY) / 2
		case 2:
			yoff = -minY
		}
	} else {
		switch tx.VAlign {
		case 1:
			yoff = -minY
		case 2:
			yoff = -(minY + maxY) / 2
		case 3:
			yoff = -maxY
		}
	}
	ux, uy, vx, vy := tx.Ux, tx.Uy, tx.Vx, tx.Vy
	if tx.Gen&2 != 0 {
		ux = -ux
		uy = -uy
	}
	if tx.Gen&4 != 0 {
		vx = -vx
		vy = -vy
	}
	xoff := func(line int) float64 {
		ha := tx.HAlign
		if tx.Attachment > 0 {
			ha = uint16((tx.Attachment - 1) % 3)
		}
		switch ha {
		case 1, 4:
			return -widths[line] / 2
		case 2:
			return -widths[line]
		}
		return 0
	}
	world := func(xx, yy float64) entity.Point2 {
		return entity.Point2{X: p.Lb.X + ux*xx + vx*yy, Y: p.Lb.Y + uy*xx + vy*yy}
	}
	for _, d := range stackDecor {
		a, b := world(d.a.X+xoff(d.line), d.a.Y+yoff), world(d.b.X+xoff(d.line), d.b.Y+yoff)
		plan.Decor = append(plan.Decor, drawing.Stroke{X1: a.X, Y1: a.Y, X2: b.X, Y2: b.Y})
		plan.Bounds.Extend(a.X, a.Y)
		plan.Bounds.Extend(b.X, b.Y)
	}
	for _, o := range outlines {
		g := o.g
		offx, offy := g.x+xoff(g.line), g.y+yoff
		origin := world(offx, offy)
		a := g.h * g.w
		b := math.Tan(g.slant) * g.h
		matrix := [6]float64{ux * a, uy * a, -ux*b - vx*g.h, -uy*b - vy*g.h, origin.X, origin.Y}
		item := laidGlyph{ColorIndex: g.colorIndex, TrueColor: g.trueColor, Rune: g.r, Font: g.path, Size: g.h / cadFontScale(g.f), Matrix: matrix}
		for _, s := range o.seg {
			pc := pathCommand{Op: 'M'}
			n := 1
			switch s.Op {
			case sfnt.SegmentOpLineTo:
				pc.Op = 'L'
			case sfnt.SegmentOpQuadTo:
				pc.Op = 'Q'
				n = 2
			case sfnt.SegmentOpCubeTo:
				pc.Op = 'C'
				n = 3
			}
			for j := 0; j < n; j++ {
				xx, yy := float64(s.Args[j].X)/65536, float64(s.Args[j].Y)/65536
				pc.P[j] = entity.Point2{X: matrix[0]*xx + matrix[2]*yy + matrix[4], Y: matrix[1]*xx + matrix[3]*yy + matrix[5]}
				plan.Bounds.Extend(pc.P[j].X, pc.P[j].Y)
			}
			item.Path = append(item.Path, pc)
		}
		plan.Glyphs = append(plan.Glyphs, item)
		w := advance(g.f, g.r) * g.h * g.w
		for _, d := range []struct {
			yes bool
			y   float64
		}{{g.under, -.12}, {g.over, .9}, {g.strike, .35}} {
			if d.yes {
				a, b := world(offx, offy+d.y*g.h), world(offx+w, offy+d.y*g.h)
				plan.Decor = append(plan.Decor, drawing.Stroke{X1: a.X, Y1: a.Y, X2: b.X, Y2: b.Y})
				plan.Bounds.Extend(a.X, a.Y)
				plan.Bounds.Extend(b.X, b.Y)
			}
		}
	}
	if len(plan.Glyphs) == 0 && len(local) > 0 {
		uses = append(uses, FontUse{Handle: p.Source.Handle, Reason: fmt.Sprintf("no glyph outlines for %d characters", len(local))})
	}
	return plan, uses
}

// inlineTextShift 根据 MTEXT 内联底部、居中或顶部对齐计算纵向偏移。
// base 和 size 使用相同的字高单位；未知对齐值按底部对齐处理。
func inlineTextShift(alignment int, base, size float64) float64 {
	switch alignment {
	case 1:
		return (base - size) / 2
	case 2:
		return base - size
	}
	return 0
}
