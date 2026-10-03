# PyInstaller build of the Allwinner NPU worker (one folder, no Python needed to run it).
#
#   pip install "../sdk-python[heic,ppocr]" onnxruntime pyinstaller
#   pyinstaller docveta-worker-allwinner.spec
#
# Output: dist/docveta-worker-allwinner/. The release workflow adds fonts/, the reading models
# (models/rec_*.onnx + dict_*.txt) and an empty viplite/ folder. VIPLite itself isn't included:
# Allwinner's libraries can't be redistributed, so users copy them into viplite/ (see README.md).
from PyInstaller.utils.hooks import collect_all, collect_data_files, collect_dynamic_libs

datas, binaries, hiddenimports = [], [], []
for pkg in ("onnxruntime", "pypdfium2", "pypdfium2_raw"):
    d, b, h = collect_all(pkg)
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
    excludes=["tkinter", "matplotlib", "onnx", "IPython", "pytest"],
)
a.binaries = [b for b in a.binaries if "opencv_videoio_ffmpeg" not in b[0]]
pyz = PYZ(a.pure)
exe = EXE(pyz, a.scripts, [], exclude_binaries=True, name="docveta-worker-allwinner", console=True, upx=False)
coll = COLLECT(exe, a.binaries, a.datas, name="docveta-worker-allwinner", upx=False)
