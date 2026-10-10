# Docveta OCR worker for the Allwinner A733 NPU

Reads scans and photos on Allwinner A733 boards such as the Radxa Cubie A7A, on the NPU: it
finds the text on each page and reads every line with PaddleOCR's PP-OCRv5 models, exactly as
accurately as Docveta's GPU/CPU engine, about 7x faster than a Cortex-A76 core. The processor
only prepares the images.

Reading on this NPU needed a trick: compiled as they are, the reading models read nearly every
line as blank on the chip (in int16, uint8 and even float16), although Allwinner's simulator
reads them right. The A733's compiler merges layers in a way the chip computes wrongly; making
each convolution's input and output a network output stops that (see
[convert/npu_rewrite.py](convert/npu_rewrite.py)).

## Install

On the board (bare metal, a VM, or a container that has `/dev/vipcore`):

```bash
curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh
sudo sh get-docveta.sh
```

It finds the NPU and installs Docveta together with this engine:

- the prebuilt NPU models (`det.nb` and `rec_<script>_<width>.nb`, see [prebuilt/](prebuilt/README.md))
  and the reading dictionaries come with the download
- Allwinner's two VIPLite libraries are fetched from Radxa's `ai-sdk` repository, pinned to a
  fixed version and checked against their SHA-256 (they can't be redistributed)
- a check reads a test image on the NPU before anything is switched on
- Docveta starts the engine itself and gives it a token; there's no separate service

Then open the address it prints and click **Save and start**.

**Proxmox:** first run `sudo sh get-docveta.sh --proxmox <CTID>` on the host. It passes
`/dev/vipcore` into the container and pins it to the two fast Cortex-A76 cores. Then run the
install inside the container.

The host kernel needs the `vipcore` NPU driver: Radxa's image and Armbian's
`vendor-sun60iw2` kernel have it (`ls /dev/vipcore`). Its version file may say 1.13.0; the
driver is VIPLite 2.0 either way.

## Check it

```bash
/opt/docveta-worker-allwinner/docveta-worker-allwinner --probe
```

```
Detection on the NPU: input (1, 3, 960, 960) uint8 affine …, 77.1 ms per tile
Reading on the NPU: rec_devanagari_320.nb, input (1, 3, 48, 320) int16 dfp fl=15, 38 ms per line
…
Test lines, devanagari: 0.0% character errors
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
| `DOCVETA_CONCURRENCY` | 2 | Pages at once: the processor prepares one while the NPU works on another. |
| `DOCVETA_NPU_READ` | `npu` | `cpu` reads lines on the processor (ONNX Runtime) instead. |
| `DOCVETA_CPU_THREADS` | 1 | Processor threads per page, when reading on the processor. |

## How it works

- The NPU only runs fixed input sizes, so pages are detected in overlapping square tiles;
  pieces of a line split between tiles are joined again.
- PaddleOCR needs normalised input in BGR order, and the NPU wants it quantised. The worker
  does both with one 256-entry lookup table per colour channel, written straight into the
  NPU's memory.
- Lines are read with the same models and preparation (`docveta_worker.ppocr`) as Docveta's
  GPU/CPU engine, in int16. The NPU needs fixed input sizes, so each script has a reader for
  lines up to 320, 640 and 960 pixels wide (at a height of 48); wider lines are cut at gaps
  between words and the pieces read separately. Each reader takes NPU memory (24 MB at 320,
  three times that at 960).
- At start the worker reads test lines on the NPU; if a reader misreads them (a wrong or broken
  model), it logs an error and reads on the processor rather than return empty pages.

## Rebuilding the NPU models (advanced)

Only needed to change the models. See [prebuilt/README.md](prebuilt/README.md) and
`convert/convert.py`: an ONNX export with calibration images on any x86-64 PC, then
compilation in Allwinner's ACUITY container (`ubuntu-npu:v2.0.10.2`, from Allwinner's download
area, see [Radxa's guide](https://docs.radxa.com/en/cubie/a7a/app-dev/npu-dev/cubie-acuity-env)).

## Troubleshooting

| Problem | What to do |
|---|---|
| `/dev/vipcore not found` | The kernel has no NPU driver, or the container doesn't have the device (`get-docveta.sh --proxmox <CTID>` on the host). |
| `cannot load VIPLite` | `libNBGlinker.so` and `libVIPhal.so` are missing from `viplite/`: run the installer again. |
| `fail to read device vipcore` | Those are VIPLite 1.13 libraries; the installer uses the 2.0 ones. |
| `load … failed (VIPLite status -10)` | A `.nb` file was compiled for a different NPU. |
| `the NPU readers misread their test lines` | The `rec_*.nb` files don't match this version: install the package again, or rebuild them (above). |
| `run … failed (VIPLite status -1)` on every page | The NPU hung (a broken model can do that): reload the driver on the host (`rmmod vipcore && modprobe vipcore`) or reboot it. |
| Pages are slow | Check the cores: `cat /sys/fs/cgroup/cpuset.cpus.effective`. Preparing pages on an A55 is about 5× slower than on an A76. |
