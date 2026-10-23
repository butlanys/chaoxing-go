package ocr

import (
	"fmt"
	"sort"
)

// opFunc 是算子的执行函数。env 是当前张量环境（权重 + 中间结果），
// 函数需要把结果写回 env[n.Outputs[i]]。
type opFunc func(env Env, n *Node) error

// registry 是算子注册表。新增算子只需在这里注册。
//
// 注册表是两套模型的**超集**：量化模型（common_old.onnx）与浮点模型（common.onnx）
// 的算子名互不冲突，加载哪个模型就走哪套实现。
var registry = map[string]opFunc{
	// ---- 量化模型（common_old.onnx，Python 原版 chaoxing 用的配置）----
	"DynamicQuantizeLinear": opDynamicQuantizeLinear,
	"ConvInteger":           opConvInteger,
	"MatMulInteger":         opMatMulInteger,
	"DynamicQuantizeLSTM":   opDynamicQuantizeLSTM,
	// ---- 浮点模型（common.onnx，DdddOcr(beta=True)）----
	"Conv":  opConv,
	"Gemm":  opGemm,
	"LSTM":  opLSTM,
	// ---- 两者共用 ----
	"Cast":            opCast,
	"Div":             opDiv,
	"Floor":           opFloor,
	"Mul":             opMul,
	"Add":             opAdd,
	"Sigmoid":         opSigmoid,
	"Reshape":         opReshape,
	"Transpose":       opTranspose,
	"Shape":           opShape,
	"Gather":          opGather,
	"Unsqueeze":       opUnsqueeze,
	"Concat":          opConcat,
	"Constant":        opConstant,
	"ConstantOfShape": opConstantOfShape,
}

// SupportedOps 返回已实现的算子名（按字典序），用于启动自检与文档。
func SupportedOps() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Env 是图执行过程中的张量环境，便于逐层取值做数值比对。
type Env map[string]*Tensor

// Run 按拓扑顺序执行整张图，返回指定输出的张量。
// inputs 提供图输入张量；未实现的算子会返回 error 而不是 panic。
func (g *Graph) Run(inputs map[string]*Tensor) (map[string]*Tensor, error) {
	return g.RunTrace(inputs, nil)
}

// RunTrace 与 Run 相同，但在每个节点执行后回调 trace（trace 可为 nil）。
// 用于测试里逐层比对 onnxruntime 的中间激活。
func (g *Graph) RunTrace(inputs map[string]*Tensor, trace func(node *Node, env Env)) (map[string]*Tensor, error) {
	env := make(Env, len(g.Initializers)+len(g.Nodes)+len(inputs))
	for k, v := range g.Initializers {
		env[k] = v
	}
	for k, v := range inputs {
		env[k] = v
	}

	for i, n := range g.Nodes {
		fn, ok := registry[n.Op]
		if !ok {
			return nil, fmt.Errorf("第 %d 个节点使用了未实现的算子 %q（已实现: %v）",
				i, n.Op, SupportedOps())
		}
		if err := fn(env, n); err != nil {
			return nil, fmt.Errorf("第 %d 个节点 %s(%s) 执行失败: %w",
				i, n.Op, firstOr(n.Outputs, "?"), err)
		}
		if trace != nil {
			trace(n, env)
		}
	}

	out := make(map[string]*Tensor, len(g.Outputs))
	for _, name := range g.Outputs {
		t, ok := env[name]
		if !ok {
			return nil, fmt.Errorf("图输出 %q 不存在", name)
		}
		out[name] = t
	}
	return out, nil
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// 取输入张量，缺失时报错（比静默当成零张量安全）。
func in(env Env, n *Node, i int) (*Tensor, error) {
	if i >= len(n.Inputs) {
		return nil, fmt.Errorf("算子 %s 缺少第 %d 个输入", n.Op, i)
	}
	name := n.Inputs[i]
	if name == "" {
		return nil, fmt.Errorf("算子 %s 的第 %d 个输入是可选占位且未被使用", n.Op, i)
	}
	t, ok := env[name]
	if !ok {
		return nil, fmt.Errorf("算子 %s 的输入 %q 不存在", n.Op, name)
	}
	return t, nil
}

// inOpt 取可选输入，缺失或为空占位时返回 nil 且不报错。
func inOpt(env Env, n *Node, i int) *Tensor {
	if i >= len(n.Inputs) {
		return nil
	}
	name := n.Inputs[i]
	if name == "" {
		return nil
	}
	return env[name]
}

func out(env Env, n *Node, i int, t *Tensor) error {
	if i >= len(n.Outputs) {
		return fmt.Errorf("算子 %s 输出个数不足", n.Op)
	}
	env[n.Outputs[i]] = t
	return nil
}

// 广播形状计算与偏移映射见 ops_shape.go 的 broadcastShape / broadcastIndex。

