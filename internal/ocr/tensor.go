// Package ocr 是 ddddocr 验证码识别模型的纯 Go 推理实现。
//
// 背景：原 Python 版依赖 ddddocr（onnxruntime + 模型文件），部署门槛高。
// 本包把 ddddocr 的模型转成紧凑格式内嵌进二进制，用纯 Go 执行，
// 不需要 onnxruntime 动态库、不需要 Python、不需要外部 OCR 服务。
//
// **模型选择**：Python 版 chaoxing 用的是 `DdddOcr(show_ad=False)`，它走的是
// `load_ocr_model(old=False, beta=False)` 分支，即 **common_old.onnx**（ORT 动态量化版，
// ConvInteger / DynamicQuantizeLSTM / MatMulInteger），而不是 common.onnx（浮点 beta 模型）。
// 两者不是同一个模型：394 张真实验证码上输出 0 张一致，common.onnx 给出的是乱码。
// 因此本实现以 common_old.onnx 为准，逐算子复刻其量化语义。
//
// 模型结构（由 scripts/convert_ddddocr.py 断言校验）：
//
//	CRNN: 3 组 stride-2 卷积 (64xW -> 8xW/8, 64 通道)
//	      -> 转置/展平为序列 (seqlen x 512)
//	      -> 双向 LSTM (hidden=512)
//	      -> MatMul (1024 -> 字符集)
//
// 输出层已按超星验证码的实际字符集裁剪：平台前端正则即 /[0-9a-zA-Z]{4}/，
// 所以 8210 类裁到 63 类（62 个字符 + CTC blank），8.02MB 权重降到 62KB。
package ocr

import "fmt"

// Kind 张量元素类型。
type Kind uint8

const (
	// KindF32 单精度浮点
	KindF32 Kind = iota
	// KindI32 32 位整数（量化卷积/矩阵乘的累加类型）
	KindI32
	// KindI64 64 位整数（形状计算）
	KindI64
	// KindU8 无符号 8 位（量化后的激活）
	KindU8
	// KindI8 有符号 8 位（量化后的权重）
	KindI8
)

// String 便于报错时说明类型。
func (k Kind) String() string {
	switch k {
	case KindF32:
		return "float32"
	case KindI32:
		return "int32"
	case KindI64:
		return "int64"
	case KindU8:
		return "uint8"
	case KindI8:
		return "int8"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Tensor 是一个稠密张量。按 Kind 使用对应的数据切片，其余为 nil。
type Tensor struct {
	Shape []int
	Kind  Kind
	F32   []float32
	I32   []int32
	I64   []int64
	U8    []uint8
	I8    []int8
}

// NewF32 构造 float32 张量。
func NewF32(shape []int, data []float32) *Tensor {
	return &Tensor{Shape: append([]int(nil), shape...), Kind: KindF32, F32: data}
}

// NewF32Zeros 构造填零的 float32 张量。
func NewF32Zeros(shape []int) *Tensor { return NewF32(shape, make([]float32, NumElem(shape))) }

// NewI32 构造 int32 张量。
func NewI32(shape []int, data []int32) *Tensor {
	return &Tensor{Shape: append([]int(nil), shape...), Kind: KindI32, I32: data}
}

// NewI32Zeros 构造填零的 int32 张量。
func NewI32Zeros(shape []int) *Tensor { return NewI32(shape, make([]int32, NumElem(shape))) }

// NewI64 构造 int64 张量。
func NewI64(shape []int, data []int64) *Tensor {
	return &Tensor{Shape: append([]int(nil), shape...), Kind: KindI64, I64: data}
}

// NewU8 构造 uint8 张量。
func NewU8(shape []int, data []uint8) *Tensor {
	return &Tensor{Shape: append([]int(nil), shape...), Kind: KindU8, U8: data}
}

// NewI8 构造 int8 张量。
func NewI8(shape []int, data []int8) *Tensor {
	return &Tensor{Shape: append([]int(nil), shape...), Kind: KindI8, I8: data}
}

// NumElem 张量元素个数。
func (t *Tensor) NumElem() int { return NumElem(t.Shape) }

// Scalar 取标量值（0 维或单元素张量）的 float32 值。
func (t *Tensor) Scalar() float32 {
	if t == nil {
		return 0
	}
	switch t.Kind {
	case KindF32:
		if len(t.F32) > 0 {
			return t.F32[0]
		}
	case KindI32:
		if len(t.I32) > 0 {
			return float32(t.I32[0])
		}
	case KindI64:
		if len(t.I64) > 0 {
			return float32(t.I64[0])
		}
	case KindU8:
		if len(t.U8) > 0 {
			return float32(t.U8[0])
		}
	case KindI8:
		if len(t.I8) > 0 {
			return float32(t.I8[0])
		}
	}
	return 0
}

// IntScalar 取标量的整数表示，常用于量化参数的 zero_point。
func (t *Tensor) IntScalar() int64 {
	if t == nil {
		return 0
	}
	switch t.Kind {
	case KindU8:
		if len(t.U8) > 0 {
			return int64(t.U8[0])
		}
	case KindI8:
		if len(t.I8) > 0 {
			return int64(t.I8[0])
		}
	case KindI32:
		if len(t.I32) > 0 {
			return int64(t.I32[0])
		}
	case KindI64:
		if len(t.I64) > 0 {
			return t.I64[0]
		}
	}
	return int64(t.Scalar())
}

// NumElem 计算给定形状的元素个数。
func NumElem(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

// Offset 把多维下标转成一维偏移（行主序）。越界返回 false。
func Offset(shape []int, idx []int) (int, bool) {
	if len(idx) != len(shape) {
		return 0, false
	}
	off := 0
	for i, d := range shape {
		if idx[i] < 0 || idx[i] >= d {
			return 0, false
		}
		off = off*d + idx[i]
	}
	return off, true
}

// String 便于调试输出。
func (t *Tensor) String() string {
	return fmt.Sprintf("Tensor%v(%s)", t.Shape, t.Kind)
}
