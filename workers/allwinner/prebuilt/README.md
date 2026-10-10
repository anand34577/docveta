# Prebuilt models for the Allwinner A733 NPU

Compiled for the A733 NPU (`VIP9000NANODI_PLUS_PID0X1000003B`) with ACUITY 6.30.22. The release
workflow copies them into the worker package, so nobody needs Allwinner's toolkit to install
Docveta. All were calibrated on generated pages and text lines (no real documents), so they can be
rebuilt the same way.

| File | Model | Input |
|---|---|---|
| `det.nb` | PP-OCRv4 text detection, uint8 | 960×960 |
| `rec_<script>_<width>.nb` | PP-OCRv5 reading (Kannada: PP-OCRv3), int16, rewritten by `../convert/npu_rewrite.py` | 48×320, 48×640, 48×960 |

The reading models need the dictionaries (`dict_<script>.txt`) from the release's ONNX models.
On a Cubie A7A each reads its held-out test lines exactly as the CPU (ONNX Runtime) does.

SHA-256:

```
df50a796541e5215de6fd4564760bbc38b559425b722b8f0b7826cc8d1f012c3  det.nb
e2c35a95c73e2d5520c5eabd679489121e41cfa8403aba9fa0fda72c0881b616  rec_devanagari_320.nb
f58f9e1765384ed90a46a7070c61f47e97c765d3e85684d4c5a13ae40be49e04  rec_devanagari_640.nb
d8c37423c4672d6a6ec17739c4df21dea003fdc088381eae7be18a702aba1e62  rec_devanagari_960.nb
1f5bdf5419aa3b299ab968cbfb1979707f1954e67188d9bf1b4cb60017e5ab17  rec_en_320.nb
6d55fe8dbf482318c51046e54d9bef720293e7ecd3af80bf77b2886234405444  rec_en_640.nb
35ff987cb20351abec4e56c8b0990eb61738c16660daadbf4b3a4a03ca4be981  rec_en_960.nb
e76c3b4342871179dea1be680d04daa5c8dd81a735e8ee881acc32b064548bf8  rec_ka_320.nb
b373c021c08930e841a997bd3929ac81239292684935998ae3ca9272578f0b67  rec_ka_640.nb
9735159be29a6b00e9568b280b96a07247532cb6b3c7185f1554942833844ec2  rec_ka_960.nb
579076f1529eec05b2f0c5c8e77f36b9770ef6c944537fe080304e94a2371401  rec_ta_320.nb
666ec40c17650dbcd6bd8efadba7fd3215b1c1b9f1d7adba6282f589662c5612  rec_ta_640.nb
bcbe22c851420a5504a4f16d8b09ee51370e1dff736bca31f86b6e0cf09dd188  rec_ta_960.nb
6ae95820c6d52e32a4461abd05893f0ee93c2ffae7c5bc79e7f0e0596980b391  rec_te_320.nb
0f2cc82378db9dd5f391008ad1206193f378e667c3a4b6300359278b624a4a13  rec_te_640.nb
83a5fc9e7a7b152e610c98bc70941d83f3efd20184ae721e5e0291c7946799e9  rec_te_960.nb
```

Rebuild them with the converter in `../convert`:

```bash
cd workers/allwinner/convert
python synthetic_pages.py --out synth --fonts <Noto fonts folder>
python convert.py onnx --calib-dir synth --fonts <Noto fonts folder> --work work
# inside Allwinner's ACUITY container ubuntu-npu:v2.0.10.2 (ACUITY 6.30.22), from workers/:
python3 convert.py nb --work work --out ../prebuilt
```

The fonts are the release's Noto fonts (NotoSans, NotoSansDevanagari, NotoSansTamil, NotoSansTelugu,
NotoSansKannada); step 1 needs Pillow with libraqm to shape them. Toolchain: PaddlePaddle 3.2.0,
paddle2onnx 2.1.0, onnxsim 0.4.36, ACUITY 6.30.22. `det.nb` was built with paddle2onnx 1.3.1 and
PaddlePaddle 3.3.1 (the detection model is unchanged).
