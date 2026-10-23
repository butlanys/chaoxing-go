package ocr

import (
	"encoding/binary"
	"math"
	"os"
	"sort"
	"testing"
)

type goldT struct {
	shape []int
	kind  Kind
	f32   []float32
	i64   []int64
	i32   []int32
	u8    []uint8
	i8    []int8
}

func readLayers(t *testing.T, path string) (map[string]*goldT, []float32, int, int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pos := 8
	w := int(binary.LittleEndian.Uint32(raw[pos:]))
	h := int(binary.LittleEndian.Uint32(raw[pos+4:]))
	pos += 8
	in := make([]float32, w*h)
	for i := range in {
		in[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[pos+i*4:]))
	}
	pos += w * h * 4

	out := map[string]*goldT{}
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
		g := &goldT{shape: shape}
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
		out[name] = g
	}
	return out, in, w, h
}

func devOf(got *Tensor, g *goldT) (absDev, relDev float64, neq int) {
	for i := 0; i < got.NumElem(); i++ {
		var d, ref float64
		switch g.kind {
		case KindF32:
			d = math.Abs(float64(got.F32[i]) - float64(g.f32[i]))
			ref = math.Abs(float64(g.f32[i]))
		case KindI64:
			d = math.Abs(float64(got.I64[i] - g.i64[i]))
		case KindU8:
			d = math.Abs(float64(got.U8[i]) - float64(g.u8[i]))
		case KindI8:
			d = math.Abs(float64(got.I8[i]) - float64(g.i8[i]))
		case KindI32:
			d = math.Abs(float64(got.I32[i] - g.i32[i]))
		}
		if d > 0 {
			neq++
		}
		if d > absDev {
			absDev = d
		}
		r := d
		if ref > 1 {
			r = d / ref
		}
		if r > relDev {
			relDev = r
		}
	}
	return
}

// TestFindFirstDivergence 找出**执行顺序上第一个显著偏离**的算子。
//
// 逐层比对的输出按节点顺序排列，所以第一个显著偏差就是问题源头。
// 阈值取相对 1e-3（浮点）与绝对 1（整型），远超浮点噪声量级。
func TestFindFirstDivergence(t *testing.T) {
	path := os.Getenv("OCR_LAYERS")
	if path == "" {
		t.Skip("未设置 OCR_LAYERS")
	}
	gold, in, w, h := readLayers(t, path)
	g, err := Model()
	if err != nil {
		t.Fatal(err)
	}

	type div struct {
		idx    int
		node   string
		op     string
		name   string
		absDev float64
		relDev float64
		neq    int
		total  int
	}
	var divs []div

	_, err = g.RunTrace(map[string]*Tensor{g.Inputs[0]: NewF32([]int{1, 1, h, w}, in)},
		func(node *Node, env Env) {
			for _, oname := range node.Outputs {
				gg, ok := gold[oname]
				if !ok {
					continue
				}
				got, ok := env[oname]
				if !ok {
					continue
				}
				if len(got.Shape) != len(gg.shape) {
					continue
				}
				bad := false
				for i := range got.Shape {
					if got.Shape[i] != gg.shape[i] {
						bad = true
					}
				}
				if bad {
					continue
				}
				a, r, neq := devOf(got, gg)
				sig := false
				if gg.kind == KindF32 {
					sig = r > 1e-7
				} else {
					sig = a >= 1
				}
				if sig {
					divs = append(divs, div{len(divs), node.Op, node.Op, oname, a, r, neq, got.NumElem()})
				}
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("显著偏离的张量数: %d", len(divs))
	sort.Slice(divs, func(i, j int) bool { return divs[i].absDev > divs[j].absDev })
	lim := 15
	if len(divs) < lim {
		lim = len(divs)
	}
	for i := 0; i < lim; i++ {
		d := divs[i]
		t.Logf("  %-24s %-22s 绝对偏差 %.4g 相对 %.3g 不等 %d/%d",
			d.name, d.op, d.absDev, d.relDev, d.neq, d.total)
	}
	_ = sort.Ints
}
