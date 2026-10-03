# PyInstaller build of the Allwinner NPU worker (one folder, no Python needed to run it).
#
#   pip install "../sdk-python[heic,ppocr]" pyinstaller
#   pyinstaller docveta-worker-allwinner.spec
#
# Output: dist/docveta-worker-allwinner/. The release workflow adds fonts/ and empty models/
# and viplite/ folders. VIPLite itself isn't included: Allwinner's libraries can't be
# redistributed, so users copy them into viplite/ (see README.md).
from PyInstaller.utils.hooks import collect_all, collect_data_files, collect_dynamic_libs

datas, binaries, hiddenimports = [], [], []
for pkg in ("pypdfium2", "pypdfium2_raw"):
    d, b, h = collect_all(pkg)
    datas += d
    binaries += b
    hiddenimports += h
datas += collect_data_files("reportlab")
binaries += collect_dynamic_libs("pillow_heif")
hiddenimports += ["pillow_heif"]

a = Analysis(
    ["worker.py"],
    binaries=binaries,
    datas=datas,
    hiddenimports=hiddenimports,
    excludes=["tkinter", "matplotlib", "IPython", "pytest"],
)
a.binaries = [b for b in a.binaries if "opencv_videoio_ffmpeg" not in b[0]]
pyz = PYZ(a.pure)
exe = EXE(pyz, a.scripts, [], exclude_binaries=True, name="docveta-worker-allwinner", console=True, upx=False)
coll = COLLECT(exe, a.binaries, a.datas, name="docveta-worker-allwinner", upx=False)
