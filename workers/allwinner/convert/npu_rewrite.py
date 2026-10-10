"""Rewrites a PP-OCR recognition ONNX graph into operations the A733 NPU executes correctly.

The model compiles, but on the physical NPU (VIP9000NANODI_PLUS) some operations give wrong
results, even in float16, so the error builds up through the network:

  * strided convolutions          -> stride-1 convolution, then a 1x1 MaxPool with the stride
  * 1x1 convolutions on (1, C, 1, 1) (squeeze-and-excite)
                                  -> Reshape, broadcast Mul, ReduceSum, Reshape (+ bias)
  * LayerNorm's x / sqrt(v + eps) -> x * (v + eps) ^ -0.5, and Pow(x, 2) -> x * x

Each rewrite is exact: the result is checked against the original with ONNX Runtime.
Found by others porting PP-OCR to this NPU (github.com/sog777/orange-pi-zero-3w-rapidocr-npu).

That isn't enough on its own: on a Cubie A7A every build of the reader (int16, uint8, even
float16) read nearly all lines as blank, while ACUITY's simulator read them right. Tensors that
are network outputs always came out right, though, so the compiler's merging of layers between
them is what runs wrong on the chip. expose() makes the input and output of every convolution an
extra output (outputs[0] stays the real one; the worker ignores the rest). On the chip that gave
the CPU's accuracy exactly (1.48% character errors on 160 test lines, the same 143 read exactly)
at 38 ms a line, 7x faster than an A76 core; exposing less (conv outputs only, activations only,
conv inputs only) still lost lines. It costs NPU memory: ~24 MB for a 320-wide reader.

    python npu_rewrite.py in.onnx out.onnx [--fold]   (a fixed-shape, simplified graph)
"""

from __future__ import annotations

import sys

import numpy as np
import onnx
from onnx import helper, numpy_helper


