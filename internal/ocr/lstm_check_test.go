package ocr

import (
	"math"
	"testing"
)

// TestLSTMAgainstGoldenInput 用黄金张量作为 LSTM 的输入，检验 opLSTM。
// 输入相同的情况下若仍偏离 ORT，则 LSTM 实现本身有问题。
func TestLSTMAgainstGoldenInput(t *testing.T) {
	path := goldenPath(t)
	gold, in, w, h := readLayers(t, path)
	g, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	gold["input1"] = &goldT{shape: []int{1, 1, h, w}, kind: KindF32, f32: in}

	for _, node := range g.Nodes {
		if node.Op != "LSTM" {
			continue
		}
		inName := node.Inputs[0]
		gx, ok := gold[inName]
		if !ok {
			t.Fatalf("黄金里没有 LSTM 输入 %s", inName)
		}
		want, ok := gold[node.Outputs[0]]
		if !ok {
			t.Fatalf("黄金里没有 LSTM 输出 %s", node.Outputs[0])
		}

		env := Env{inName: NewF32(gx.shape, gx.f32)}
		for i := 1; i < len(node.Inputs); i++ {
			if node.Inputs[i] == "" {
				continue
			}
			if t0, ok := g.Initializers[node.Inputs[i]]; ok {
				env[node.Inputs[i]] = t0
			} else if t0, ok := gold[node.Inputs[i]]; ok {
				env[node.Inputs[i]] = NewF32(t0.shape, t0.f32)
			}
		}
		t.Logf("LSTM 输入 %s %v, W %v, R %v", inName, gx.shape,
			g.Initializers[node.Inputs[1]].Shape, g.Initializers[node.Inputs[2]].Shape)

		if err := opLSTM(env, node); err != nil {
			t.Fatalf("LSTM 执行失败: %v", err)
		}
		got := env[node.Outputs[0]]
		if len(got.Shape) != len(want.shape) {
			t.Fatalf("输出形状 %v 期望 %v", got.Shape, want.shape)
		}
		var maxAbs, maxRel float64
		for i := range got.F32 {
			d := math.Abs(float64(got.F32[i]) - float64(want.f32[i]))
			if d > maxAbs {
				maxAbs = d
			}
			ref := math.Abs(float64(want.f32[i]))
			if ref > 1e-3 {
				if r := d / ref; r > maxRel {
					maxRel = r
				}
			}
		}
		t.Logf("LSTM 输出 %s %v: 最大绝对偏差 %.4e 最大相对 %.4e", node.Outputs[0], got.Shape, maxAbs, maxRel)

		// 输出值域，便于判断偏差量级
		mn, mx := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, v := range want.f32 {
			if v < mn {
				mn = v
			}
			if v > mx {
				mx = v
			}
		}
		t.Logf("黄金输出值域 [%.4f, %.4f]", mn, mx)

		if maxAbs > 1e-4 {
			t.Errorf("LSTM 用相同输入仍偏离 ORT %.4e", maxAbs)
		}
	}
}
