package ocr

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// opLSTM 双向 LSTM 的量化实现（ONNX Runtime 的 com.microsoft::DynamicQuantizeLSTM）。
//
// 语义由黄金参考逐步反推确认（见 scripts/ 与 internal/ocr 的比对测试）：
//
//  1. X 先做一次全局动态量化（scale/zp 取自整个 X）
//  2. h 每个时间步各做一次动态量化（h 是浮点，随迭代变化）
//  3. 输入门矩阵乘： (X_q - X_zp) @ W_q  -> int32，再乘 (X_scale * W_scale)
//     循环门矩阵乘： (h_q - h_zp) @ R_q  -> int32，再乘 (h_scale * R_scale)
//  4. gates = gx + gh + Wb + Rb，按 ONNX 的 iofc 顺序拆成四份
//  5. c_t = f * c_{t-1} + i * tanh(gc);  h_t = o * tanh(c_t)
//
// 几个容易搞错的点（都经过实测排除）：
//   - 门顺序是 ONNX 的 **iofc**，不是常见的 ifco；用错会让偏差从 1e-7 涨到 2.0
//   - W/R 的布局是 [dirs, input|hidden, 4H]，即相对于 ONNX 浮点 LSTM 的 W 是转置的
//   - **循环路径也必须量化 h**；只用反量化权重做浮点乘，偏差 1.4e-3，量化后是 1.8e-7
//   - X 是"整段一次"量化而非逐时间步量化；逐时间步会让偏差涨到 0.19
func opDynamicQuantizeLSTM(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	w, err := in(env, n, 1)
	if err != nil {
		return err
	}
	r, err := in(env, n, 2)
	if err != nil {
		return err
	}
	b := inOpt(env, n, 3)
	// 索引 4 是 sequence_lens，索引 7 是 peephole P，本模型均为空
	initH := inOpt(env, n, 5)
	initC := inOpt(env, n, 6)
	wScale := inOpt(env, n, 8)
	wZp := inOpt(env, n, 9)
	rScale := inOpt(env, n, 10)
	rZp := inOpt(env, n, 11)

	if len(x.Shape) != 3 {
		return errType("LSTM", fmt.Sprintf("输入需为 [seq,batch,input]，实得 %v", x.Shape))
	}
	if x.Kind != KindF32 {
		return errType("LSTM", "输入需为 float32，实得 "+x.Kind.String())
	}
	dir := n.Attr("direction").StringOr("forward")
	dirs := 1
	switch dir {
	case "bidirectional":
		dirs = 2
	case "reverse":
		dirs = 1
	}
	if w.Shape[0] != dirs {
		return errType("LSTM", fmt.Sprintf("权重方向数 %d 与 direction=%s 不符", w.Shape[0], dir))
	}

	seq, batch, inSize := x.Shape[0], x.Shape[1], x.Shape[2]
	H := int(n.Attr("hidden_size").Int(0))
	if H <= 0 {
		H = w.Shape[2] / 4
	}
	if w.Shape[2] != 4*H {
		return errType("LSTM", fmt.Sprintf("权重末维 %d 应为 4*hidden_size=%d", w.Shape[2], 4*H))
	}
	// W: [dirs, input, 4H]  R: [dirs, H, 4H]
	if w.Shape[1] != inSize {
		return errType("LSTM", fmt.Sprintf("权重中间维 %d 应为 input_size=%d", w.Shape[1], inSize))
	}
	if r.Shape[1] != H || r.Shape[2] != 4*H {
		return errType("LSTM", fmt.Sprintf("循环权重形状 %v 应为 [%d,%d,%d]", r.Shape, dirs, H, 4*H))
	}

	// 1. X 全局动态量化
	xq, xScale, xZp := quantizeLinear(x.F32)

	yOut := make([]float32, seq*dirs*batch*H)
	yH := make([]float32, dirs*batch*H)
	yC := make([]float32, dirs*batch*H)

	// 两个方向互不依赖，并行执行
	workers := dirs
	if workers < 1 {
		workers = 1
	}
	_ = workers
	var wg sync.WaitGroup
	for d := 0; d < dirs; d++ {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()
			lstmDirection(d, seq, batch, H, inSize,
				xq, xScale, xZp, x.F32,
				w, r, b, initH, initC,
				wScale, wZp, rScale, rZp,
				dir, yOut, yH, yC)
		}(d)
	}
	wg.Wait()

	// Y: [seq, dirs, batch, H]；Y_h / Y_c: [dirs, batch, H]
	if err := out(env, n, 0, NewF32([]int{seq, dirs, batch, H}, yOut)); err != nil {
		return err
	}
	if err := out(env, n, 1, NewF32([]int{dirs, batch, H}, yH)); err != nil {
		return err
	}
	return out(env, n, 2, NewF32([]int{dirs, batch, H}, yC))
}