def fold_affine(model: onnx.ModelProto) -> dict[str, int]:
    """Folds PP-LCNetV3's learnable affine blocks (x * s + b, scalars) into the convolutions.

    The A733 compiler fuses conv -> scale -> bias -> hardswish -> scale -> bias into one kernel
    that computes wrongly on the chip (right in the simulator); without the scalars it's the
    plain conv -> hardswish chain the NPU is built for. Exact:
      conv -> *s -> +b            -> conv with W*s, B*s + b
      x -> *s -> +b -> conv(s)    -> conv(s) with W*s, B + b*sum(W); a zero-padded conv first
                                     pads x with -b/s, which the affine would have turned into 0
    """
    g = model.graph
    inits = {i.name: i for i in g.initializer}
    consumers: dict[str, list[onnx.NodeProto]] = {}
    for n in g.node:
        for i in n.input:
            consumers.setdefault(i, []).append(n)
    outputs = {o.name for o in g.output}
    counts = {"affine_after_conv": 0, "affine_before_conv": 0}
    drop: set[int] = set()
    before: dict[int, list[onnx.NodeProto]] = {}  # Pad nodes to insert before a node
    uid = iter(range(1 << 30))

    def scalar(name: str) -> float | None:
        t = inits.get(name)
        return float(numpy_helper.to_array(t).reshape(-1)[0]) if t is not None and int(np.prod(t.dims)) == 1 else None

    def affine(x: str) -> tuple[onnx.NodeProto, float, onnx.NodeProto, float] | None:
        """x -> Mul(scalar) -> Add(scalar), each the only reader of its input."""
        c = consumers.get(x, [])
        if len(c) != 1 or c[0].op_type != "Mul" or x in outputs:
            return None
        m = c[0]
        s = scalar(m.input[0] if m.input[1] == x else m.input[1])
        a = consumers.get(m.output[0], [])
        if s is None or len(a) != 1 or a[0].op_type != "Add" or m.output[0] in outputs:
            return None
        b = scalar(a[0].input[0] if a[0].input[1] == m.output[0] else a[0].input[1])
        return (m, s, a[0], b) if b is not None else None

    def set_init(name: str, arr: np.ndarray) -> str:
        new = f"{name}__fold{next(uid)}"
        g.initializer.append(numpy_helper.from_array(arr.astype(np.float32), new))
        inits[new] = g.initializer[-1]
        return new

    def conv_params(c: onnx.NodeProto) -> tuple[np.ndarray, np.ndarray]:
        w = numpy_helper.to_array(inits[c.input[1]])
        b = numpy_helper.to_array(inits[c.input[2]]) if len(c.input) > 2 and c.input[2] else np.zeros(w.shape[0], np.float32)
        return w, b

    index = {id(n): k for k, n in enumerate(g.node)}
    for n in list(g.node):
        if index[id(n)] in drop:
            continue
        # conv -> *s -> +b
        if n.op_type == "Conv" and n.input[1] in inits:
            f = affine(n.output[0])
            if f:
                m, s, a, b = f
                w, bias = conv_params(n)
                n.input[1] = set_init(n.input[1], w * s)
                bias_name = set_init(n.input[1] + "_b", bias * s + b)
                if len(n.input) > 2:
                    n.input[2] = bias_name
                else:
                    n.input.append(bias_name)
                n.output[0] = a.output[0]
                drop.update((index[id(m)], index[id(a)]))
                counts["affine_after_conv"] += 1
                continue
        # x -> *s -> +b -> conv(s): fold into every reader
        if n.op_type == "Mul" and index[id(n)] not in drop:
            x = next((i for i in n.input if i not in inits), None)
            f = affine(x) if x else None
            if not f or f[0] is not n or f[1] == 0:
                continue
            m, s, a, b = f
            readers = consumers.get(a.output[0], [])
            if a.output[0] in outputs or not readers or any(r.op_type != "Conv" or r.input[0] != a.output[0] or r.input[1] not in inits for r in readers):
                continue
            for r in readers:
                w, bias = conv_params(r)
                attrs = {t.name: t for t in r.attribute}
                pads = list(helper.get_attribute_value(attrs["pads"])) if "pads" in attrs else [0, 0, 0, 0]
                r.input[1] = set_init(r.input[1], w * s)
                bias_name = set_init(r.input[1] + "_b", bias + b * w.sum(axis=(1, 2, 3)))
                if len(r.input) > 2:
                    r.input[2] = bias_name
                else:
                    r.input.append(bias_name)
                src = x
                if any(pads):
                    p = f"{x}__pad{next(uid)}"
                    pt, pl, pb, pr = pads
                    pad_shape = f"pads__fold{next(uid)}"
                    g.initializer.append(numpy_helper.from_array(np.array([0, 0, pt, pl, 0, 0, pb, pr], np.int64), pad_shape))
                    value = set_init("padval", np.array(-b / s, np.float32))
                    before.setdefault(index[id(r)], []).append(helper.make_node("Pad", [x, pad_shape, value], [p], mode="constant"))
                    r.attribute.remove(attrs["pads"])
                    r.attribute.append(helper.make_attribute("pads", [0, 0, 0, 0]))
                    src = p
                r.input[0] = src
            drop.update((index[id(m)], index[id(a)]))
            counts["affine_before_conv"] += 1
    nodes = []
    for k, n in enumerate(g.node):
        nodes += before.get(k, [])
        if k not in drop:
            nodes.append(n)
    del g.node[:]
    g.node.extend(nodes)
    return counts


