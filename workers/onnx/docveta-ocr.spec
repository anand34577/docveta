# PyInstaller build of the Docveta OCR engine (one folder, no Python needed to run it).
#
#   pip install "../sdk-python[heic,ppocr]" onnxruntime-directml pyinstaller   # Windows (any GPU)
#   pip install "../sdk-python[heic,ppocr]" onnxruntime pyinstaller            # Linux / macOS
#   pyinstaller docveta-ocr.spec
#
# Output: dist/docveta-ocr/ (docveta-ocr[.exe] + _internal/). The release workflow adds
# models/ and fonts/ next to the executable and ships the folder as ocr/ beside docveta.
# One-folder mode on purpose: a single process that Docveta can stop cleanly, and fast start.
from PyInstaller.utils.hooks import collect_all, collect_data_files, collect_dynamic_libs

datas, binaries, hiddenimports = [], [], []
for pkg in ("onnxruntime", "pypdfium2", "pypdfium2_raw"):
    d, b, h = collect_all(pkg)  # native libraries (DirectML.dll, pdfium) and data
    datas += d
    binaries += b
    hiddenimports += h
# Model-building tools inside onnxruntime aren't needed to run models.
_unused = ("onnxruntime/tools", "onnxruntime/transformers", "onnxruntime/quantization", "onnxruntime/datasets")
datas = [d for d in datas if not any(u in d[1].replace("\\", "/") for u in _unused)]
datas += collect_data_files("reportlab")
binaries += collect_dynamic_libs("pillow_heif")
hiddenimports += ["pillow_heif"]

a = Analysis(
    ["worker.py"],
    binaries=binaries,
    datas=datas,
    hiddenimports=hiddenimports,
    excludes=["tkinter", "matplotlib", "paddle", "paddle2onnx", "onnx", "IPython", "pytest"],
)
# OpenCV's video/FFmpeg backend isn't used for OCR (saves ~30 MB).
a.binaries = [b for b in a.binaries if "opencv_videoio_ffmpeg" not in b[0]]
pyz = PYZ(a.pure)
exe = EXE(pyz, a.scripts, [], exclude_binaries=True, name="docveta-ocr", console=True, upx=False)
coll = COLLECT(exe, a.binaries, a.datas, name="docveta-ocr", upx=False)
