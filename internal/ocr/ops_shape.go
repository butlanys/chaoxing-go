package ocr

import "fmt"

// opTranspose 按 perm 重排维度；perm 缺省时反转维度。
func opTranspose(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	perm := n.Attr("perm").IntsOr(nil)
	nd := len(x.Shape)
	if perm == nil {
		perm = make([]int64, nd)
		for i := range perm {
			perm[i] = int64(nd - 1 - i)
		}
	}
	if len(perm) != nd {
		return errType("Transpose", "perm 长度与维数不符")
	}
	newShape := make([]int, nd)
	for i, p := range perm {
		if p < 0 || int(p) >= nd {
			return errType("Transpose", "perm 越界")
		}
		newShape[i] = x.Shape[p]
	}
	cnt := x.NumElem()
	idx := make([]int, nd)
	src := make([]int, nd)
	remap := func(o int) int {
		rem := o
		for i := nd - 1; i >= 0; i-- {
			idx[i] = rem % newShape[i]
			rem /= newShape[i]
		}
		for i, p := range perm {
			src[p] = idx[i]
		}
		off, _ := Offset(x.Shape, src)
		return off
	}
	switch x.Kind {
	case KindF32:
		y := make([]float32, cnt)
		for o := range y {
			y[o] = x.F32[remap(o)]
		}
		return out(env, n, 0, NewF32(newShape, y))
	case KindI32:
		y := make([]int32, cnt)
		for o := range y {
			y[o] = x.I32[remap(o)]
		}
		return out(env, n, 0, NewI32(newShape, y))
	case KindI64:
		y := make([]int64, cnt)
		for o := range y {
			y[o] = x.I64[remap(o)]
		}
		return out(env, n, 0, NewI64(newShape, y))
	case KindU8:
		y := make([]uint8, cnt)
		for o := range y {
			y[o] = x.U8[remap(o)]
		}
		return out(env, n, 0, NewU8(newShape, y))
	case KindI8:
		y := make([]int8, cnt)
		for o := range y {
			y[o] = x.I8[remap(o)]
		}
		return out(env, n, 0, NewI8(newShape, y))
	}
	return errType("Transpose", "不支持的类型 "+x.Kind.String())
}

// opShape 返回输入的形状（int64 一维张量）。
func opShape(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	v := make([]int64, len(x.Shape))
	for i, d := range x.Shape {
		v[i] = int64(d)
	}
	return out(env, n, 0, NewI64([]int{len(v)}, v))
}

// opGather 沿 axis 按下标取值。量化模型里用它从形状里取某一维。
func opGather(env Env, n *Node) error {
	data, err := in(env, n, 0)
	if err != nil {
		return err
	}
	idxT, err := in(env, n, 1)
	if err != nil {
		return err
	}
	axis := int(n.Attr("axis").Int(0))
	if axis < 0 {
		axis += len(data.Shape)
	}
	if axis < 0 || axis >= len(data.Shape) {
		return errType("Gather", "axis 越界")
	}

	nIdx := idxT.NumElem()
	idx := make([]int, nIdx)
	for i := range idx {
		v := int(idxT.IntScalarAt(i))
		if v < 0 {
			v += data.Shape[axis]
		}
		if v < 0 || v >= data.Shape[axis] {
			return errType("Gather", fmt.Sprintf("下标越界: %d", v))
		}
		idx[i] = v
	}

	outShape := make([]int, 0, len(data.Shape)+nIdx-1)
	outShape = append(outShape, data.Shape[:axis]...)
	outShape = append(outShape, idxT.Shape...)
	outShape = append(outShape, data.Shape[axis+1:]...)

	inner := NumElem(data.Shape[axis+1:])
	outer := NumElem(data.Shape[:axis])
	cnt := outer * nIdx * inner

	switch data.Kind {
	case KindI64:
		y := make([]int64, cnt)
		k := 0
		for o := 0; o < outer; o++ {
			for _, ix := range idx {
				base := (o*data.Shape[axis] + ix) * inner
				copy(y[k:k+inner], data.I64[base:base+inner])
				k += inner
			}
		}
		return out(env, n, 0, NewI64(outShape, y))
	case KindF32:
		y := make([]float32, cnt)
		k := 0
		for o := 0; o < outer; o++ {
			for _, ix := range idx {
				base := (o*data.Shape[axis] + ix) * inner
				copy(y[k:k+inner], data.F32[base:base+inner])
				k += inner
			}
		}
		return out(env, n, 0, NewF32(outShape, y))
	}
	return errType("Gather", "暂不支持的类型 "+data.Kind.String())
}

