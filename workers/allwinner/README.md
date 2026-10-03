# Docveta OCR worker for the Allwinner A733 NPU

Reads scans and photos on Allwinner A733 boards such as the Radxa Cubie A7A: the NPU finds
the text on each page (the heavy part), and the processor reads the lines with the same
PaddleOCR models as Docveta's GPU/CPU engine, so the text is just as accurate.

Why not read on the NPU too? We tried: this NPU can't run PaddleOCR's text-reading network
accurately (int16 output drifts to nothing, int8 gets about one character in five wrong,
float16 is 20× slower and still wrong). Detection works well on it.

## Install

On the board (bare metal, a VM, or a container that has `/dev/vipcore`):

```bash
curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh
sudo sh get-docveta.sh
```

It finds the NPU and installs Docveta together with this engine:

- the prebuilt detection model (`det.nb`, see [prebuilt/](prebuilt/README.md)) and the reading
  models come with the download
- Allwinner's two VIPLite libraries are fetched from Radxa's `ai-sdk` repository, pinned to a
  fixed version and checked against their SHA-256 (they can't be redistributed)
- a check reads a test image on the NPU before anything is switched on
- Docveta starts the engine itself and gives it a token; there's no separate service

Then open the address it prints and click **Save and start**.

**Proxmox:** first run `sudo sh get-docveta.sh --proxmox <CTID>` on the host. It passes
`/dev/vipcore` into the container and pins it to the two fast Cortex-A76 cores (reading a line
takes about 0.3 s on an A76 and 1.5 s on the A55s). Then run the install inside the container.

The host kernel needs the `vipcore` NPU driver: Radxa's image and Armbian's
`vendor-sun60iw2` kernel have it (`ls /dev/vipcore`). Its version file may say 1.13.0; the
driver is VIPLite 2.0 either way.

## Check it

```bash
/opt/docveta-worker-allwinner/docveta-worker-allwinner --probe
```

```
Detection on the NPU: input (1, 3, 960, 960) uint8 affine …, 77.1 ms per tile
Test image: 2 line(s) in … ms
  1.00  Electricity bill dated 05/08/2026
  1.00  Amount due: 1842.00 INR
OK
```

## Docveta on another machine

The engine can also connect to a Docveta server elsewhere. Install the package from the
[releases page](https://github.com/anand34577/docveta/releases)
(`docveta-worker-allwinner-<version>-linux-arm64.tar.gz`, unpacked to `/opt`), put the two
VIPLite libraries in its `viplite/` folder, then run it with `DOCVETA_URL` and a token from
*Administration → Processing → Add worker* in `DOCVETA_WORKER_TOKEN` (for example as a systemd
service with `EnvironmentFile=`).

## Tuning

| Setting | Default | Notes |
|---|---|---|
| `DOCVETA_CONCURRENCY` | 2 | Pages at once: one is detected on the NPU while another is read on the CPU. |
| `DOCVETA_CPU_THREADS` | 1 | CPU threads for reading, per page. |

## How it works

- The NPU only runs fixed input sizes, so pages are detected in overlapping square tiles;
  pieces of a line split between tiles are joined again.
- PaddleOCR needs normalised input in BGR order, and the NPU wants it quantised. The worker
  does both with one 256-entry lookup table per colour channel, written straight into the
  NPU's memory.
- Lines are read on the CPU with ONNX Runtime, with the same models and preparation
  (`docveta_worker.ppocr`) as Docveta's GPU/CPU engine.

## Rebuilding det.nb (advanced)

Only needed to change the model. See [prebuilt/README.md](prebuilt/README.md) and
`convert/convert.py`: an ONNX export with calibration images on any x86-64 PC, then
compilation in Allwinner's ACUITY container (`ubuntu-npu:v2.0.10.2`, from Allwinner's download
area, see [Radxa's guide](https://docs.radxa.com/en/cubie/a7a/app-dev/npu-dev/cubie-acuity-env)).

## Troubleshooting

| Problem | What to do |
|---|---|
| `/dev/vipcore not found` | The kernel has no NPU driver, or the container doesn't have the device (`get-docveta.sh --proxmox <CTID>` on the host). |
| `cannot load VIPLite` | `libNBGlinker.so` and `libVIPhal.so` are missing from `viplite/`: run the installer again. |
| `fail to read device vipcore` | Those are VIPLite 1.13 libraries; the installer uses the 2.0 ones. |
| `load … failed (VIPLite status -10)` | `det.nb` was compiled for a different NPU. |
| Pages are slow | Check the cores: `cat /sys/fs/cgroup/cpuset.cpus.effective`. Reading on an A55 is about 5× slower than on an A76. |
