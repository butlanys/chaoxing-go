package ocr

import (
	_ "embed"
	"fmt"
	"sync"
)

// ddddocrBin 是内嵌的 ddddocr 模型（ORT 动态量化版 common_old.onnx 的裁剪结果）。
// 由 scripts/convert_ddddocr.py 生成，可用 --charset full 还原完整 8210 类版本。
//
//go:embed data/ddddocr.bin
var ddddocrBin []byte

var (
	modelOnce sync.Once
	modelG    *Graph
	modelErr  error
)

// Model 返回内嵌模型解析出的计算图（首次调用时解析，之后复用）。
func Model() (*Graph, error) {
	modelOnce.Do(func() {
		modelG, modelErr = ParseModel(ddddocrBin)
	})
	return modelG, modelErr
}

// ModelSize 返回内嵌模型字节数，便于自检与文档。
func ModelSize() int { return len(ddddocrBin) }

// Result 是一次识别的结果。
type Result struct {
	// Text 识别出的字符串（CTC 贪心解码后）
	Text string
	// Indices 每个时间步的 argmax 类别下标（已去掉连续重复与 blank）
	Indices []int
	// Steps 时间步总数
	Steps int
	// MinMargin 所有保留时间步中，最大类与次大类 logit 之差的最小值。
	// 越小说明越接近"犹豫"，可用于判断是否需要重试。
	MinMargin float32
}

// Recognizer 复用已加载的模型做识别。可安全并发使用。
type Recognizer struct {
	g *Graph
}

// New 构造识别器（模型只解析一次，多个 Recognizer 共享）。
func New() (*Recognizer, error) {
	g, err := Model()
	if err != nil {
		return nil, err
	}
	return &Recognizer{g: g}, nil
}

// Charset 返回识别使用的字符表（索引 0 为 CTC blank）。
func (r *Recognizer) Charset() []string { return r.g.Charset }

// Recognize 识别一张验证码图片（PNG/JPEG/GIF 字节）。
func (r *Recognizer) Recognize(imgBytes []byte) (Result, error) {
	pix, w, err := Preprocess(imgBytes)
	if err != nil {
		return Result{}, err
	}
	in := map[string]*Tensor{
		r.g.Inputs[0]: NewF32([]int{1, 1, 64, w}, pix),
	}
	outs, err := r.g.Run(in)
	if err != nil {
		return Result{}, err
	}
	logits, ok := outs[r.g.Outputs[0]]
	if !ok {
		return Result{}, fmt.Errorf("模型没有产出输出 %q", r.g.Outputs[0])
	}
	return r.decode(logits)
}

// decode 做 CTC 贪心解码：跳过连续重复，跳过 blank（索引 0）。
// 与 Python 版 _ctc_decode_indices 的行为一致。
func (r *Recognizer) decode(logits *Tensor) (Result, error) {
	shape := logits.Shape
	if len(shape) != 3 {
		return Result{}, fmt.Errorf("输出应为 [seq,1,classes]，实得 %v", shape)
	}
	seq, classes := shape[0], shape[2]
	if shape[1] != 1 {
		return Result{}, fmt.Errorf("batch 维应为 1，实得 %d", shape[1])
	}
	if classes != len(r.g.Charset) {
		return Result{}, fmt.Errorf("输出类别数 %d 与字符表 %d 不一致", classes, len(r.g.Charset))
	}

	res := Result{Steps: seq, MinMargin: float32(1e30)}
	prev := -1
	for t := 0; t < seq; t++ {
		base := t * classes
		best, second := -1, -1
		var bestV, secondV float32
		for c := 0; c < classes; c++ {
			v := logits.F32[base+c]
			if best < 0 || v > bestV {
				second, secondV = best, bestV
				best, bestV = c, v
			} else if second < 0 || v > secondV {
				second, secondV = c, v
			}
		}
		// 与 Python 一致：先判"是否与上一步相同"，再判"是否 blank"
		if best != prev && best != 0 {
			res.Indices = append(res.Indices, best)
			res.Text += r.g.Charset[best]
			if second >= 0 && bestV-secondV < res.MinMargin {
				res.MinMargin = bestV - secondV
			}
		}
		prev = best
	}
	if len(res.Indices) == 0 {
		res.MinMargin = 0
	}
	return res, nil
}

// RecognizeBytes 是包级便捷函数，首次调用会解析内嵌模型。
func RecognizeBytes(imgBytes []byte) (string, error) {
	r, err := New()
	if err != nil {
		return "", err
	}
	res, err := r.Recognize(imgBytes)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
