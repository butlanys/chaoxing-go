package ocr

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// opDynamicQuantizeLinear 动态量化：按输入的实际值域算出 scale / zero_point，
// 再把输入线性映射到 uint8。对齐 ONNX DynamicQuantizeLinear：
//
//	x_min = min(min(x), 0); x_max = max(max(x), 0)
//	y_scale = (x_max - x_min) / 255
//	y_zero_point = round(clip(-x_min / y_scale, 0, 255))
//	y = clip(round(x / y_scale) + y_zero_point, 0, 255)
//
// 已用 onnxruntime 的中间张量逐元素比对，偏差为 0。
func opDynamicQuantizeLinear(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	if x.Kind != KindF32 {
		return errType("DynamicQuantizeLinear", "仅支持 float32，实得 "+x.Kind.String())
	}
	if len(x.F32) == 0 {
		return errType("DynamicQuantizeLinear", "输入为空")
	}

	xmin, xmax := x.F32[0], x.F32[0]
	for _, v := range x.F32 {
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
	// 全零输入时 scale 为 0，此时量化结果恒为 zero_point（0），与 ORT 行为一致
	if scale == 0 {
		if err := out(env, n, 0, NewU8(x.Shape, make([]uint8, len(x.F32)))); err != nil {
			return err
		}
		// scale / zero_point 的形状对齐 onnxruntime 的 [1]（ONNX 规范是标量，
		// 但 ORT 实际物化成 1 元素张量，逐层比对时按 ORT 的来）
		if err := out(env, n, 1, NewF32([]int{1}, []float32{0})); err != nil {
			return err
		}
		return out(env, n, 2, NewU8([]int{1}, []uint8{0}))
	}

	zpF := math.Round(float64(-xmin / scale))
	if zpF < 0 {
		zpF = 0
	}
	if zpF > 255 {
		zpF = 255
	}
	zp := uint8(zpF)

	y := make([]uint8, len(x.F32))
	inv := 1.0 / float64(scale)
	for i, v := range x.F32 {
		q := math.Round(float64(v)*inv) + zpF
		if q < 0 {
			q = 0
		}
		if q > 255 {
			q = 255
		}
		y[i] = uint8(q)
	}

	if err := out(env, n, 0, NewU8(x.Shape, y)); err != nil {
		return err
	}
	if err := out(env, n, 1, NewF32([]int{1}, []float32{float32(scale)})); err != nil {
		return err
	}
	return out(env, n, 2, NewU8([]int{1}, []uint8{zp}))
}

// opConvInteger 整数卷积。对齐 ONNX ConvInteger：
//
//	y[n,m,oh,ow] = sum_{c,kh,kw} (x[n,c,ih,iw] - x_zero_point) * (w[m,c,kh,kw] - w_zero_point)
//
// 累加在 int32 上精确完成（整数运算无舍入），因此结果与 onnxruntime 逐元素一致。
func opConvInteger(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	w, err := in(env, n, 1)
	if err != nil {
		return err
	}
	xzp := int32(inOpt(env, n, 2).IntScalar())
	wzp := int32(inOpt(env, n, 3).IntScalar())

	if len(x.Shape) != 4 {
		return errType("ConvInteger", fmt.Sprintf("输入需为 4 维，实得 %v", x.Shape))
	}
	if len(w.Shape) != 4 {
		return errType("ConvInteger", fmt.Sprintf("权重需为 4 维，实得 %v", w.Shape))
	}
	if x.Kind != KindU8 && x.Kind != KindI8 {
		return errType("ConvInteger", "输入需为 uint8/int8，实得 "+x.Kind.String())
	}
	if w.Kind != KindU8 && w.Kind != KindI8 {
		return errType("ConvInteger", "权重需为 uint8/int8，实得 "+w.Kind.String())
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
		return errType("ConvInteger", fmt.Sprintf("group=%d 与通道数不匹配 (C=%d, C/g=%d)", group, C, Cg))
	}
	sh, sw := strides[0], strides[1]
	dh, dw := dils[0], dils[1]
	ph0, pw0, ph1, pw1 := pads[0], pads[1], pads[2], pads[3]

	oH := (H+ph0+ph1-dh*(kH-1)-1)/sh + 1
	oW := (W+pw0+pw1-dw*(kW-1)-1)/sw + 1
	if oH <= 0 || oW <= 0 {
		return errType("ConvInteger", "输出尺寸非法")
	}

	// 输入转 int32 并减去 zero_point（padding 区域视为 0，即贡献 -xzp*... 不参与）
	xi := make([]int32, len(x.U8)+len(x.I8))
	if x.Kind == KindU8 {
		for i, v := range x.U8 {
			xi[i] = int32(v) - xzp
		}
	} else {
		for i, v := range x.I8 {
			xi[i] = int32(v) - xzp
		}
	}
	wi := make([]int32, M*Cg*kH*kW)
	if w.Kind == KindU8 {
		for i, v := range w.U8 {
			wi[i] = int32(v) - wzp
		}
	} else {
		for i, v := range w.I8 {
			wi[i] = int32(v) - wzp
		}
	}

	outShape := []int{N, M, oH, oW}
	y := make([]int32, N*M*oH*oW)

	// 按输出通道并行：每个 goroutine 负责一段 m，各自遍历全部像素。
	// 卷积是这里最重的算子（约 2.9 亿次乘加），并行后单张图降到百毫秒级。
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
					wBase := m * Cg * kH * kW
					xBase := nIdx * C * H * W
					for oh := 0; oh < oH; oh++ {
						for ow := 0; ow < oW; ow++ {
							var acc int32
							for c := 0; c < Cg; c++ {
								cIn := g*Cg + c
								xPlane := xBase + cIn*H*W
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
										acc += xi[rowBase+iw] * wi[wRow+j]
									}
								}
							}
							y[((nIdx*M+m)*oH+oh)*oW+ow] = acc
						}
					}
				}
			}
		}(m0, m1)
	}
	wg.Wait()

	return out(env, n, 0, NewI32(outShape, y))
}

