"""Tests for the ONNX engine's device choice and preprocessing (no models or GPU needed).

    python -m unittest test_onnx      (from workers/onnx, with the SDK installed)
"""

import unittest

import numpy as np

import worker

CPU = "CPUExecutionProvider"
DML = "DmlExecutionProvider"
CUDA = "CUDAExecutionProvider"


def providers(plans):
    return [(p.provider, p.options.get("performance_preference") or p.options.get("device_filter")) for p in plans]


class DevicePlans(unittest.TestCase):
    def test_cpu_always_last(self):
        for device in worker.DEVICES:
            for avail in ([CPU], [DML, CPU], [CUDA, CPU], ["QNNExecutionProvider", CPU]):
                plans = worker.plans_for(device, avail)
                self.assertEqual(plans[-1].provider, CPU)
                self.assertEqual(plans[-1].kind, "cpu")

    def test_cpu_only_machine(self):
        self.assertEqual([p.provider for p in worker.plans_for("auto", [CPU])], [CPU])
        self.assertEqual([p.provider for p in worker.plans_for("gpu", [CPU])], [CPU])

    def test_windows_directml(self):
        auto = providers(worker.plans_for("auto", [DML, CPU]))
        self.assertEqual(auto[0], (DML, "high_performance"))  # dedicated / external GPU first
        igpu = providers(worker.plans_for("igpu", [DML, CPU]))
        self.assertEqual(igpu[0], (DML, "minimum_power"))
        npu = worker.plans_for("npu", [DML, CPU])
        self.assertEqual((npu[0].provider, npu[0].options, npu[0].kind), (DML, {"device_filter": "npu"}, "npu"))
        explicit = worker.plans_for("gpu", [DML, CPU], gpu_id=1)
        self.assertEqual(explicit[0].options, {"device_id": 1})

    def test_cuda_preferred_and_skipped_for_igpu(self):
        self.assertEqual(worker.plans_for("auto", [CUDA, DML, CPU])[0].provider, CUDA)
        self.assertNotIn(CUDA, [p.provider for p in worker.plans_for("igpu", [CUDA, DML, CPU])])

    def test_cpu_setting_ignores_gpus(self):
        self.assertEqual([p.provider for p in worker.plans_for("cpu", [CUDA, DML, CPU])], [CPU])

    def test_unknown_setting_means_auto(self):
        self.assertEqual(worker.plans_for("fastest", [DML, CPU])[0].provider, DML)


class Preprocessing(unittest.TestCase):
    def test_rec_input_shape_and_padding(self):
        crop = np.full((30, 200, 3), 255, dtype=np.uint8)
        x, frac = worker.rec_input(crop)
        self.assertEqual(x.shape[:3], (1, 3, 48))
        self.assertEqual(x.shape[3] % 160, 0)
        content = int(round(frac * x.shape[3]))
        self.assertAlmostEqual(float(x[0, 0, 0, 0]), 1.0, places=5)  # white → 1
        if content < x.shape[3]:
            self.assertEqual(float(x[0, 0, 0, -1]), 0.0)  # padding → 0

    def test_det_infer_feeds_normalised_bgr_nchw(self):
        seen = {}

        class FakeSession:
            def get_inputs(self):
                return [type("I", (), {"name": "x"})()]

            def run(self, _, feeds):
                seen["x"] = feeds["x"]
                return [np.zeros((1, 1, 32, 32), dtype=np.float32)]

        img = np.zeros((1, 32, 32, 3), dtype=np.uint8)
        img[..., 0] = 255  # pure red in RGB
        worker.det_infer(FakeSession(), img)
        x = seen["x"]
        self.assertEqual(x.shape, (1, 3, 32, 32))
        # Channel 2 (R after BGR swap) is bright, channel 0 (B) is dark.
        self.assertGreater(x[0, 2].mean(), x[0, 0].mean())


if __name__ == "__main__":
    unittest.main()
