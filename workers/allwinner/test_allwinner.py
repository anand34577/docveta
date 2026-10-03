"""Tests for the Allwinner worker's tensor (de)quantisation (no NPU needed).

    python -m unittest test_allwinner      (from workers/allwinner, with the SDK installed)
"""

import unittest

import numpy as np

import worker as w


def roundtrip(x, fmt, quant, **q):
    out = np.zeros(x.shape, dtype=w.NP_DTYPE[fmt])
    w.quantize(x, out, fmt, quant, **q)
    return out, w.dequantize(out, fmt, quant, **q)


class Quantisation(unittest.TestCase):
    pixels = np.arange(256, dtype=np.uint8).reshape(1, 1, 16, 16)

    def test_uint8_identity_is_raw_copy(self):
        out, back = roundtrip(self.pixels, w.FMT_UINT8, w.Q_AFFINE, scale=1.0, zero_point=0)
        np.testing.assert_array_equal(out, self.pixels)
        np.testing.assert_array_equal(back, self.pixels.astype(np.float32))

    def test_uint8_affine(self):
        out, back = roundtrip(self.pixels, w.FMT_UINT8, w.Q_AFFINE, scale=2.0, zero_point=10)
        self.assertEqual(out[0, 0, 0, 0], 10)
        self.assertEqual(out.max(), 138)  # rint(255/2 + 10)
        np.testing.assert_allclose(back, self.pixels, atol=1.0)

    def test_int16_dfp(self):
        _, back = roundtrip(self.pixels, w.FMT_INT16, w.Q_DFP, fl=7)
        np.testing.assert_allclose(back, self.pixels, atol=1 / 128)

    def test_int16_dfp_clips(self):
        out, _ = roundtrip(self.pixels, w.FMT_INT16, w.Q_DFP, fl=10)  # 255*1024 > int16
        self.assertEqual(out.max(), 32767)

    def test_bf16_and_fp16(self):
        x = np.linspace(0, 1, 64, dtype=np.float32).reshape(1, 1, 8, 8)
        _, back = roundtrip(x, w.FMT_BFP16, w.Q_NONE)
        np.testing.assert_allclose(back, x, atol=1 / 128)
        _, back = roundtrip(x, w.FMT_FP16, w.Q_NONE)
        np.testing.assert_allclose(back, x, atol=1e-3)

    def test_nchw_from_nhwc_view(self):
        nhwc = np.random.default_rng(0).integers(0, 256, (1, 4, 6, 3), dtype=np.uint8)
        out = np.zeros((1, 3, 4, 6), dtype=np.uint8)
        w.quantize(nhwc.transpose(0, 3, 1, 2), out, w.FMT_UINT8, w.Q_AFFINE)
        np.testing.assert_array_equal(out[0, 1], nhwc[0, :, :, 1])


class InputNormalisation(unittest.TestCase):
    """The NPU input must equal the ONNX engine's PP-OCR detection preprocessing (BGR, normalised)."""

    img = np.random.default_rng(1).integers(0, 256, (1, 6, 8, 3), dtype=np.uint8)

    def npu_input(self, norm, dtype, fmt, quant, **q):
        out = np.zeros((1, 3, 6, 8), dtype=dtype)
        w.write_input(w.input_lut(norm, np.dtype(dtype), fmt, quant, **q), self.img, out)
        return w.dequantize(out, fmt, quant, **q)

    def test_detection_uint8(self):  # quantisation reported by det.nb on a Cubie A7A
        got = self.npu_input(w.DET_NORM, np.uint8, w.FMT_UINT8, w.Q_AFFINE, scale=0.0186584, zero_point=114)
        bgr = self.img[..., ::-1].astype(np.float32) / 255
        ref = ((bgr - [0.485, 0.456, 0.406]) / [0.229, 0.224, 0.225]).transpose(0, 3, 1, 2)
        np.testing.assert_allclose(got, ref, atol=0.0186584 / 2 + 1e-6)


if __name__ == "__main__":
    unittest.main()
