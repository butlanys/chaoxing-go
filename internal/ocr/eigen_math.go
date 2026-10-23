package ocr

import "math"

// 本文件移植 Eigen 的 float32 exp 实现（pexp_float），用于与 onnxruntime 逐位对齐。
//
// 为什么需要它：ORT 的 Sigmoid/Tanh 等激活走的是 Eigen 的向量化实现，
// 而 Eigen 用的是 Cephes 风格的 6 阶 minimax 多项式 + FMA，
// 与 Go 的 math.Exp（float64，完全不同的算法）在结果上会差 1~2 个 float32 ULP。
// 1 ULP 看起来无所谓，但 DynamicQuantizeLinear 的 round() 是不连续的：
// 输入差 1 ULP 就可能让量化值差 1，之后被逐层放大成整个字符的差异。
//
// 源码：Eigen/src/Core/arch/Default/GenericPacketMathFunctions.h 的
// pexp_float / pexp_float_reduced。

// fma32 计算 a*b+c，只舍入一次（等价于硬件的 float32 FMA）。
//
// 用 float64 中间量实现：float32 有 24 位有效位，float64 有 53 位，
// 53 >= 2*24+2，满足"双舍入无害"的条件，因此 float64 算完再舍到 float32
// 与直接做 float32 FMA 结果一致。
func fma32(a, b, c float32) float32 {
	return float32(math.FMA(float64(a), float64(b), float64(c)))
}

// eigenExpReduced 计算 exp(r)，r 限定在 [-ln2/2, ln2/2]，6 阶 minimax 多项式。
func eigenExpReduced(r float32) float32 {
	const (
		p2 = float32(0.49999988079071044921875)
		p3 = float32(0.16666518151760101318359375)
		p4 = float32(4.166965186595916748046875e-2)
		p5 = float32(8.36894474923610687255859375e-3)
		p6 = float32(1.37449637986719608306884765625e-3)
	)
	r2 := r * r
	pEven := fma32(r2, p6, p4)
	pOdd := fma32(r2, p5, p3)
	pEven = fma32(r2, pEven, p2)
	pLow := r + 1.0
	y := fma32(r, pOdd, pEven)
	return fma32(r2, y, pLow)
}

// eigenExp 移植 Eigen 的 pexp_float：x = m*ln2 + r，exp(x) = 2^m * exp(r)。
func eigenExp(x float32) float32 {
	const (
		cstExpHi   = float32(88.723)
		cstExpLo   = float32(-104.0)
		cstLog2EF  = float32(1.44269504088896341)
		cstExpC1   = float32(-0.693359375)
		cstExpC2   = float32(2.12194440e-4)
		doubleBias = int32(254)
	)
	// 夹到安全区间，避免上溢/下溢
	if x < cstExpLo {
		x = cstExpLo
	}
	if x > cstExpHi {
		x = cstExpHi
	}
	// m = rint(x / ln2)
	m := float32(math.RoundToEven(float64(x * cstLog2EF)))
	// r = x - m*ln2，分两段相减避免截断误差累积
	r := fma32(m, cstExpC1, x)
	r = fma32(m, cstExpC2, r)

	y := eigenExpReduced(r)

	// 用指数位拆分构造 2^m，避免中间结果溢出
	mi := int32(m)
	biasedSum := mi + doubleBias
	biasedHi := biasedSum >> 1
	biasedLo := biasedSum - biasedHi
	pow2Hi := math.Float32frombits(uint32(biasedHi) << 23)
	pow2Lo := math.Float32frombits(uint32(biasedLo) << 23)
	return y * pow2Hi * pow2Lo
}

// eigenSigmoid 用 Eigen 的 exp 计算 sigmoid，用于与 onnxruntime 对齐。
func eigenSigmoid(x float32) float32 {
	return 1.0 / (1.0 + eigenExp(-x))
}

// eigenTanh 用 Eigen 的 exp 计算 tanh。
//
// Eigen 的 tanh 实现在数值上等价于 (1 - e^-2x)/(1 + e^-2x) 的变体，
// 这里采用同样只用 exp 的写法，保证与 Eigen 走同一条数值路径。
func eigenTanh(x float32) float32 {
	if x >= 0 {
		e := eigenExp(-2.0 * x)
		return (1.0 - e) / (1.0 + e)
	}
	e := eigenExp(2.0 * x)
	return (e - 1.0) / (e + 1.0)
}