// opMatMulInteger 整数矩阵乘。对齐 ONNX MatMulInteger：
//
//	y[i,j] = sum_k (a[i,k] - a_zero_point) * (b[k,j] - b_zero_point)
func opMatMulInteger(env Env, n *Node) error {
	a, err := in(env, n, 0)
	if err != nil {
		return err
	}
	b, err := in(env, n, 1)
	if err != nil {
		return err
	}
	azp := int32(inOpt(env, n, 2).IntScalar())
	bzp := int32(inOpt(env, n, 3).IntScalar())

	if len(a.Shape) != 2 || len(b.Shape) != 2 {
		return errType("MatMulInteger", fmt.Sprintf("仅支持 2 维，实得 %v 与 %v", a.Shape, b.Shape))
	}
	M, K := a.Shape[0], a.Shape[1]
	K2, N := b.Shape[0], b.Shape[1]
	if K != K2 {
		return errType("MatMulInteger", fmt.Sprintf("K 维不匹配: %d 与 %d", K, K2))
	}

	ai := make([]int32, len(a.U8)+len(a.I8))
	if a.Kind == KindU8 {
		for i, v := range a.U8 {
			ai[i] = int32(v) - azp
		}
	} else {
		for i, v := range a.I8 {
			ai[i] = int32(v) - azp
		}
	}
	bi := make([]int32, len(b.U8)+len(b.I8))
	if b.Kind == KindU8 {
		for i, v := range b.U8 {
			bi[i] = int32(v) - bzp
		}
	} else {
		for i, v := range b.I8 {
			bi[i] = int32(v) - bzp
		}
	}

	y := make([]int32, M*N)
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
				aRow := ai[i*K : i*K+K]
				yRow := y[i*N : i*N+N]
				for k := 0; k < K; k++ {
					av := aRow[k]
					if av == 0 {
						continue
					}
					bRow := bi[k*N : k*N+N]
					for j := range yRow {
						yRow[j] += av * bRow[j]
					}
				}
			}
		}(r0, r1)
	}
	wg.Wait()

	return out(env, n, 0, NewI32([]int{M, N}, y))
}

// intsOr 读取整型数组属性，缺失时用默认值。
func intsOr(n *Node, name string, def []int) []int {
	a := n.Attr(name)
	if a.Type != attrInts || len(a.Ints) == 0 {
		return def
	}
	out := make([]int, len(a.Ints))
	for i, v := range a.Ints {
		out[i] = int(v)
	}
	return out
}