// IntScalarAt 取第 i 个元素的整数表示。
func (t *Tensor) IntScalarAt(i int) int64 {
	switch t.Kind {
	case KindI64:
		return t.I64[i]
	case KindI32:
		return int64(t.I32[i])
	case KindU8:
		return int64(t.U8[i])
	case KindI8:
		return int64(t.I8[i])
	case KindF32:
		return int64(t.F32[i])
	}
	return 0
}

// opUnsqueeze 在 axes 指定的位置插入长度为 1 的维度。
func opUnsqueeze(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	axes := n.Attr("axes").IntsOr(nil)
	if axes == nil {
		// 老版本 opset 把 axes 放在第二个输入里
		if a := inOpt(env, n, 1); a != nil {
			for i := 0; i < a.NumElem(); i++ {
				axes = append(axes, a.IntScalarAt(i))
			}
		}
	}
	nd := len(x.Shape) + len(axes)
	if nd == 0 {
		return errType("Unsqueeze", "缺少 axes")
	}
	newShape := make([]int, nd)
	mark := make([]bool, nd)
	for _, a := range axes {
		i := int(a)
		if i < 0 {
			i += nd
		}
		if i < 0 || i >= nd || mark[i] {
			return errType("Unsqueeze", fmt.Sprintf("axes 非法: %v", axes))
		}
		mark[i] = true
		newShape[i] = 1
	}
	j := 0
	for i := 0; i < nd; i++ {
		if !mark[i] {
			newShape[i] = x.Shape[j]
			j++
		}
	}
	return reshapeTo(env, n, x, newShape)
}

// opReshape 在不改变元素顺序的前提下改变形状；允许 -1 表示自动推断，
// 也允许 0 表示"沿用输入该维"（ONNX 语义）。
func opReshape(env Env, n *Node) error {
	x, err := in(env, n, 0)
	if err != nil {
		return err
	}
	shT, err := in(env, n, 1)
	if err != nil {
		return err
	}
	raw := make([]int, shT.NumElem())
	for i := range raw {
		raw[i] = int(shT.IntScalarAt(i))
	}
	target, err := resolveShape(raw, x.Shape)
	if err != nil {
		return errType("Reshape", err.Error())
	}
	return reshapeTo(env, n, x, target)
}

func resolveShape(raw, inShape []int) ([]int, error) {
	out := make([]int, len(raw))
	neg := -1
	prod := 1
	for i, d := range raw {
		switch {
		case d == -1:
			if neg >= 0 {
				return nil, fmt.Errorf("只能有一个 -1")
			}
			neg = i
		case d == 0:
			if i >= len(inShape) {
				return nil, fmt.Errorf("0 的位置超出了输入维数")
			}
			out[i] = inShape[i]
			prod *= out[i]
		case d < 0:
			return nil, fmt.Errorf("非法维度 %d", d)
		default:
			out[i] = d
			prod *= d
		}
	}
	total := NumElem(inShape)
	if neg >= 0 {
		if prod == 0 || total%prod != 0 {
			return nil, fmt.Errorf("无法推断 -1 维: 元素总数 %d 不能被 %d 整除", total, prod)
		}
		out[neg] = total / prod
	}
	if NumElem(out) != total {
		return nil, fmt.Errorf("形状元素总数不符: %v -> %v (输入 %v)", raw, out, inShape)
	}
	return out, nil
}

// reshapeTo 只改形状不搬数据。
func reshapeTo(env Env, n *Node, x *Tensor, shape []int) error {
	y := &Tensor{Shape: append([]int(nil), shape...), Kind: x.Kind,
		F32: x.F32, I32: x.I32, I64: x.I64, U8: x.U8, I8: x.I8}
	return out(env, n, 0, y)
}

