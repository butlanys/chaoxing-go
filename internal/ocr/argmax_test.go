package ocr

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"
)

// TestFinalLogitsArgmax 对比 Go 与 onnxruntime 最终输出的 argmax 序列。
//
// 逐层比对的噪声会在 argmax 处被放大或抹平，所以这里直接看"最终判了哪个字"，
// 能区分"只是浮点噪声"与"结构性错误"。
//
// 黄金文件需由**裁剪后**的模型导出（scripts/dump_layers.py），
// 这样两边输出类别数一致，可以直接比。
func TestFinalLogitsArgmax(t *testing.T) {
	path := os.Getenv("OCR_LAYERS")
	if path == "" {
		t.Skip("未设置 OCR_LAYERS")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pos := 8
	w := int(binary.LittleEndian.Uint32(raw[pos:]))
	h := int(binary.LittleEndian.Uint32(raw[pos+4:]))
	pos += 8
	inFloats := make([]float32, w*h)
	for i := range inFloats {
		inFloats[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[pos+i*4:]))
	}
	pos += w * h * 4

	var gold387 []float32
	var goldShape []int
	n := int(binary.LittleEndian.Uint32(raw[pos:]))
	pos += 4
	for i := 0; i < n; i++ {
		nameLen := int(raw[pos])
		pos++
		name := string(raw[pos : pos+nameLen])
		pos += nameLen
		nd := int(raw[pos])
		pos++
		shape := make([]int, nd)
		for d := range shape {
			shape[d] = int(int64(binary.LittleEndian.Uint64(raw[pos:])))
			pos += 8
		}
		dt := raw[pos]
		pos++
		nb := int(binary.LittleEndian.Uint64(raw[pos:]))
		pos += 8
		body := raw[pos : pos+nb]
		pos += nb
		if name == "387" && dt == 0 {
			goldShape = shape
			gold387 = make([]float32, nb/4)
			for k := range gold387 {
				gold387[k] = math.Float32frombits(binary.LittleEndian.Uint32(body[k*4:]))
			}
		}
	}
	if gold387 == nil {
		t.Fatal("黄金文件里没有最终输出 387")
	}

	g, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	outs, err := g.Run(map[string]*Tensor{g.Inputs[0]: NewF32([]int{1, 1, h, w}, inFloats)})
	if err != nil {
		t.Fatal(err)
	}
	got := outs[g.Outputs[0]]

	if len(goldShape) != len(got.Shape) {
		t.Fatalf("维数不同: 黄金 %v, Go %v", goldShape, got.Shape)
	}
	for i := range goldShape {
		if goldShape[i] != got.Shape[i] {
			t.Fatalf("形状不同: 黄金 %v, Go %v", goldShape, got.Shape)
		}
	}
	seq, classes := goldShape[0], goldShape[2]
	if classes != len(g.Charset) {
		t.Fatalf("黄金类别数 %d 与 Go 字符集 %d 不一致，黄金文件是否用了裁剪后的模型？", classes, len(g.Charset))
	}

	argmax := func(data []float32, t int) (int, float32, float32) {
		base := t * classes
		best, second := 0, 0
		for c := 1; c < classes; c++ {
			if data[base+c] > data[base+best] {
				second = best
				best = c
			} else if data[base+c] > data[base+second] {
				second = c
			}
		}
		return best, data[base+best], data[base+second]
	}

	diffCount := 0
	maxAbs := float32(0)
	for step := 0; step < seq; step++ {
		for c := 0; c < classes; c++ {
			d := got.F32[step*classes+c] - gold387[step*classes+c]
			if d < 0 {
				d = -d
			}
			if d > maxAbs {
				maxAbs = d
			}
		}
		gi, gv, gs := argmax(gold387, step)
		oi, ov, os_ := argmax(got.F32, step)
		if gi != oi {
			diffCount++
			t.Logf("  t=%2d 黄金=%2d(%q 值%.4f 次%.4f)  Go=%2d(%q 值%.4f 次%.4f)  **不同**",
				step, gi, g.Charset[gi], gv, gs, oi, g.Charset[oi], ov, os_)
		}
	}
	t.Logf("logits 最大绝对偏差 %.6g", maxAbs)
	t.Logf("argmax 不同的时间步: %d/%d", diffCount, seq)
	if diffCount > 0 {
		t.Errorf("有 %d 个时间步的 argmax 与 onnxruntime 不同", diffCount)
	}
	_ = fmt.Sprint
}
