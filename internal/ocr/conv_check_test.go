package ocr

import (
	"math"
	"os"
	"testing"
)

// TestConvAgainstGoldenInput 直接检验 opConv：用黄金张量作为输入，
// 看 Go 的卷积输出是否与 onnxruntime 一致。
//
// 这样能把"上游误差"排除掉 —— 如果拿完全相同的输入，Go 的卷积仍偏离 ORT，
// 那就是卷积本身的问题，而不是误差传导。
//
// 用法: OCR_LAYERS=<float.pruned 的 layers.bin> go test -run TestConvAgainstGoldenInput
func TestConvAgainstGoldenInput(t *testing.T) {
	path := goldenPath(t)
	gold, in, w, h := readLayers(t, path)
	g, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	gold["input1"] = &goldT{shape: []int{1, 1, h, w}, kind: KindF32, f32: in}

	convIdx := 0
	for _, node := range g.Nodes {
		if node.Op != "Conv" {
			continue
		}
		convIdx++
		// 取该卷积的第一个输入张量（黄金值）与权重（模型自带）
		inName := node.Inputs[0]
		gx, ok := gold[inName]
		if !ok {
			t.Logf("卷积 %d: 黄金里没有输入 %s，跳过", convIdx, inName)
			continue
		}
		wt := g.Initializers[node.Inputs[1]]
		bt := g.Initializers[node.Inputs[2]]
		want, ok := gold[node.Outputs[0]]
		if !ok {
			t.Logf("卷积 %d: 黄金里没有输出 %s，跳过", convIdx, node.Outputs[0])
			continue
		}

		env := Env{
			inName:         NewF32(gx.shape, gx.f32),
			node.Inputs[1]: wt,
			node.Inputs[2]: bt,
		}
		if err := opConv(env, node); err != nil {
			t.Fatalf("卷积 %d 执行失败: %v", convIdx, err)
		}
		got := env[node.Outputs[0]]

		if len(got.Shape) != len(want.shape) {
			t.Errorf("卷积 %d (%s): 形状 %v 期望 %v", convIdx, node.Outputs[0], got.Shape, want.shape)
			continue
		}
		var maxAbs, maxRel float64
		for i := range got.F32 {
			d := math.Abs(float64(got.F32[i]) - float64(want.f32[i]))
			if d > maxAbs {
				maxAbs = d
			}
			ref := math.Abs(float64(want.f32[i]))
			if ref > 1e-6 {
				if r := d / ref; r > maxRel {
					maxRel = r
				}
			}
		}
		status := "OK"
		if maxAbs > 1e-4 {
			status = "**偏离过大**"
		}
		t.Logf("卷积 %2d 输出 %-5s 输入 %-5s 权重%v: 最大绝对偏差 %.4e 最大相对 %.4e  %s",
			convIdx, node.Outputs[0], inName, wt.Shape, maxAbs, maxRel, status)
		if maxAbs > 1e-4 {
			t.Errorf("卷积 %d 用相同输入仍偏离 ORT %.4e，说明卷积实现有问题", convIdx, maxAbs)
		}
	}
	if convIdx == 0 {
		t.Fatal("模型里没有 Conv 节点")
	}
}

func goldenPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("OCR_LAYERS")
	if p == "" {
		t.Skip("未设置 OCR_LAYERS")
	}
	return p
}