// opConcat 沿 axis 拼接。
func opConcat(env Env, n *Node) error {
	ins := make([]*Tensor, 0, len(n.Inputs))
	for i := range n.Inputs {
		t, err := in(env, n, i)
		if err != nil {
			return err
		}
		ins = append(ins, t)
	}
	if len(ins) == 0 {
		return errType("Concat", "没有输入")
	}
	axis := int(n.Attr("axis").Int(0))
	nd := len(ins[0].Shape)
	if axis < 0 {
		axis += nd
	}
	if axis < 0 || axis >= nd {
		return errType("Concat", "axis 越界")
	}
	outShape := append([]int(nil), ins[0].Shape...)
	outShape[axis] = 0
	for _, t := range ins {
		if len(t.Shape) != nd {
			return errType("Concat", "输入维数不一致")
		}
		if t.Kind != ins[0].Kind {
			return errType("Concat", "输入类型不一致")
		}
		for d := range t.Shape {
			if d != axis && t.Shape[d] != outShape[d] {
				return errType("Concat", "非拼接维不一致")
			}
		}
		outShape[axis] += t.Shape[axis]
	}

	outer := NumElem(ins[0].Shape[:axis])
	inner := NumElem(ins[0].Shape[axis+1:])
	cnt := NumElem(outShape)

	switch ins[0].Kind {
	case KindI64:
		y := make([]int64, 0, cnt)
		for o := 0; o < outer; o++ {
			for _, t := range ins {
				seg := t.Shape[axis] * inner
				base := o * seg
				y = append(y, t.I64[base:base+seg]...)
			}
		}
		return out(env, n, 0, NewI64(outShape, y))
	case KindF32:
		y := make([]float32, 0, cnt)
		for o := 0; o < outer; o++ {
			for _, t := range ins {
				seg := t.Shape[axis] * inner
				base := o * seg
				y = append(y, t.F32[base:base+seg]...)
			}
		}
		return out(env, n, 0, NewF32(outShape, y))
	case KindI32:
		y := make([]int32, 0, cnt)
		for o := 0; o < outer; o++ {
			for _, t := range ins {
				seg := t.Shape[axis] * inner
				base := o * seg
				y = append(y, t.I32[base:base+seg]...)
			}
		}
		return out(env, n, 0, NewI32(outShape, y))
	}
	return errType("Concat", "不支持的类型 "+ins[0].Kind.String())
}

// opConstant 直接产出一个常量张量（形状计算用）。
func opConstant(env Env, n *Node) error {
	a := n.Attr("value")
	if a.Type != attrTensor || a.T == nil {
		return errType("Constant", "缺少 value 张量属性")
	}
	return out(env, n, 0, a.T)
}

// opConstantOfShape 产出指定形状、以 value 填充的张量（缺省填 0）。
func opConstantOfShape(env Env, n *Node) error {
	shT, err := in(env, n, 0)
	if err != nil {
		return err
	}
	shape := make([]int, shT.NumElem())
	for i := range shape {
		shape[i] = int(shT.IntScalarAt(i))
	}
	cnt := NumElem(shape)
	a := n.Attr("value")
	if a.Type == attrTensor && a.T != nil {
		switch a.T.Kind {
		case KindF32:
			y := make([]float32, cnt)
			if len(a.T.F32) > 0 {
				for i := range y {
					y[i] = a.T.F32[0]
				}
			}
			return out(env, n, 0, NewF32(shape, y))
		case KindI64:
			y := make([]int64, cnt)
			if len(a.T.I64) > 0 {
				for i := range y {
					y[i] = a.T.I64[0]
				}
			}
			return out(env, n, 0, NewI64(shape, y))
		case KindI32:
			y := make([]int32, cnt)
			if len(a.T.I32) > 0 {
				for i := range y {
					y[i] = a.T.I32[0]
				}
			}
			return out(env, n, 0, NewI32(shape, y))
		}
	}
	return out(env, n, 0, NewF32Zeros(shape))
}

// broadcastShape 计算 NumPy 广播后的形状（右侧对齐）。
func broadcastShape(a, b []int) ([]int, error) {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	res := make([]int, n)
	for i := 0; i < n; i++ {
		da, db := 1, 1
		if i >= n-len(a) {
			da = a[i-(n-len(a))]
		}
		if i >= n-len(b) {
			db = b[i-(n-len(b))]
		}
		switch {
		case da == db:
			res[i] = da
		case da == 1:
			res[i] = db
		case db == 1:
			res[i] = da
		default:
			return nil, fmt.Errorf("形状无法广播: %v 与 %v", a, b)
		}
	}
	return res, nil
}

// broadcastIndex 把输出的一维偏移映射回输入的一维偏移（按广播规则）。
func broadcastIndex(outShape, inShape []int, outOff int) int {
	idx := make([]int, len(outShape))
	rem := outOff
	for i := len(outShape) - 1; i >= 0; i-- {
		idx[i] = rem % outShape[i]
		rem /= outShape[i]
	}
	off := 0
	shift := len(outShape) - len(inShape)
	for i := range inShape {
		d := idx[i+shift]
		if inShape[i] == 1 {
			d = 0
		}
		off = off*inShape[i] + d
	}
	return off
}
