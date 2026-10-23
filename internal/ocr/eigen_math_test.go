package ocr

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

// TestEigenSigmoidMatchesORT 用 onnxruntime 的 Sigmoid 真值表验证移植的 Eigen exp。
//
// 真值表由 scripts/ 里的方法生成：构造只含 Sigmoid 的最小 ONNX 模型，
// 输入 np.linspace(-30,30,200001)，取 ORT 的实际输出。
//
// 这是"能否与 onnxruntime 位级对齐"的判决性实验：
// 如果 eigenSigmoid 与 ORT 逐位一致，整条流水线就能做到零偏差；
// 如果仍有 1 ULP 差异，说明位级对齐不可行，只能量化端到端一致率。
//
// 用法: OCR_SIGMOID_TRUTH=/tmp/sigmoid_ort.bin go test -run TestEigenSigmoidMatchesORT
func TestEigenSigmoidMatchesORT(t *testing.T) {
	path := os.Getenv("OCR_SIGMOID_TRUTH")
	if path == "" {
		t.Skip("未设置 OCR_SIGMOID_TRUTH")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := int(binary.LittleEndian.Uint32(raw))
	xs := make([]float32, n)
	ys := make([]float32, n)
	for i := 0; i < n; i++ {
		xs[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4+i*4:]))
		ys[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4+n*4+i*4:]))
	}

	type cand struct {
		name string
		fn   func(float32) float32
	}
	cands := []cand{
		{"Eigen exp 移植", eigenSigmoid},
		{"float64 math.Exp", func(x float32) float32 {
			return 1 / (1 + float32(math.Exp(float64(-x))))
		}},
	}

	for _, c := range cands {
		neq := 0
		maxUlp := 0
		var firstAt int = -1
		for i := 0; i < n; i++ {
			got := c.fn(xs[i])
			if got != ys[i] {
				neq++
				if firstAt < 0 {
					firstAt = i
				}
				d := int(int64(int32(math.Float32bits(got))) - int64(int32(math.Float32bits(ys[i]))))
				if d < 0 {
					d = -d
				}
				if d > maxUlp {
					maxUlp = d
				}
			}
		}
		pct := 100 * float64(neq) / float64(n)
		if neq == 0 {
			t.Logf("  ✅ %-18s 与 onnxruntime **逐位完全一致** (%d 组)", c.name, n)
		} else {
			t.Logf("  ❌ %-18s 不等 %d/%d (%.2f%%)  最大 ULP 距离 %d",
				c.name, neq, n, pct, maxUlp)
			if firstAt >= 0 {
				t.Logf("       首个不同: x=%v  ORT=%v  ours=%v",
					xs[firstAt], ys[firstAt], c.fn(xs[firstAt]))
			}
		}
	}
}
