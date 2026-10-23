package ocr

import (
	"fmt"
	"math"
)

func errType(op, msg string) error {
	return fmt.Errorf("算子 %s: %s", op, msg)
}

// ONNX TensorProto 的元素类型编号，Cast 的 to 属性用它。
const (
	onnxFloat  = 1
	onnxUint8  = 2
	onnxInt8   = 3
	onnxInt32  = 6
	onnxInt64  = 7
	onnxDouble = 11
)

// opSigmoid 逐元素 sigmoid。对齐 ONNX Sigmoid: 1/(1+exp(-x))。
func opSigmoid(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	if x.Kind != KindF32 {
		return errType("Sigmoid", "仅支持 float32，实得 "+x.Kind.String())
	}
	y := make([]float32, len(x.F32))
	for i, v := range x.F32 {
		y[i] = 1 / (1 + float32(math.Exp(float64(-v))))
	}
	return out(env, n, 0, NewF32(x.Shape, y))
}

// opMul 逐元素相乘，支持 NumPy 广播。ddddocr 的 SiLU 激活由 Sigmoid+Mul 拼成。
func opMul(env Env, n *Node) error { return binaryElemwise(env, n, opMulName) }

// opAdd 逐元素相加，支持 NumPy 广播。残差连接与量化偏置相加用。
func opAdd(env Env, n *Node) error { return binaryElemwise(env, n, opAddName) }

// opDiv 逐元素相除，支持广播。量化权重时用（W / scale）。
func opDiv(env Env, n *Node) error { return binaryElemwise(env, n, opDivName) }

const (
	opMulName = "Mul"
	opAddName = "Add"
	opDivName = "Div"
)

func binaryElemwise(env Env, n *Node, op string) error {
	a, err := in(env, n, 0)
	if err != nil {
		return err
	}
	b, err := in(env, n, 1)
	if err != nil {
		return err
	}
	if a.Kind != b.Kind {
		return errType(op, fmt.Sprintf("两个输入类型不一致: %s 与 %s", a.Kind, b.Kind))
	}
	shape, err := broadcastShape(a.Shape, b.Shape)
	if err != nil {
		return err
	}
	cnt := NumElem(shape)

	switch a.Kind {
	case KindF32:
		y := make([]float32, cnt)
		for i := range y {
			x := a.F32[broadcastIndex(shape, a.Shape, i)]
			z := b.F32[broadcastIndex(shape, b.Shape, i)]
			y[i] = applyF32(op, x, z)
		}
		return out(env, n, 0, NewF32(shape, y))
	case KindI32:
		y := make([]int32, cnt)
		for i := range y {
			x := a.I32[broadcastIndex(shape, a.Shape, i)]
			z := b.I32[broadcastIndex(shape, b.Shape, i)]
			switch op {
			case opMulName:
				y[i] = x * z
			case opAddName:
				y[i] = x + z
			default:
				return errType(op, "整型不支持除法")
			}
		}
		return out(env, n, 0, NewI32(shape, y))
	case KindI64:
		y := make([]int64, cnt)
		for i := range y {
			x := a.I64[broadcastIndex(shape, a.Shape, i)]
			z := b.I64[broadcastIndex(shape, b.Shape, i)]
			switch op {
			case opMulName:
				y[i] = x * z
			case opAddName:
				y[i] = x + z
			default:
				return errType(op, "整型不支持除法")
			}
		}
		return out(env, n, 0, NewI64(shape, y))
	}
	return errType(op, "不支持的类型 "+a.Kind.String())
}

func applyF32(op string, x, z float32) float32 {
	switch op {
	case opMulName:
		return x * z
	case opAddName:
		return x + z
	default:
		return x / z
	}
}

// opFloor 逐元素向下取整。量化权重时用。
func opFloor(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	if x.Kind != KindF32 {
		return errType("Floor", "仅支持 float32，实得 "+x.Kind.String())
	}
	y := make([]float32, len(x.F32))
	for i, v := range x.F32 {
		y[i] = float32(math.Floor(float64(v)))
	}
	return out(env, n, 0, NewF32(x.Shape, y))
}

// opCast 类型转换。量化模型里用它在 int32/float/uint8 之间搬运。
func opCast(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	to := int(n.Attr("to").Int(0))
	if to == 0 {
		return errType("Cast", "缺少 to 属性")
	}
	cnt := x.NumElem()
	switch to {
	case onnxFloat, onnxDouble:
		y := make([]float32, cnt)
		for i := range y {
			y[i] = elemF32(x, i)
		}
		return out(env, n, 0, NewF32(x.Shape, y))
	case onnxInt32:
		y := make([]int32, cnt)
		for i := range y {
			y[i] = int32(elemF64(x, i))
		}
		return out(env, n, 0, NewI32(x.Shape, y))
	case onnxInt64:
		y := make([]int64, cnt)
		for i := range y {
			y[i] = int64(elemF64(x, i))
		}
		return out(env, n, 0, NewI64(x.Shape, y))
	case onnxUint8:
		y := make([]uint8, cnt)
		for i := range y {
			y[i] = uint8(clampI64(int64(elemF64(x, i)), 0, 255))
		}
		return out(env, n, 0, NewU8(x.Shape, y))
	case onnxInt8:
		y := make([]int8, cnt)
		for i := range y {
			y[i] = int8(clampI64(int64(elemF64(x, i)), -128, 127))
		}
		return out(env, n, 0, NewI8(x.Shape, y))
	}
	return errType("Cast", fmt.Sprintf("不支持的目标类型 %d", to))
}

// elemF32 按一维偏移取元素并转成 float32。
func elemF32(t *Tensor, i int) float32 {
	switch t.Kind {
	case KindF32:
		return t.F32[i]
	case KindI32:
		return float32(t.I32[i])
	case KindI64:
		return float32(t.I64[i])
	case KindU8:
		return float32(t.U8[i])
	case KindI8:
		return float32(t.I8[i])
	}
	return 0
}

// elemF64 按一维偏移取元素并转成 float64（Cast 到整型时用，先取整再截断）。
func elemF64(t *Tensor, i int) float64 {
	switch t.Kind {
	case KindF32:
		return float64(t.F32[i])
	case KindI32:
		return float64(t.I32[i])
	case KindI64:
		return float64(t.I64[i])
	case KindU8:
		return float64(t.U8[i])
	case KindI8:
		return float64(t.I8[i])
	}
	return 0
}

func clampI64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
