# Docveta OCR worker for the Allwinner A733 NPU

This worker reads scans and photos on the NPU of Allwinner A733 boards, such as the Radxa
Cubie A7A. Both halves of PaddleOCR (finding the text lines and reading them) run on the NPU.
The CPU only cuts pages into tiles and lines, so the board stays usable for other things,
for example other containers under Proxmox.

The models have to be compiled for the NPU once with Allwinner's ACUITY Toolkit. The worker
then talks to the NPU through Allwinner's VIPLite library and `/dev/vipcore`.

Docveta sends pages to this worker before CPU engines, because it reports the tag `npu`
(*Administration → Processing → Prefer workers tagged*).

## 1. Convert the models (once, on an x86_64 Linux PC)

First, export the PaddleOCR models to ONNX and cut calibration samples from your own
documents. Use any Python 3.10–3.12 venv and 20–200 scans or photos that look like the ones
you'll be processing:

```bash
cd workers/allwinner/convert
python -m venv .venv && . .venv/bin/activate
pip install paddlepaddle "paddle2onnx==1.3.1" onnx onnxsim pillow
python convert.py onnx --scripts en devanagari --calib-dir ~/Scans --work work
```

Then compile them in Allwinner's ACUITY container. Radxa explains where to download
`docker_images_v2.0.x.zip` in their
[ACUITY environment guide](https://docs.radxa.com/en/cubie/a7a/app-dev/npu-dev/cubie-acuity-env).
After `docker load`, run this from the `workers/` folder (the converter reuses code from
`rknn/`):

```bash
docker run --rm --ipc=host -v "$PWD:/workspace" -w /workspace/allwinner/convert ubuntu-npu:v2.0.10.2 \
  bash -lc "python3 convert.py nb --work work --out ../models"
```

If the container doesn't already set `ACUITY_PATH` (the folder containing `pegasus`) and
`VIV_SDK` (the Vivante IDE `cmdtools/vsimulator` folder), export them inside the
`bash -lc "…"` string.

You end up with `workers/allwinner/models/`: `det.nb`, `rec_<script>_320.nb` up to
`rec_<script>_1280.nb`, `dict_<script>.txt` and `VERSION`. Available scripts are `en`
(English and other Latin-alphabet languages), `devanagari` (Hindi, Marathi, Nepali, Sanskrit;
it reads English too), `ta`, `te` and `ka`.

| Option | Default | What it changes |
|---|---|---|
| `onnx --det-size` | 960 | Size of the square tiles the page is cut into for text detection. Bigger tiles catch smaller print but take longer each. |
| `nb --det-dtype` | `uint8` | Number format for detection. `uint8` is the fastest and works well here. |
| `nb --rec-dtype` | `int16` | Number format for reading the text. `uint8` is faster but misreads more characters. `bf16` is the most accurate and the slowest. |

## 2. Prepare the Proxmox host

The NPU driver is part of the host kernel, so Proxmox has to run a kernel that includes
the `vipcore` driver: Radxa's own image, or Armbian's `vendor-sun60iw2` kernel. Check on the
host:

```bash
ls -l /dev/vipcore
cat /sys/module/vipcore/version
```

`vipcore` is usually built into the kernel, so `lsmod` and `dmesg` may show nothing even when
it works. Note the driver version for step 3.

Then give the container access to the device. Proxmox VE 8.1 and newer can do this directly,
and it works for unprivileged containers too:

```bash
pct set <CTID> -dev0 /dev/vipcore,mode=0666
```

On older Proxmox versions, add these two lines to `/etc/pve/lxc/<CTID>.conf` instead.
`<major>` is the first of the two numbers that `ls -l /dev/vipcore` prints (often 199):

```
lxc.cgroup2.devices.allow: c <major>:* rwm
lxc.mount.entry: /dev/vipcore dev/vipcore none bind,optional,create=file
```

The worker asks the driver to run the NPU at full clock when it starts. If the host also has
an NPU entry under `/sys/class/devfreq/`, set its governor to `performance` on the host,
because a container can't change it.

## 3. Install in the container (Debian 12 or 13, arm64)

```bash
apt update && apt install -y python3-venv git fonts-noto-core fonts-dejavu-core
git clone --depth 1 https://github.com/anand34577/docveta.git /opt/docveta-src
git clone --depth 1 https://github.com/ZIFENG278/ai-sdk.git /opt/ai-sdk
python3 -m venv /opt/docveta-worker
/opt/docveta-worker/bin/pip install "/opt/docveta-src/workers/sdk-python[heic,ppocr]"
mkdir -p /opt/docveta-worker/models
```

Copy the converted `models/` folder into `/opt/docveta-worker/models`.

The `ai-sdk` repository has two versions of the VIPLite library. Use the one that matches the
driver version from step 2:

| Driver version | `DOCVETA_VIPLITE_DIR` |
|---|---|
| 1.13.x (Armbian `vendor-sun60iw2`) | `/opt/ai-sdk/viplite-tina/lib/aarch64-none-linux-gnu/v1.13` |
| 2.0.x (Radxa image) | `/opt/ai-sdk/viplite-tina/lib/aarch64-none-linux-gnu/v2.0` |

Before connecting to Docveta, check that the NPU and models work:

```bash
cd /opt/docveta-src/workers/allwinner
DOCVETA_MODELS_DIR=/opt/docveta-worker/models \
DOCVETA_VIPLITE_DIR=/opt/ai-sdk/viplite-tina/lib/aarch64-none-linux-gnu/v1.13 \
/opt/docveta-worker/bin/python worker.py --probe
```

The first line shows the VIPLite version and the number of NPU cores. Next comes one line per
model with its input and output formats and how long one run takes, and `NPU OK` at the end.
If no models are installed yet, the probe stops after the first line. That's still a useful
test that the library and driver work together.

Add a worker in Docveta (*Administration → Processing → Add worker*) to get a token, and
create `/etc/docveta-worker.env`:

```
DOCVETA_URL=https://docs.example.com
DOCVETA_WORKER_TOKEN=dvt_wrk_...
DOCVETA_MODELS_DIR=/opt/docveta-worker/models
DOCVETA_VIPLITE_DIR=/opt/ai-sdk/viplite-tina/lib/aarch64-none-linux-gnu/v1.13
```

Then `/etc/systemd/system/docveta-worker.service`:

```ini
[Unit]
Description=Docveta OCR worker (Allwinner NPU)
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/docveta-worker.env
WorkingDirectory=/opt/docveta-src/workers/allwinner
ExecStart=/opt/docveta-worker/bin/python worker.py
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

## Tuning

| Setting | Default | Notes |
|---|---|---|
| `DOCVETA_CONCURRENCY` | 2 (NPU cores + 1) | How many pages the worker handles at once. With 2, one page uses the NPU while the CPU prepares the next one. `1` uses the least CPU, but the NPU then waits between steps. |
| `DOCVETA_NPU_CLOCK_PERCENT` | 100 | NPU clock the worker asks for at start. |
| Container CPU cores | | 1–2 is plenty. Turning PDFs into images and building the searchable PDF are the main CPU work. |

## How it works

- The NPU only runs fixed input sizes. Pages are detected in overlapping square tiles, and
  pieces of a line that were split between tiles are joined again. Text lines are read at
  a height of 48 pixels, in the narrowest of four widths (320, 640, 960, 1280) that fits.
- Each model's input and output memory is allocated by the driver once and used directly
  from Python, so a tile is written straight into NPU memory without extra copies.
- The NPU is locked only while a model runs, not for a whole page, so two pages can take
  turns on it.
- Cutting and post-processing use the same code (`docveta_worker.ppocr`) as the Rockchip and
  ONNX engines.

## Troubleshooting

| Problem | What to do |
|---|---|
| `/dev/vipcore not found` | The host kernel has no NPU driver, or the device isn't passed to the container (`pct set … -dev0`). |
| `cannot load VIPLite` | Point `DOCVETA_VIPLITE_DIR` at the `ai-sdk` folder for your driver version (see step 3). |
| `vip_init failed` or `buffer smaller than tensor` | The library and driver versions don't match. Compare with `cat /sys/module/vipcore/version` on the host. |
| `load … failed (VIPLite status -10)` | The driver can't run this model file. Either it was compiled for a different NPU (the A733 target is `VIP9000NANODI_PLUS_PID0X1000003B`), or a 1.13 driver is rejecting models from the 2.0 ACUITY image. In that case use an older ACUITY image or a kernel with a 2.0 driver. |
| Boxes look right but the text is garbled | Recompile the reading models with `--rec-dtype bf16`. |
| ACUITY fails to import a recognition model | The English model (PP-OCRv4) uses operations some ACUITY versions don't support. Try a newer ACUITY image, or start with `devanagari`, `ta`, `te` or `ka` (PP-OCRv3). |
