# Docveta OCR worker for the Allwinner A733 NPU

This worker reads scans and photos on Allwinner A733 boards, such as the Radxa Cubie A7A.
Finding the text on a page, the heaviest step, runs on the NPU. Reading each line then runs
on the CPU, with the same PaddleOCR models as Docveta's GPU/CPU engine, so the text comes out
just as accurate.

Why not read on the NPU too? We tried: this NPU can't run PaddleOCR's text-reading network
accurately. In int16 its output drifts to nothing, int8 gets about one character in five
wrong, and float16 is 20× slower and still wrong. Detection is far more tolerant and works
well.

Docveta sends pages to this worker before CPU engines, because it reports the tag `npu`
(*Administration → Processing → Prefer workers tagged*).

## 1. Convert the detection model (once, on an x86_64 Linux PC)

The release package already contains the reading models. You only have to compile the
detection model, `det.nb`, for the NPU.

First, export it to ONNX and prepare calibration samples from your own documents. Use any
Python 3.10–3.12 venv and 20–200 scans or photos that look like the ones you'll process:

```bash
cd workers/allwinner/convert
python -m venv .venv && . .venv/bin/activate
pip install paddlepaddle "paddle2onnx==1.3.1" onnx "onnxsim==0.4.36" pillow setuptools
python convert.py onnx --calib-dir ~/Scans --work work
```

