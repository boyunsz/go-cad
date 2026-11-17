package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type preparedImage struct {
	Pixels  *image.NRGBA
	Encoded string
	Matrix  [6]float64
	Bounds  drawing.Box2
	Path    string
}

// prepareImage 按资源目录、源图目录及绝对路径查找 IMAGEDEF 对应的位图。
// 校验文件大小和像素预算，应用图片裁切并生成像素、PNG 数据及世界坐标变换；
// 资源不可用、尺寸无效或反向裁切不受支持时返回错误。
func prepareImage(r *drawing.RasterGeometry, opts RenderOptions) (*preparedImage, error) {
	source := strings.ReplaceAll(r.Path, "\\", "/")
	var candidates []string
	if opts.ResourceDir != "" {
		candidates = append(candidates, filepath.Join(opts.ResourceDir, filepath.Base(source)))
	}
	if !filepath.IsAbs(source) && !strings.Contains(source, ":") && opts.SourceDir != "" {
		candidates = append(candidates, filepath.Join(opts.SourceDir, source))
	}
	if filepath.IsAbs(source) {
		candidates = append(candidates, source)
	}
	var data []byte
	var path string
	for _, c := range candidates {
		st, e := os.Stat(c)
		if e == nil && !st.IsDir() && st.Size() <= 64*1024*1024 {
			data, e = os.ReadFile(c)
			if e == nil {
				path = c
				break
			}
		}
	}
	if path == "" {
		return nil, fmt.Errorf("IMAGEDEF %X resource unavailable: %q", r.Image.ImageDef, r.Path)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
		return nil, fmt.Errorf("raster resource exceeds 32M pixels")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	e := r.Image
	if e.ImageSize.X <= 0 || e.ImageSize.Y <= 0 || !finite(e.ImageSize.X) || !finite(e.ImageSize.Y) {
		return nil, fmt.Errorf("invalid raster dimensions")
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	if e.ClipMode != 0 {
		return nil, fmt.Errorf("inverted raster clipping requires unsupported clip mode")
	}
	// Source image clipping coordinates use pixel centers and lower-left origin.
	inside := func(x, y float64) bool {
		if !e.Clipping || len(e.ClipVerts) < 2 {
			return true
		}
		v := e.ClipVerts
		if e.ClipBoundaryType == 1 {
			return x >= math.Min(v[0].X, v[1].X) && x <= math.Max(v[0].X, v[1].X) && y >= math.Min(v[0].Y, v[1].Y) && y <= math.Max(v[0].Y, v[1].Y)
		}
		ok := false
		j := len(v) - 1
		for i, a := range v {
			b := v[j]
			if (a.Y > y) != (b.Y > y) && x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
				ok = !ok
			}
			j = i
		}
		return ok
	}
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			sx := (float64(x)+.5)*e.ImageSize.X/float64(cfg.Width) - .5
			sy := e.ImageSize.Y - .5 - (float64(y)+.5)*e.ImageSize.Y/float64(cfg.Height)
			if inside(sx, sy) {
				pixels.Set(x, y, src.At(src.Bounds().Min.X+x, src.Bounds().Min.Y+y))
			}
		}
	}
	origin := e.Pt0
	origin.X -= .5 * e.Uvec.X
	origin.Y -= .5 * e.Uvec.Y
	origin.Z -= .5 * e.Uvec.Z
	origin.X += (e.ImageSize.Y - .5) * e.Vvec.X
	origin.Y += (e.ImageSize.Y - .5) * e.Vvec.Y
	origin.Z += (e.ImageSize.Y - .5) * e.Vvec.Z
	p := r.Transform.Project(origin)
	u := e.Uvec
	v := e.Vvec
	pu := r.Transform.Project(entity.Point3{X: origin.X + u.X*e.ImageSize.X/float64(cfg.Width), Y: origin.Y + u.Y*e.ImageSize.X/float64(cfg.Width), Z: origin.Z + u.Z*e.ImageSize.X/float64(cfg.Width)})
	pv := r.Transform.Project(entity.Point3{X: origin.X - v.X*e.ImageSize.Y/float64(cfg.Height), Y: origin.Y - v.Y*e.ImageSize.Y/float64(cfg.Height), Z: origin.Z - v.Z*e.ImageSize.Y/float64(cfg.Height)})
	m := [6]float64{pu.X - p.X, pu.Y - p.Y, pv.X - p.X, pv.Y - p.Y, p.X, p.Y}
	bbox := emptyBounds()
	for _, q := range [][2]float64{{0, 0}, {float64(cfg.Width), 0}, {0, float64(cfg.Height)}, {float64(cfg.Width), float64(cfg.Height)}} {
		bbox.Extend(m[0]*q[0]+m[2]*q[1]+m[4], m[1]*q[0]+m[3]*q[1]+m[5])
	}
	var b bytes.Buffer
	if err = png.Encode(&b, pixels); err != nil {
		return nil, err
	}
	return &preparedImage{Pixels: pixels, Encoded: base64.StdEncoding.EncodeToString(b.Bytes()), Matrix: m, Bounds: bbox, Path: path}, nil
}

// paintImage 将准备好的位图仿射映射到画布并作透明叠加；im 为 nil 时不绘制。
func (p *PreparedDrawing) paintImage(dst *image.RGBA, im *preparedImage) {
	if im == nil {
		return
	}
	m := im.Matrix
	s := float64(p.Audit.Width) / (p.Audit.Bounds.MaxX - p.Audit.Bounds.MinX)
	mat := f64.Aff3{m[0] * s, m[2] * s, (m[4] - p.Audit.Bounds.MinX) * s, -m[1] * s, -m[3] * s, (p.Audit.Bounds.MaxY - m[5]) * s}
	xdraw.BiLinear.Transform(dst, mat, im.Pixels, im.Pixels.Bounds(), xdraw.Over, nil)
}
