package ocr

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// 本文件是**浮点模型**（ddddocr 的 common.onnx，即 DdddOcr(beta=True) 用的那个）的算子实现。
//
// 为什么同时保留两套算子：
//   量化模型（common_old.onnx）是 Python 原版 chaoxing 实际使用的配置，语义已逐算子反推验证；
//   但它的 DynamicQuantizeLinear 里 round() 不连续，纯 Go 与 onnxruntime 在 sigmoid/tanh
//   上的 1 ULP 浮点差会被"张量极值 -> scale -> 全量量化值"这条链放大，
//   实测 394 张真实验证码上与 Python 只有 47% 一致，不可用。
//   浮点模型没有量化，1 ULP 差不会级联，因此作为实际采用的推理路径。
//
// 两个模型的算子名不冲突（Conv/Gemm/LSTM vs ConvInteger/MatMulInteger/DynamicQuantizeLSTM），
// 所以注册表是两者的超集，加载哪个模型就走哪套算子。

// opConv 浮点卷积。对齐 ONNX Conv：
//
//	y[n,m,oh,ow] = bias[m] + sum_{c,kh,kw} x[n,c,ih,iw] * w[m,c,kh,kw]
func opConv(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	w, err := in(env, n, 1)
	if err != nil {
		return err
	}
	bias := inOpt(env, n, 2)

	if x.Kind != KindF32 || w.Kind != KindF32 {
		return errType("Conv", "需要 float32 输入与权重")
	}
	if len(x.Shape) != 4 || len(w.Shape) != 4 {
		return errType("Conv", fmt.Sprintf("需要 4 维张量，实得 %v 与 %v", x.Shape, w.Shape))
	}

	N, C, H, W := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	M, Cg, kH, kW := w.Shape[0], w.Shape[1], w.Shape[2], w.Shape[3]

	strides := intsOr(n, "strides", []int{1, 1})
	dils := intsOr(n, "dilations", []int{1, 1})
	pads := intsOr(n, "pads", []int{0, 0, 0, 0})
	group := int(n.Attr("group").Int(1))
	if group < 1 {
		group = 1
	}
	if Cg*group != C {
		return errType("Conv", fmt.Sprintf("group=%d 与通道数不匹配", group))
	}
	sh, sw := strides[0], strides[1]
	dh, dw := dils[0], dils[1]
	ph0, pw0, ph1, pw1 := pads[0], pads[1], pads[2], pads[3]

	oH := (H+ph0+ph1-dh*(kH-1)-1)/sh + 1
	oW := (W+pw0+pw1-dw*(kW-1)-1)/sw + 1
	if oH <= 0 || oW <= 0 {
		return errType("Conv", "输出尺寸非法")
	}

	outShape := []int{N, M, oH, oW}
	y := make([]float32, N*M*oH*oW)

	// 按输出通道并行（卷积是最重的算子，实测占整图耗时的大头）
	workers := runtime.GOMAXPROCS(0)
	if workers > M {
		workers = M
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	chunk := (M + workers - 1) / workers
	for wk := 0; wk < workers; wk++ {
		m0 := wk * chunk
		m1 := m0 + chunk
		if m1 > M {
			m1 = M
		}
		if m0 >= m1 {
			continue
		}
		wg.Add(1)
		go func(m0, m1 int) {
			defer wg.Done()
			for nIdx := 0; nIdx < N; nIdx++ {
				for m := m0; m < m1; m++ {
					g := m / (M / group)
					var bv float32
					if bias != nil && m < len(bias.F32) {
						bv = bias.F32[m]
					}
					wBase := m * Cg * kH * kW
					xBase := nIdx * C * H * W
					for oh := 0; oh < oH; oh++ {
						for ow := 0; ow < oW; ow++ {
							// 累加必须在 float64 里做：float32 顺序累加 1000+ 项时，
							// 部分和可达上百，舍入误差会累积到 1e-3 量级；
							// ORT 用分块/向量化累加，精度在 1e-5 以内。
							// 实测：float32 累加时用相同输入仍偏离 ORT 1.3e-3，
							// 改 float64 后降到 1e-5 以内。
							acc := float64(bv)
							for c := 0; c < Cg; c++ {
								xPlane := xBase + (g*Cg+c)*H*W
								wPlane := wBase + c*kH*kW
								for i := 0; i < kH; i++ {
									ih := oh*sh - ph0 + i*dh
									if ih < 0 || ih >= H {
										continue
									}
									rowBase := xPlane + ih*W
									wRow := wPlane + i*kW
									for j := 0; j < kW; j++ {
										iw := ow*sw - pw0 + j*dw
										if iw < 0 || iw >= W {
											continue
										}
										acc += float64(x.F32[rowBase+iw]) * float64(w.F32[wRow+j])
									}
								}
							}
							y[((nIdx*M+m)*oH+oh)*oW+ow] = float32(acc)
						}
					}
				}
			}
		}(m0, m1)
	}
	wg.Wait()

	return out(env, n, 0, NewF32(outShape, y))
}

// opGemm 浮点矩阵乘加。对齐 ONNX Gemm：y = alpha * A' * B' + beta * C
func opGemm(env Env, n *Node) error {
	a, err := in(env, n, 0)
	if err != nil {
		return err
	}
	b, err := in(env, n, 1)
	if err != nil {
		return err
	}
	c := inOpt(env, n, 2)
	if a.Kind != KindF32 || b.Kind != KindF32 {
		return errType("Gemm", "需要 float32")
	}
	if len(a.Shape) != 2 || len(b.Shape) != 2 {
		return errType("Gemm", fmt.Sprintf("需要 2 维张量，实得 %v 与 %v", a.Shape, b.Shape))
	}
	alpha := n.Attr("alpha").F
	if n.Attr("alpha").Type != attrFloat {
		alpha = 1
	}
	beta := n.Attr("beta").F
	if n.Attr("beta").Type != attrFloat {
		beta = 1
	}
	transA := n.Attr("transA").Int(0) != 0
	transB := n.Attr("transB").Int(0) != 0

	M, K := a.Shape[0], a.Shape[1]
	if transA {
		M, K = a.Shape[1], a.Shape[0]
	}
	Kb, N := b.Shape[0], b.Shape[1]
	if transB {
		Kb, N = b.Shape[1], b.Shape[0]
	}
	if K != Kb {
		return errType("Gemm", fmt.Sprintf("K 维不匹配: %d 与 %d", K, Kb))
	}

	getA := func(i, j int) float32 {
		if transA {
			return a.F32[j*a.Shape[1]+i]
		}
		return a.F32[i*a.Shape[1]+j]
	}
	getB := func(i, j int) float32 {
		if transB {
			return b.F32[j*b.Shape[1]+i]
		}
		return b.F32[i*b.Shape[1]+j]
	}

	y := make([]float32, M*N)
	workers := runtime.GOMAXPROCS(0)
	if workers > M {
		workers = M
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	chunk := (M + workers - 1) / workers
	for wk := 0; wk < workers; wk++ {
		r0 := wk * chunk
		r1 := r0 + chunk
		if r1 > M {
			r1 = M
		}
		if r0 >= r1 {
			continue
		}
		wg.Add(1)
		go func(r0, r1 int) {
			defer wg.Done()
			for i := r0; i < r1; i++ {
				// 同 Conv：累加放 float64，避免 float32 顺序累加在大 K 下的精度损失
				acc := make([]float64, N)
				for k := 0; k < K; k++ {
					av := float64(getA(i, k)) * float64(alpha)
					if av == 0 {
						continue
					}
					for j := 0; j < N; j++ {
						acc[j] += av * float64(getB(k, j))
					}
				}
				row := y[i*N : i*N+N]
				for j := 0; j < N; j++ {
					v := acc[j]
					if c != nil && beta != 0 {
						v += float64(beta) * float64(c.F32[j])
					}
					row[j] = float32(v)
				}
			}
		}(r0, r1)
	}
	wg.Wait()

	return out(env, n, 0, NewF32([]int{M, N}, y))
}

// opLSTM 标准 ONNX 浮点 LSTM（无 peephole）。
//
// W 布局 [dirs, 4H, input]，门顺序 iofc（ONNX 约定）：
//
//	gates = W x_t + R h_{t-1} + Wb + Rb
//	i, o, f, c~ = 按 iofc 拆四份
//	c_t = f * c_{t-1} + i * tanh(c~)
//	h_t = o * tanh(c_t)
func opLSTM(env Env, n *Node) error {
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
	initH := inOpt(env, n, 5)
	initC := inOpt(env, n, 6)

	if x.Kind != KindF32 || w.Kind != KindF32 || r.Kind != KindF32 {
		return errType("LSTM", "需要 float32")
	}
	if len(x.Shape) != 3 {
		return errType("LSTM", fmt.Sprintf("输入需为 [seq,batch,input]，实得 %v", x.Shape))
	}
	dir := n.Attr("direction").StringOr("forward")
	dirs := 1
	if dir == "bidirectional" {
		dirs = 2
	}
	seq, batch, inSize := x.Shape[0], x.Shape[1], x.Shape[2]
	H := int(n.Attr("hidden_size").Int(0))
	if H <= 0 {
		H = w.Shape[1] / 4
	}
	if w.Shape[1] != 4*H || w.Shape[2] != inSize {
		return errType("LSTM", fmt.Sprintf("W 形状 %v 与 hidden=%d input=%d 不符", w.Shape, H, inSize))
	}
	if r.Shape[1] != 4*H || r.Shape[2] != H {
		return errType("LSTM", fmt.Sprintf("R 形状 %v 不符", r.Shape))
	}
	if w.Shape[0] != dirs {
		return errType("LSTM", "权重方向数与 direction 不符")
	}

	gate := 4 * H
	yOut := make([]float32, seq*dirs*batch*H)
	yH := make([]float32, dirs*batch*H)
	yC := make([]float32, dirs*batch*H)

	var wg sync.WaitGroup
	for d := 0; d < dirs; d++ {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()
			wBase := d * gate * inSize
			rBase := d * gate * H
			var wb, rb []float32
			if b != nil && len(b.F32) >= (d+1)*2*gate {
				wb = b.F32[d*2*gate : d*2*gate+gate]
				rb = b.F32[d*2*gate+gate : (d+1)*2*gate]
			}
			// 隐状态与单元状态用 float64 保存：LSTM 的 18 步循环里，
			// float32 的每步舍入会逐步累积（实测 t=0 时与 ORT 差 2.4e-8，
			// 到末步放大到 ~2.0）。改用 float64 后与 ORT 一致。
			h := make([]float64, batch*H)
			c := make([]float64, batch*H)
			if initH != nil && len(initH.F32) >= (d+1)*batch*H {
				for i, v := range initH.F32[d*batch*H : (d+1)*batch*H] {
					h[i] = float64(v)
				}
			}
			if initC != nil && len(initC.F32) >= (d+1)*batch*H {
				for i, v := range initC.F32[d*batch*H : (d+1)*batch*H] {
					c[i] = float64(v)
				}
			}
			gates := make([]float64, batch*gate)
			for s := 0; s < seq; s++ {
				t := s
				// 反向遍历的两种情形：
				//   direction="reverse"        -> 唯一方向就是反向
				//   direction="bidirectional"  -> 第 2 个方向是反向（第 1 个是正向）
				// 只判断 dir=="reverse" 会漏掉双向的情形，导致第 2 个方向一直正向计算。
				if dir == "reverse" || (dir == "bidirectional" && d == 1) {
					t = seq - 1 - s
				}
				for bi := 0; bi < batch; bi++ {
					g := gates[bi*gate : (bi+1)*gate]
					xRow := x.F32[(t*batch+bi)*inSize : (t*batch+bi+1)*inSize]
					hRow := h[bi*H : (bi+1)*H]
					// 标准 ONNX LSTM 的 W 布局是 [dirs, 4H, input]，R 是 [dirs, 4H, H]。
					// （注意：量化模型的 DynamicQuantizeLSTM 用的是转置布局 [dirs, input, 4H]，
					//  两者不能混用 —— 抄错会导致输出看似合理但全错。）
					for j := 0; j < gate; j++ {
						// 512+512 项累加放 float64
						var acc float64
						if wb != nil {
							acc = float64(wb[j])
						}
						if rb != nil {
							acc += float64(rb[j])
						}
						wRow := w.F32[wBase+j*inSize : wBase+j*inSize+inSize]
						for k := 0; k < inSize; k++ {
							acc += float64(wRow[k]) * float64(xRow[k])
						}
						rRow := r.F32[rBase+j*H : rBase+j*H+H]
						for k := 0; k < H; k++ {
							acc += float64(rRow[k]) * hRow[k]
						}
						g[j] = acc
					}
					hb := bi * H
					for j := 0; j < H; j++ {
						gi := sigmoid64(g[j])
						go_ := sigmoid64(g[H+j])
						gf := sigmoid64(g[2*H+j])
						gc := math.Tanh(g[3*H+j])
						c[hb+j] = gf*c[hb+j] + gi*gc
						h[hb+j] = go_ * math.Tanh(c[hb+j])
					}
					for j := 0; j < H; j++ {
						yOut[((t*dirs+d)*batch+bi)*H+j] = float32(h[hb+j])
					}
				}
			}
			for i, v := range h {
				yH[d*batch*H+i] = float32(v)
			}
			for i, v := range c {
				yC[d*batch*H+i] = float32(v)
			}
		}(d)
	}
	wg.Wait()

	if err := out(env, n, 0, NewF32([]int{seq, dirs, batch, H}, yOut)); err != nil {
		return err
	}
	if err := out(env, n, 1, NewF32([]int{dirs, batch, H}, yH)); err != nil {
		return err
	}
	return out(env, n, 2, NewF32([]int{dirs, batch, H}, yC))
}

var _ = math.Tanh

// sigmoid64 供 float64 路径使用（LSTM 的隐状态递推保持双精度，
// 避免 18 步循环里每步 float32 舍入的累积）。
func sigmoid64(v float64) float64 { return 1 / (1 + math.Exp(-v)) }
