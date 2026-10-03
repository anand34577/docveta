# Prebuilt detection model for the Allwinner A733 NPU

`det.nb` is PaddleOCR's PP-OCRv4 text detection model compiled for the A733 NPU
(`VIP9000NANODI_PLUS_PID0X1000003B`), uint8, 960×960 input. The release workflow copies it
into the worker package, so nobody needs Allwinner's toolkit to install Docveta.

SHA-256: `df50a796541e5215de6fd4564760bbc38b559425b722b8f0b7826cc8d1f012c3`

It was built with the converter in `../convert`, calibrated on generated pages (no real
documents), so it can be rebuilt the same way:

```bash
cd workers/allwinner/convert
python synthetic_pages.py --out synth --fonts <folder with NotoSans-Regular.ttf and NotoSansDevanagari-Regular.ttf>
python convert.py onnx --calib-dir synth --work work
# inside Allwinner's ACUITY container ubuntu-npu:v2.0.10.2 (ACUITY 6.30.22), from workers/:
python3 convert.py nb --work work --out ../prebuilt
```

Toolchain: paddle2onnx 1.3.1, onnxsim 0.4.36, PaddlePaddle 3.3.1, ACUITY 6.30.22.