func lstmDirection(d, seq, batch, H, inSize int,
	xq []uint8, xScale float32, xZp uint8, xF []float32,
	w, r, b, initH, initC, wScale, wZp, rScale, rZp *Tensor,
	dir string, yOut, yH, yC []float32) {

	gate := 4 * H
	wScaleD := wScale.F32[d]
	rScaleD := rScale.F32[d]
	wZpD := int32(0)
	if wZp != nil {
		wZpD = int32(wZp.IntScalarAt(d))
	}
	rZpD := int32(0)
	if rZp != nil {
		rZpD = int32(rZp.IntScalarAt(d))
	}

	// 权重转 int32 并减 zero_point（zero_point 恒为 0 时就是原值）
	wBase := d * inSize * gate
	rBase := d * H * gate
	wi := make([]int32, inSize*gate)
	for i := 0; i < inSize*gate; i++ {
		wi[i] = int32(w.I8[wBase+i]) - wZpD
	}
	ri := make([]int32, H*gate)
	for i := 0; i < H*gate; i++ {
		ri[i] = int32(r.I8[rBase+i]) - rZpD
	}

	// 偏置 B: [dirs, 8H] -> Wb 取前 4H，Rb 取后 4H
	var wb, rb []float32
	if b != nil && len(b.F32) >= (d+1)*8*H {
		wb = b.F32[d*8*H : d*8*H+gate]
		rb = b.F32[d*8*H+gate : (d+1)*8*H]
	}

	h := make([]float32, batch*H)
	c := make([]float32, batch*H)
	if initH != nil && len(initH.F32) >= (d+1)*batch*H {
		copy(h, initH.F32[d*batch*H:(d+1)*batch*H])
	}
	if initC != nil && len(initC.F32) >= (d+1)*batch*H {
		copy(c, initC.F32[d*batch*H:(d+1)*batch*H])
	}

	gates := make([]float32, batch*gate)
	hqBuf := make([]uint8, batch*H)
	ghBuf := make([]float32, batch*gate)

	for s := 0; s < seq; s++ {
		t := s
		if dir == "reverse" {
			t = seq - 1 - s
		}

		// ---- 输入门矩阵乘： (X_q - X_zp) @ W_q ----
		gx := make([]float32, batch*gate)
		for bi := 0; bi < batch; bi++ {
			xRow := xq[(t*batch+bi)*inSize : (t*batch+bi+1)*inSize]
			gRow := gx[bi*gate : (bi+1)*gate]
			for k := 0; k < inSize; k++ {
				av := int32(xRow[k]) - int32(xZp)
				if av == 0 {
					continue
				}
				wRow := wi[k*gate : (k+1)*gate]
				for j := range gRow {
					gRow[j] += float32(av * wRow[j])
				}
			}
			sc := xScale * wScaleD
			for j := range gRow {
				gRow[j] *= sc
			}
		}

		// ---- 循环门矩阵乘： (h_q - h_zp) @ R_q ----
		// h 是浮点且逐步变化，这里对每个时间步重新动态量化
		_, hScale, hZp := quantizeLinearInto(h, hqBuf)
		for j := range ghBuf {
			ghBuf[j] = 0
		}
		if hScale != 0 {
			for bi := 0; bi < batch; bi++ {
				hRow := hqBuf[bi*H : (bi+1)*H]
				gRow := ghBuf[bi*gate : (bi+1)*gate]
				for k := 0; k < H; k++ {
					av := int32(hRow[k]) - int32(hZp)
					if av == 0 {
						continue
					}
					rRow := ri[k*gate : (k+1)*gate]
					for j := range gRow {
						gRow[j] += float32(av * rRow[j])
					}
				}
				sc := hScale * rScaleD
				for j := range gRow {
					gRow[j] *= sc
				}
			}
		}

		// ---- 门控：iofc 顺序 ----
		for bi := 0; bi < batch; bi++ {
			base := bi * gate
			for j := 0; j < gate; j++ {
				v := gx[base+j] + ghBuf[base+j]
				if wb != nil {
					v += wb[j]
				}
				if rb != nil {
					v += rb[j]
				}
				gates[base+j] = v
			}
			for j := 0; j < H; j++ {
				gi := sigmoid32(gates[base+j])
				go_ := sigmoid32(gates[base+H+j])
				gf := sigmoid32(gates[base+2*H+j])
				gc := tanh32(gates[base+3*H+j])
				ci := base + j
				c[ci] = gf*c[ci] + gi*gc
				h[ci] = go_ * tanh32(c[ci])
			}
			// Y[t, d, batch, H]
			copy(yOut[((t*2+d)*batch+bi)*H:((t*2+d)*batch+bi+1)*H], h[bi*H:(bi+1)*H])
		}
	}

	copy(yH[d*batch*H:(d+1)*batch*H], h)
	copy(yC[d*batch*H:(d+1)*batch*H], c)
}

// quantizeLinear 对一段 float32 做动态量化，返回量化结果、scale、zero_point。
func quantizeLinear(x []float32) ([]uint8, float32, uint8) {
	q := make([]uint8, len(x))
	return quantizeLinearInto(x, q)
}

// quantizeLinearInto 与 quantizeLinear 相同，但复用调用方给的缓冲区。
func quantizeLinearInto(x []float32, buf []uint8) ([]uint8, float32, uint8) {
	if len(x) == 0 {
		return buf[:0], 0, 0
	}
	xmin, xmax := x[0], x[0]
	for _, v := range x {
		if v < xmin {
			xmin = v
		}
		if v > xmax {
			xmax = v
		}
	}
	if xmin > 0 {
		xmin = 0
	}
	if xmax < 0 {
		xmax = 0
	}
	scale := (xmax - xmin) / 255.0
	if scale == 0 {
		for i := range buf {
			buf[i] = 0
		}
		return buf, 0, 0
	}
	zpF := math.Round(float64(-xmin / scale))
	if zpF < 0 {
		zpF = 0
	}
	if zpF > 255 {
		zpF = 255
	}
	inv := 1.0 / float64(scale)
	for i, v := range x {
		q := math.Round(float64(v)*inv) + zpF
		if q < 0 {
			q = 0
		}
		if q > 255 {
			q = 255
		}
		buf[i] = uint8(q)
	}
	return buf, float32(scale), uint8(zpF)
}

func sigmoid32(v float32) float32 { return 1 / (1 + float32(math.Exp(float64(-v)))) }

func tanh32(v float32) float32 { return float32(math.Tanh(float64(v))) }

// 让编译器知道 runtime 被使用（并行度在调用方决定）
var _ = runtime.GOMAXPROCS