Then compile it in Allwinner's ACUITY container. Radxa explains where to download
`docker_images_v2.0.x.zip` in their
[ACUITY environment guide](https://docs.radxa.com/en/cubie/a7a/app-dev/npu-dev/cubie-acuity-env).
After `docker load`, run this from the `workers/` folder (the converter reuses code from
`rknn/`):

```bash
docker run --rm --ipc=host -v "$PWD:/workspace" -w /workspace/allwinner/convert ubuntu-npu:v2.0.10.2 bash -c \
  "ACUITY_PATH=/root/acuity-toolkit-whl-6.30.22/bin VIV_SDK=/root/Vivante_IDE/VivanteIDE5.11.0/cmdtools python3 convert.py nb --work work --out ../models"
```

This takes a few minutes and writes `workers/allwinner/models/det.nb`. In the
`ubuntu-npu:v2.0.10.2` image the toolkit is at the paths above. For other image versions, look
inside the container's `/root` folder.

| Option | Default | What it changes |
|---|---|---|
| `onnx --det-size` | 960 | Size of the square tiles the page is cut into. Bigger tiles catch smaller print but take longer each. |
| `nb --det-dtype` | `uint8` | Number format on the NPU. `uint8` is the fast path and works well for detection. |

## 2. Prepare the Proxmox host

The NPU driver is part of the host kernel, so Proxmox has to run a kernel that includes
the `vipcore` driver: Radxa's own image, or Armbian's `vendor-sun60iw2` kernel. Check on the
host:

```bash
ls -l /dev/vipcore
```

`vipcore` is usually built into the kernel, so `lsmod` and `dmesg` may show nothing even when
it works.

Give the container access to the device. Proxmox VE 8.1 and newer can do this directly,
also for unprivileged containers:

```bash
pct set <CTID> -dev0 /dev/vipcore,mode=0666
```

On older Proxmox versions, add these two lines to `/etc/pve/lxc/<CTID>.conf` instead.
`<major>` is the first of the two numbers that `ls -l /dev/vipcore` prints (often 199):

```
lxc.cgroup2.devices.allow: c <major>:* rwm
lxc.mount.entry: /dev/vipcore dev/vipcore none bind,optional,create=file
```

**Give the container the fast cores.** The A733 has two fast Cortex-A76 cores (CPUs 6 and 7)
and six slower A55s. Reading a line takes about 0.3 s on an A76 and about 1.5 s on an A55,
so pin the container to the A76s by adding this line to `/etc/pve/lxc/<CTID>.conf`:

```
lxc.cgroup2.cpuset.cpus: 6-7
```

Then restart the container.

## 3. Install in the container (Debian 12 or 13, arm64)

Download the worker from the [releases page](https://github.com/anand34577/docveta/releases)
(`docveta-worker-allwinner-<version>-linux-arm64.tar.gz`). It doesn't need Python.

```bash
VER=0.3.0
curl -fL "https://github.com/anand34577/docveta/releases/download/v$VER/docveta-worker-allwinner-$VER-linux-arm64.tar.gz" | tar -xz -C /opt
```

That gives you `/opt/docveta-worker-allwinner/` with the program and three folders:

| Folder | What goes in it |
|---|---|
| `viplite/` | Allwinner's two VIPLite 2.0 libraries. They aren't in the download because they can't be redistributed. |
| `models/` | Already holds the reading models. Add your `det.nb` from step 1. |
| `fonts/` | Already filled. These fonts are used for the text layer of searchable PDFs. |

Get the libraries from Radxa's `ai-sdk` repository:

```bash
cd /opt/docveta-worker-allwinner/viplite
base=https://github.com/ZIFENG278/ai-sdk/raw/main/viplite-tina/lib/aarch64-none-linux-gnu/v2.0
curl -fLO "$base/libNBGlinker.so" && curl -fLO "$base/libVIPhal.so"
```

Use the `v2.0` libraries even if `/sys/module/vipcore/version` on the host says 1.13.0. The
A733 driver is 2.0 in both Radxa's image and Armbian's `vendor-sun60iw2` kernel. The `v1.13`
libraries fail with `fail to read device vipcore`.

Copy `det.nb` into `/opt/docveta-worker-allwinner/models/`, then check that everything works:

```bash
/opt/docveta-worker-allwinner/docveta-worker-allwinner --probe
```

It shows the VIPLite version, the time per detection tile on the NPU, and the text it reads
from a rendered test image:

```
Detection on the NPU: input (1, 3, 960, 960) uint8 affine scale=0.0186584 zp=114, 77.1 ms per tile
Test image: 2 line(s) in … ms
  1.00  Electricity bill dated 05/08/2026
  1.00  Amount due: 1842.00 INR
OK
```

Add a worker in Docveta (*Administration → Processing → Add worker*) to get a token, and
create `/etc/docveta-worker.env`:

```
DOCVETA_URL=https://docs.example.com
DOCVETA_WORKER_TOKEN=dvt_wrk_...
```

Then `/etc/systemd/system/docveta-worker.service`:

```ini
[Unit]
Description=Docveta OCR worker (Allwinner NPU)
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/docveta-worker.env
ExecStart=/opt/docveta-worker-allwinner/docveta-worker-allwinner
Restart=on-failure
RestartSec=5
# Other containers share the CPU; give them priority.
Nice=10

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload && systemctl enable --now docveta-worker
journalctl -u docveta-worker -f
```

To update later, unpack a newer download over `/opt/docveta-worker-allwinner`. Your
`viplite/` libraries and `det.nb` stay where they are.

### From source instead

```bash
apt install -y python3-venv git fonts-noto-core
git clone --depth 1 https://github.com/anand34577/docveta.git /opt/docveta-src
python3 -m venv /opt/docveta-venv
/opt/docveta-venv/bin/pip install "/opt/docveta-src/workers/sdk-python[heic,ppocr]" onnxruntime
```

Run `/opt/docveta-venv/bin/python /opt/docveta-src/workers/allwinner/worker.py` instead of
the program. Point it at the libraries and models with `DOCVETA_VIPLITE_DIR` and
`DOCVETA_MODELS_DIR`. The models folder needs your `det.nb`, plus `rec_*.onnx` and
`dict_*.txt` from the release's `docveta-ocr-models-<version>.zip`.

## Tuning

| Setting | Default | Notes |
|---|---|---|
| Container CPUs | | Pin to the A76 cores (6-7), see step 2. That's the biggest speed difference. |
| `DOCVETA_CONCURRENCY` | 2 | How many pages the worker handles at once. With 2, one page is detected on the NPU while another is read on the CPU. |
| `DOCVETA_CPU_THREADS` | 1 | CPU threads for reading, per page. More threads don't help when the extra cores are A55s. |

## How it works

- The NPU only runs fixed input sizes, so pages are detected in overlapping square tiles.
  Pieces of a line that were split between tiles are joined again.
- PaddleOCR needs normalised input in BGR order, and the NPU wants that quantised. The worker
  does both with one 256-entry lookup table per colour channel, written straight into the
  NPU's memory, so it costs no more than copying the tile.
- Each detected line is read on the CPU with ONNX Runtime, using the same models and the same
  preparation (`docveta_worker.ppocr`) as Docveta's GPU/CPU engine.

## Troubleshooting

| Problem | What to do |
|---|---|
| `/dev/vipcore not found` | The host kernel has no NPU driver, or the device isn't passed to the container (`pct set … -dev0`). |
| `cannot load VIPLite` | `libNBGlinker.so` and `libVIPhal.so` are missing from `viplite/` (or from `DOCVETA_VIPLITE_DIR`). |
| `fail to read device vipcore`, `vip_init failed (VIPLite status -2)` | Those are the 1.13 libraries. Use the ones from the `v2.0` folder. |
| `load … failed (VIPLite status -10)` | `det.nb` was compiled for a different NPU. The A733 target is `VIP9000NANODI_PLUS_PID0X1000003B`. |
| `No det.nb in …` | Copy the `det.nb` you built in step 1 into `models/`. |
| Pages are slow | Check which cores the container has (`nproc`, and `cat /sys/fs/cgroup/cpuset.cpus.effective`). Reading on an A55 is about 5× slower than on an A76. |
