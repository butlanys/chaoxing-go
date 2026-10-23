package ocr

import (
	"bytes"
	"fmt"
	"image"
	"math"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// 本文件复刻 ddddocr 的图像预处理，目标是**逐像素**与 Python 版一致：
//
//	img = Image.open(bytes).convert("RGB")          # 丢弃 alpha
//	th = 64; tw = int(img.width * 64 / img.height)
//	img = img.resize((tw, th), Image.LANCZOS)
//	img = img.convert("L")
//	arr = np.array(img).astype(float32) / 255.0     # -> [1,1,64,tw]
//
// 两处细节必须对齐，否则识别结果会漂：
//  1. RGB->L 用的是 16 位定点式 L = (r*19595 + g*38470 + b*7471 + 0x8000) >> 16
//     （不是教科书上的 (r*299+g*587+b*114)/1000，实测两者在部分像素上差 1）
//  2. PIL 的 LANCZOS 是定点实现：系数用 22 位精度量化、两次分离卷积中间结果
//     落到 uint8 并截断，所以不能用浮点版 Lanczos 直接替代

const (
	// pilPrecisionBits 对应 PIL Resample.c 的 PRECISION_BITS (32 - 8 - 2)
	pilPrecisionBits = 22
	// lanczosSupport Lanczos 滤波器的支撑半径
	lanczosSupport = 3.0
)

// lanczosFilter 是 PIL 使用的 Lanczos 核。
func lanczosFilter(x float64) float64 {
	if x == 0.0 {
		return 1.0
	}
	if x < -lanczosSupport || x > lanczosSupport {
		return 0.0
	}
	px := math.Pi * x
	return lanczosSupport * math.Sin(px) * math.Sin(px/lanczosSupport) / (px * px)
}

// coeffs 是一维重采样系数：每个输出位置一组（起始下标 + 22 位定点的整数系数）。
type coeffs struct {
	ksize  int
	xmin   []int
	xmax   []int
	k      []int32
	offset int
}

// precomputeCoeffs 复刻 PIL 的 precompute_coeffs + normalize_coeffs_8bpc。
func precomputeCoeffs(inSize, outSize int) *coeffs {
	scale := float64(inSize) / float64(outSize)
	filterscale := scale
	if filterscale < 1.0 {
		filterscale = 1.0
	}
	support := lanczosSupport * filterscale
	ksize := int(math.Ceil(support))*2 + 1

	c := &coeffs{
		ksize: ksize,
		xmin:  make([]int, outSize),
		xmax:  make([]int, outSize),
		k:     make([]int32, outSize*ksize),
	}
	ss := 1.0 / filterscale

	for xx := 0; xx < outSize; xx++ {
		center := (float64(xx) + 0.5) * scale
		xmin := int(center - support + 0.5)
		if xmin < 0 {
			xmin = 0
		}
		xmax := int(center + support + 0.5)
		if xmax > inSize {
			xmax = inSize
		}
		c.xmin[xx] = xmin
		c.xmax[xx] = xmax - xmin

		// 先算浮点系数并归一化，再量化到 22 位定点（与 PIL 顺序一致）
		row := make([]float64, xmax-xmin)
		var ww float64
		for i := range row {
			w := lanczosFilter((float64(i+xmin) - center + 0.5) * ss)
			row[i] = w
			ww += w
		}
		if ww != 0 {
			for i := range row {
				row[i] /= ww
			}
		}
		for i, w := range row {
			// PIL: (int)(v * (1<<PRECISION_BITS) + 0.5)，C 的截断是向零取整
			c.k[xx*ksize+i] = int32(w*float64(int64(1)<<pilPrecisionBits) + 0.5)
		}
	}
	return c
}

// resampleHorizontal 水平方向重采样，中间结果落回 uint8（与 PIL 一致）。
func resampleHorizontal(src []uint8, inW, inH, outW, bands int, c *coeffs) []uint8 {
	out := make([]uint8, outW*inH*bands)
	half := int32(1) << (pilPrecisionBits - 1)
	for y := 0; y < inH; y++ {
		rowIn := src[y*inW*bands:]
		rowOut := out[y*outW*bands:]
		for x := 0; x < outW; x++ {
			xmin := c.xmin[x]
			n := c.xmax[x]
			k := c.k[x*c.ksize : x*c.ksize+n]
			for ch := 0; ch < bands; ch++ {
				var ss int32
				for i := 0; i < n; i++ {
					ss += k[i] * int32(rowIn[(xmin+i)*bands+ch])
				}
				ss += half
				ss >>= pilPrecisionBits
				if ss < 0 {
					ss = 0
				} else if ss > 255 {
					ss = 255
				}
				rowOut[x*bands+ch] = uint8(ss)
			}
		}
	}
	return out
}

// resampleVertical 垂直方向重采样，输出同样落回 uint8。
func resampleVertical(src []uint8, inW, inH, outH, bands int, c *coeffs) []uint8 {
	out := make([]uint8, inW*outH*bands)
	half := int32(1) << (pilPrecisionBits - 1)
	for y := 0; y < outH; y++ {
		ymin := c.xmin[y]
		n := c.xmax[y]
		k := c.k[y*c.ksize : y*c.ksize+n]
		rowOut := out[y*inW*bands:]
		for x := 0; x < inW; x++ {
			for ch := 0; ch < bands; ch++ {
				var ss int32
				for i := 0; i < n; i++ {
					ss += k[i] * int32(src[((ymin+i)*inW+x)*bands+ch])
				}
				ss += half
				ss >>= pilPrecisionBits
				if ss < 0 {
					ss = 0
				} else if ss > 255 {
					ss = 255
				}
				rowOut[x*bands+ch] = uint8(ss)
			}
		}
	}
	return out
}

// resizeLanczosPIL 复刻 PIL 的 Image.resize(..., LANCZOS)：先水平后垂直，
// 两趟中间结果都是 uint8。输入输出都是 RGB 交错、每像素 3 字节。
func resizeLanczosPIL(src []uint8, inW, inH, outW, outH int) []uint8 {
	const bands = 3
	cur := src
	curW, curH := inW, inH
	if outW != inW {
		cur = resampleHorizontal(cur, curW, curH, outW, bands, precomputeCoeffs(inW, outW))
		curW = outW
	}
	if outH != curH {
		cur = resampleVertical(cur, curW, curH, outH, bands, precomputeCoeffs(inH, outH))
		curH = outH
	}
	return cur
}

// rgbToGrayPIL 复刻 PIL 的 convert("L")：16 位定点亮度式。
func rgbToGrayPIL(rgb []uint8, w, h int) []uint8 {
	out := make([]uint8, w*h)
	for i := 0; i < w*h; i++ {
		r := int32(rgb[i*3])
		g := int32(rgb[i*3+1])
		b := int32(rgb[i*3+2])
		out[i] = uint8((r*19595 + g*38470 + b*7471 + 0x8000) >> 16)
	}
	return out
}

// decodeToRGB 解码图片并转成 RGB 交错字节（等价 PIL 的 convert("RGB")，直接丢 alpha）。
func decodeToRGB(data []byte) ([]uint8, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("图片解码失败: %w", err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("图片尺寸非法: %dx%d", w, h)
	}
	rgb := make([]uint8, w*h*3)
	switch src := img.(type) {
	case *image.RGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := src.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				i := (y*w + x) * 3
				rgb[i] = src.Pix[o]
				rgb[i+1] = src.Pix[o+1]
				rgb[i+2] = src.Pix[o+2]
			}
		}
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := src.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				i := (y*w + x) * 3
				rgb[i] = src.Pix[o]
				rgb[i+1] = src.Pix[o+1]
				rgb[i+2] = src.Pix[o+2]
			}
		}
	default:
		// 其它颜色模型统一走标准转换（YCbCr 的 JPEG 会走到这里）
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				i := (y*w + x) * 3
				rgb[i] = uint8(r >> 8)
				rgb[i+1] = uint8(g >> 8)
				rgb[i+2] = uint8(bl >> 8)
			}
		}
	}
	return rgb, w, h, nil
}

// Preprocess 复刻 ddddocr 的预处理，返回 [1,1,64,W] 的 float32 数据与宽度。
func Preprocess(imgBytes []byte) ([]float32, int, error) {
	rgb, w, h, err := decodeToRGB(imgBytes)
	if err != nil {
		return nil, 0, err
	}
	targetH := 64
	targetW := int(float64(w) * (float64(targetH) / float64(h)))
	if targetW < 1 {
		targetW = 1
	}
	rs := resizeLanczosPIL(rgb, w, h, targetW, targetH)
	gray := rgbToGrayPIL(rs, targetW, targetH)
	f := make([]float32, targetW*targetH)
	for i, v := range gray {
		f[i] = float32(v) / 255.0
	}
	return f, targetW, nil
}
