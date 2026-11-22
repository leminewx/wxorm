//go:build ignore

// 该程序用于生成 README 使用的品牌图，不参与框架构建。
//
// 用法（在仓库根目录执行）：
//
//	go run ./harness/tools/generate_logo.go ./docs/assets/logo.png
//
// 画布与 leego 保持一致：48 单位逻辑坐标渲染到 1024 像素，再按 4 倍盒式
// 下采样输出 256 像素，保证两张 README 配图在视觉上同源。
package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// 画布配置：逻辑坐标 48 单位，渲染 1024 像素后下采样到 256 像素。
const (
	canvasSize = 1024
	outputSize = 256
	unitScale  = float64(canvasSize) / 48.0
	downscale  = canvasSize / outputSize
	samples    = 3 // 每像素 3×3 超采样，用于抗锯齿
)

type point struct{ x, y float64 }

type rgb struct{ r, g, b float64 }

// stroke 描述一条圆头折线笔画，按对角渐变着色。
type stroke struct {
	path  []point
	width float64
	solid bool
	ink   rgb
}

// disk 描述一个实心圆，用于右下角的强调点。
type disk struct {
	center point
	radius float64
	ink    rgb
}

// style 描述一张品牌图：圆角方底 + 渐变边框 + 字形。
type style struct {
	bg      rgb
	from    rgb
	to      rgb
	strokes []stroke
	disks   []disk
}

var (
	navy   = rgb{16.0 / 255, 23.0 / 255, 51.0 / 255}
	goCyan = rgb{91.0 / 255, 140.0 / 255, 255.0 / 255}
	teal   = rgb{41.0 / 255, 211.0 / 255, 162.0 / 255}
)

// mix 按比例混合两端颜色。
func mix(from, to rgb, t float64) rgb {
	return rgb{from.r + (to.r-from.r)*t, from.g + (to.g-from.g)*t, from.b + (to.b-from.b)*t}
}

// tone 返回该点在对角渐变中的颜色，与 leego 标识使用同一渐变方向。
func (st style) tone(p point) rgb {
	t := (p.x + p.y - 14) / 56
	return mix(st.from, st.to, math.Max(0, math.Min(1, t)))
}

// inRoundRect 判断点是否落在圆角矩形内。
func inRoundRect(p point, x0, y0, x1, y1, radius float64) bool {
	cx := math.Max(x0+radius, math.Min(p.x, x1-radius))
	cy := math.Max(y0+radius, math.Min(p.y, y1-radius))
	return math.Hypot(p.x-cx, p.y-cy) <= radius
}

// segDistance 返回点到线段的距离。
func segDistance(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	length := dx*dx + dy*dy
	t := 0.0
	if length > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/length))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

// polyDistance 返回点到折线的最短距离。
func polyDistance(p point, path []point) float64 {
	shortest := math.Inf(1)
	for i := 0; i+1 < len(path); i++ {
		if d := segDistance(p, path[i], path[i+1]); d < shortest {
			shortest = d
		}
	}
	return shortest
}

// sample 返回逻辑坐标点的颜色与不透明度。
func sample(st style, p point) (rgb, float64) {
	// 边框：外圈透明，1.5~3.5 之间为渐变描边，内部填充深色底。
	if !inRoundRect(p, 1.5, 1.5, 46.5, 46.5, 13) {
		return rgb{}, 0
	}
	if !inRoundRect(p, 3.5, 3.5, 44.5, 44.5, 11) {
		return st.tone(p), 1
	}

	for _, item := range st.strokes {
		if polyDistance(p, item.path) <= item.width/2 {
			if item.solid {
				return item.ink, 1
			}
			return st.tone(p), 1
		}
	}

	for _, item := range st.disks {
		if math.Hypot(p.x-item.center.x, p.y-item.center.y) <= item.radius {
			return item.ink, 1
		}
	}

	return st.bg, 1
}

// render 逐像素超采样生成图像。
func render(st style) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	total := float64(samples * samples)

	for y := 0; y < canvasSize; y++ {
		for x := 0; x < canvasSize; x++ {
			var sum rgb
			var alpha float64

			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/samples) / unitScale
					py := (float64(y) + (float64(sy)+0.5)/samples) / unitScale

					c, a := sample(st, point{px, py})
					sum.r += c.r * a
					sum.g += c.g * a
					sum.b += c.b * a
					alpha += a
				}
			}

			if alpha == 0 {
				continue
			}

			offset := y*img.Stride + x*4
			img.Pix[offset] = toByte(sum.r / alpha)
			img.Pix[offset+1] = toByte(sum.g / alpha)
			img.Pix[offset+2] = toByte(sum.b / alpha)
			img.Pix[offset+3] = toByte(alpha / total)
		}
	}

	return img
}

// toByte 把 0~1 的分量换算为 0~255。
func toByte(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// downsample 把渲染结果按 4×4 盒式滤波缩到 outputSize。
//
// 颜色按 alpha 加权后再除总 alpha，避免透明边缘在平均值里被“提亮”。
func downsample(src *image.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, outputSize, outputSize))
	total := uint64(downscale * downscale)

	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			var r, g, b, a uint64

			for sy := 0; sy < downscale; sy++ {
				for sx := 0; sx < downscale; sx++ {
					offset := (y*downscale+sy)*src.Stride + (x*downscale+sx)*4
					alpha := uint64(src.Pix[offset+3])
					r += uint64(src.Pix[offset]) * alpha
					g += uint64(src.Pix[offset+1]) * alpha
					b += uint64(src.Pix[offset+2]) * alpha
					a += alpha
				}
			}

			if a == 0 {
				continue
			}

			to := y*img.Stride + x*4
			img.Pix[to] = uint8(r / a)
			img.Pix[to+1] = uint8(g / a)
			img.Pix[to+2] = uint8(b / a)
			img.Pix[to+3] = uint8(a / total)
		}
	}

	return img
}

// writePNG 渲染并保存品牌图。
func writePNG(name string, st style) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}

	file, err := os.Create(name)
	if err != nil {
		return err
	}

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err = encoder.Encode(file, downsample(render(st))); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

// wxormStyle 返回 wxorm 标识：圆角方 + 字形 W + 青绿箭头 + 句点。
//
// 构图沿用 leego 的「主字形 + 右上强调 + 右下圆点」结构，只是把主字形
// 换成 W，既保留系列感，又能一眼区分两个项目。
func wxormStyle() style {
	return style{
		bg:   navy,
		from: goCyan,
		to:   teal,
		strokes: []stroke{
			// W：四段折线，外沿 9.5~28.5，底部落在 32.5，保证与右侧箭头留白。
			{path: []point{{9.5, 16}, {14.2, 32.5}, {19, 22.5}, {23.8, 32.5}, {28.5, 16}}, width: 4.2},
			// 箭头：与 leego 相同的 > 造型，用实色 teal 提升小尺寸辨识度。
			{path: []point{{32.5, 14.8}, {37.1, 19}, {32.5, 23.2}}, width: 3.2, solid: true, ink: teal},
		},
		disks: []disk{{center: point{34.5, 30.5}, radius: 2.5, ink: goCyan}},
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: generate_logo <file.png>")
		os.Exit(2)
	}

	if err := writePNG(os.Args[1], wxormStyle()); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	fmt.Println("written:", os.Args[1])
}
