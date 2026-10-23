package ocr

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"
)

// TestLayersMatchONNXRuntime 逐算子比对 Go 实现与 onnxruntime 的中间张量。
//
// 黄金文件由 scripts/dump_layers.py 生成（内部同时带上了预处理后的输入数组），
// 因此这里用的是与 Python 完全相同的输入，能精确指出是哪个算子先偏离。
//
// 用法: OCR_LAYERS=/path/to/layers.bin go test -run TestLayersMatchONNXRuntime ./internal/ocr/
func TestLayersMatchONNXRuntime(t *testing.T) {
	path := os.Getenv("OCR_LAYERS")
	if path == "" {
		t.Skip("未设置 OCR_LAYERS，跳过逐算子比对")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8 || string(raw[:8]) != "CXLAYER\x00" {
		t.Fatal("黄金文件 magic 不匹配")
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

	type golden struct {
		shape []int
		kind  Kind
		f32   []float32
		i64   []int64
		i32   []int32
		u8    []uint8
		i8    []int8
	}
	goldens := map[string]*golden{}
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

		g := &golden{shape: shape}
		switch dt {
		case 0:
			g.kind = KindF32
			g.f32 = make([]float32, nb/4)
			for k := range g.f32 {
				g.f32[k] = math.Float32frombits(binary.LittleEndian.Uint32(body[k*4:]))
			}
		case 2:
			g.kind = KindI64
			g.i64 = make([]int64, nb/8)
			for k := range g.i64 {
				g.i64[k] = int64(binary.LittleEndian.Uint64(body[k*8:]))
			}
		case 3:
			g.kind = KindU8
			g.u8 = append([]uint8(nil), body...)
		case 4:
			g.kind = KindI8
			g.i8 = make([]int8, nb)
			for k := range g.i8 {
				g.i8[k] = int8(body[k])
			}
		case 5:
			g.kind = KindI32
			g.i32 = make([]int32, nb/4)
			for k := range g.i32 {
				g.i32[k] = int32(binary.LittleEndian.Uint32(body[k*4:]))
			}
		default:
			continue
		}
		goldens[name] = g
	}

	g, err := Model()
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]*Tensor{g.Inputs[0]: NewF32([]int{1, 1, h, w}, inFloats)}

	// 记录每个节点执行后的第一次偏差
	reported := map[string]bool{}
	var intBad, floatBad int
	var firstIntBad, firstFloatBad string

	_, err = g.RunTrace(in, func(node *Node, env Env) {
		for _, oname := range node.Outputs {
			gold, ok := goldens[oname]
			if !ok || reported[oname] {
				continue
			}
			got, ok := env[oname]
			if !ok {
				continue
			}
			reported[oname] = true
			if len(got.Shape) != len(gold.shape) {
				if firstIntBad == "" {
					firstIntBad = fmt.Sprintf("%s(%s): 维数 %v 期望 %v", oname, node.Op, got.Shape, gold.shape)
				}
				intBad++
				return
			}
			for i := range got.Shape {
				if got.Shape[i] != gold.shape[i] {
					if firstIntBad == "" {
						firstIntBad = fmt.Sprintf("%s(%s): 形状 %v 期望 %v", oname, node.Op, got.Shape, gold.shape)
					}
					intBad++
					return
				}
			}
			// 整数张量必须完全一致（量化卷积/矩阵乘都是精确整数运算）；
			// 浮点张量允许亚 ULP 级差异（如 Div 的舍入路径不同），按相对误差判。
			var maxDiff, maxRel float64
			var at int
			for i := 0; i < got.NumElem(); i++ {
				var d, ref float64
				switch gold.kind {
				case KindF32:
					gv, wv := float64(got.F32[i]), float64(gold.f32[i])
					d = math.Abs(gv - wv)
					ref = math.Abs(wv)
				case KindI64:
					d = math.Abs(float64(got.I64[i] - gold.i64[i]))
				case KindU8:
					d = math.Abs(float64(got.U8[i]) - float64(gold.u8[i]))
				case KindI8:
					d = math.Abs(float64(got.I8[i]) - float64(gold.i8[i]))
				case KindI32:
					d = math.Abs(float64(got.I32[i] - gold.i32[i]))
				}
				rel := d
				if ref > 1 {
					rel = d / ref
				}
				if d > maxDiff {
					maxDiff, at = d, i
				}
				if rel > maxRel {
					maxRel = rel
				}
			}
			if gold.kind != KindF32 {
				if maxDiff != 0 {
					intBad++
					if firstIntBad == "" {
						neq := 0
						for i := 0; i < got.NumElem(); i++ {
							switch gold.kind {
							case KindI64:
								if got.I64[i] != gold.i64[i] {
									neq++
								}
							case KindU8:
								if got.U8[i] != gold.u8[i] {
									neq++
								}
							case KindI8:
								if got.I8[i] != gold.i8[i] {
									neq++
								}
							case KindI32:
								if got.I32[i] != gold.i32[i] {
									neq++
								}
							}
						}
						firstIntBad = fmt.Sprintf("%s(%s) %v: 有 %d/%d 个元素不等，最大偏差 %.6g",
							oname, node.Op, got.Shape, neq, got.NumElem(), maxDiff)
					}
				}
			} else if maxRel > 1e-5 {
				floatBad++
				if firstFloatBad == "" {
					firstFloatBad = fmt.Sprintf("%s(%s) %v: 最大相对偏差 %.3g @ 第%d个元素",
						oname, node.Op, got.Shape, maxRel, at)
				}
			}
		}
	})
	if err != nil {
		t.Fatalf("执行图失败: %v", err)
	}

	t.Logf("已比对 %d 个张量：整型不等 %d 个，浮点超差 %d 个", len(goldens), intBad, floatBad)
	t.Logf("按执行顺序，首个浮点超差: %s", firstFloatBad)
	t.Logf("按执行顺序，首个整型不等: %s", firstIntBad)
	if firstIntBad != "" {
		t.Fatalf("首个整型张量不一致（这是硬错误）: %s", firstIntBad)
	}
	if firstFloatBad != "" {
		t.Fatalf("首个浮点张量超差: %s", firstFloatBad)
	}
	t.Log("全部中间张量与 onnxruntime 一致")
}