def rewrite(model: onnx.ModelProto) -> tuple[onnx.ModelProto, dict[str, int]]:
    g = model.graph
    inits = {i.name: i for i in g.initializer}
    shapes = {v.name: [d.dim_value for d in v.type.tensor_type.shape.dim]
              for v in list(g.value_info) + list(g.input) + list(g.output)}
    producer = {o: n for n in g.node for o in n.output}
    counts = {"strided_conv": 0, "se_conv": 0, "layernorm_div": 0, "pow2": 0}
    out: list[onnx.NodeProto] = []
    uid = iter(range(1 << 30))

    def name(base: str) -> str:
        return f"{base}__npu{next(uid)}"

    def const(arr: np.ndarray, base: str) -> str:
        n = name(base)
        g.initializer.append(numpy_helper.from_array(arr, n))
        return n

    for n in g.node:
        attrs = {a.name: helper.get_attribute_value(a) for a in n.attribute}
        if n.op_type == "Conv":
            strides = list(attrs.get("strides", [1, 1]))
            w = inits.get(n.input[1])
            xs = shapes.get(n.input[0])
            if w is not None and xs and len(xs) == 4 and xs[2] == 1 and xs[3] == 1 and list(w.dims[2:]) == [1, 1] and attrs.get("group", 1) == 1:
                wt = numpy_helper.to_array(w)  # (Co, Ci, 1, 1)
                co, ci = wt.shape[:2]
                r = name("se_in")
                out.append(helper.make_node("Reshape", [n.input[0], const(np.array([1, 1, ci], np.int64), "shape")], [r]))
                m = name("se_mul")
                out.append(helper.make_node("Mul", [r, const(wt.reshape(1, co, ci), "w")], [m]))
                s = name("se_sum")
                out.append(helper.make_node("ReduceSum", [m], [s], axes=[2], keepdims=1))
                y = n.output[0] if len(n.input) < 3 else name("se_out")
                out.append(helper.make_node("Reshape", [s, const(np.array([1, co, 1, 1], np.int64), "shape")], [y]))
                if len(n.input) >= 3:
                    b = numpy_helper.to_array(inits[n.input[2]]).reshape(1, co, 1, 1)
                    out.append(helper.make_node("Add", [y, const(b, "b")], [n.output[0]]))
                counts["se_conv"] += 1
                continue
            if strides != [1, 1]:
                t = name("conv_s1")
                a = dict(attrs, strides=[1, 1])
                out.append(helper.make_node("Conv", list(n.input), [t], name=n.name, **a))
                out.append(helper.make_node("MaxPool", [t], [n.output[0]], kernel_shape=[1, 1], strides=strides))
                counts["strided_conv"] += 1
                continue
        if n.op_type == "Pow":
            e = inits.get(n.input[1])
            if e is not None and float(numpy_helper.to_array(e).reshape(-1)[0]) == 2.0:
                out.append(helper.make_node("Mul", [n.input[0], n.input[0]], list(n.output)))
                counts["pow2"] += 1
                continue
        if n.op_type == "Div":
            d = producer.get(n.input[1])
            if d is not None and d.op_type == "Sqrt":
                p = name("rsqrt")
                out.append(helper.make_node("Pow", [d.input[0], const(np.array(-0.5, np.float32), "half")], [p]))
                out.append(helper.make_node("Mul", [n.input[0], p], list(n.output)))
                counts["layernorm_div"] += 1
                continue
        out.append(n)
    del g.node[:]
    g.node.extend(out)
    # Drop Sqrt nodes nothing reads any more.
    used = {i for n in g.node for i in n.input} | {o.name for o in g.output}
    keep = [n for n in g.node if n.op_type != "Sqrt" or n.output[0] in used]
    del g.node[:]
    g.node.extend(keep)
    # ...and the weights of the replaced convolutions.
    used = {i for n in g.node for i in n.input}
    live = [i for i in g.initializer if i.name in used]
    del g.initializer[:]
    g.initializer.extend(live)
    return model, counts


def expose(model: onnx.ModelProto) -> int:
    """Adds every convolution's input and output as extra network outputs (see the module doc)."""
    g = model.graph
    info = {v.name: v for v in g.value_info}
    have = {o.name for o in g.output}
    names = [t for n in g.node if n.op_type == "Conv" for t in (n.input[0], n.output[0])]
    # The strided-conv rewrite puts a MaxPool after the conv: its output is the conv's real output.
    names += [n.output[0] for n in g.node if n.op_type == "MaxPool"]
    added = 0
    for t in dict.fromkeys(names):
        if t in info and t not in have:
            g.output.append(info[t])
            added += 1
    return added


def check(a: str, b: str, shape: tuple[int, ...], runs: int = 3) -> float:
    """Largest output difference between two graphs on random inputs."""
    import onnxruntime as ort

    sa, sb = (ort.InferenceSession(p, providers=["CPUExecutionProvider"]) for p in (a, b))
    rng, worst = np.random.default_rng(0), 0.0
    for _ in range(runs):
        x = rng.uniform(-1, 1, shape).astype(np.float32)
        ya = sa.run(None, {sa.get_inputs()[0].name: x})[0]
        yb = sb.run(None, {sb.get_inputs()[0].name: x})[0]  # outputs[0]: the real output
        worst = max(worst, float(np.abs(ya - yb).max()))
    return worst


def main() -> None:
    src, dst = [a for a in sys.argv[1:] if not a.startswith("--")][:2]
    model = onnx.load(src)
    # ponytail: folding is exact but cost accuracy on the chip (1.60% vs 1.48%), so it's opt-in.
    folded = fold_affine(model) if "--fold" in sys.argv else {}
    del model.graph.value_info[:]
    model = onnx.shape_inference.infer_shapes(model)
    model, counts = rewrite(model)
    counts.update(folded)
    model = onnx.shape_inference.infer_shapes(model)
    counts["exposed"] = expose(model)
    onnx.checker.check_model(model)
    onnx.save(model, dst)
    shape = tuple(d.dim_value for d in model.graph.input[0].type.tensor_type.shape.dim)
    diff = check(src, dst, shape)
    print(f"{dst}: {counts}, max difference {diff:.2e}")
    if diff > 1e-3:
        sys.exit("the rewritten graph doesn't match the original")


if __name__ == "__main__":
    main()
