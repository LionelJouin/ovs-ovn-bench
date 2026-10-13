# Isolated Object Scaling Benchmark: glibc malloc vs jemalloc

**Date:** 2026-10-13 14:13:25
**System:** Linux-7.1.8-200.fc44.x86_64-x86_64-with-glibc2.43
**CPU:** Intel(R) Core(TM) Ultra 7 165H (22 vCPUs)
**Memory:** 62Gi
**Glibc Version:** ldd (GNU libc) 2.43
**Jemalloc Version:** jemalloc-5.3.0-14.fc44.x86_64

---

## Methodology

Each benchmark evaluates an isolated OVN/OVS object type across powers of 10 (`1, 10, 100, 1,000, 10,000, 100,000`) through 3 lifecycle phases:
1. **Baseline (Idle)**: Clean process RSS before creating objects.
2. **After Add (N)**: Time taken to commit $N$ objects, full recompute latency (`ovn-northd-loop` stopwatch), and peak RSS (`VmHWM`).
3. **After Remove (N)**: Time taken to delete all $N$ objects, post-deletion recompute latency, and retained heap memory (`Post-Deletion RSS - Baseline RSS`).

---

## Combined Workload (All objects scaled proportionally)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.036 s | 0.031 s | -13.9% |
| | Add Rate | 27.8 ops/s | 32.3 ops/s | +16.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.05 MiB | 16.30 MiB | +8.3% |
| | Delete Duration | 0.028 s | 0.022 s | -21.4% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.05 MiB | 16.36 MiB | +8.7% |
| | **Retained Heap vs Baseline** | **+0.93 MiB** | **+1.17 MiB** | **+0.24 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.032 s | 0.031 s | -3.1% |
| | Add Rate | 312.5 ops/s | 322.6 ops/s | +3.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.22 MiB | 16.41 MiB | +7.8% |
| | Delete Duration | 0.022 s | 0.023 s | +4.5% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.23 MiB | 16.45 MiB | +8.0% |
| | **Retained Heap vs Baseline** | **+1.09 MiB** | **+1.40 MiB** | **+0.31 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.103 s | 0.095 s | -7.8% |
| | Add Rate | 970.9 ops/s | 1,052.6 ops/s | +8.4% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 2.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 17.49 MiB | 18.07 MiB | +3.3% |
| | Delete Duration | 0.021 s | 0.022 s | +4.8% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 17.57 MiB | 18.60 MiB | +5.9% |
| | **Retained Heap vs Baseline** | **+3.45 MiB** | **+3.55 MiB** | **+0.10 MiB** |
|---|---|---|---|---|
| **1,000** | Add Duration | 1.205 s | 1.116 s | -7.4% |
| | Add Rate | 829.9 ops/s | 896.1 ops/s | +8.0% |
| | Recompute (`ovn-northd-loop`) | 21.0 ms | 12.0 ms | -42.9% |
| | Logical Flow (`lflow`) | 4.0 ms | 3.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 41.09 MiB | 38.30 MiB | -6.8% |
| | Delete Duration | 0.124 s | 0.083 s | -33.1% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 41.51 MiB | 42.32 MiB | +2.0% |
| | **Retained Heap vs Baseline** | **+27.39 MiB** | **+27.30 MiB** | **-0.09 MiB** |
|---|---|---|---|---|
| **5,000** | Add Duration | 16.420 s | 14.032 s | -14.5% |
| | Add Rate | 304.5 ops/s | 356.3 ops/s | +17.0% |
| | Recompute (`ovn-northd-loop`) | 85.0 ms | 68.0 ms | -20.0% |
| | Logical Flow (`lflow`) | 24.0 ms | 15.0 ms | -37.5% |
| | Peak RSS (`VmHWM`) | 141.71 MiB | 124.41 MiB | -12.2% |
| | Delete Duration | 0.927 s | 0.491 s | -47.0% |
| | Post-Delete Recompute | 0.0 ms | 1.0 ms | N/A |
| | Post-Delete RSS | 146.10 MiB | 133.06 MiB | -8.9% |
| | **Retained Heap vs Baseline** | **+132.02 MiB** | **+117.97 MiB** | **-14.05 MiB** |
|---|---|---|---|---|

## Address Set (IP list matching ACL)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.014 s | 0.014 s | ~Parity (±1%) |
| | Add Rate | 71.4 ops/s | 71.4 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 0.0 ms | 1.0 ms | N/A |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.53 MiB | 15.47 MiB | +6.5% |
| | Delete Duration | 0.011 s | 0.012 s | +9.1% |
| | Post-Delete Recompute | 0.0 ms | 1.0 ms | N/A |
| | Post-Delete RSS | 14.53 MiB | 15.47 MiB | +6.5% |
| | **Retained Heap vs Baseline** | **+0.08 MiB** | **+0.07 MiB** | **-0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.010 s | 0.015 s | +50.0% |
| | Add Rate | 1,000.0 ops/s | 666.7 ops/s | -33.3% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.57 MiB | 15.54 MiB | +6.7% |
| | Delete Duration | 0.012 s | 0.010 s | -16.7% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.57 MiB | 15.54 MiB | +6.7% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.07 MiB** | **-0.02 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.010 s | 0.010 s | ~Parity (±1%) |
| | Add Rate | 10,000.0 ops/s | 10,000.0 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.64 MiB | 15.49 MiB | +5.8% |
| | Delete Duration | 0.011 s | 0.013 s | +18.2% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.64 MiB | 15.49 MiB | +5.8% |
| | **Retained Heap vs Baseline** | **+0.16 MiB** | **+0.08 MiB** | **-0.08 MiB** |
|---|---|---|---|---|
| **1,000** | Add Duration | 0.015 s | 0.014 s | -6.7% |
| | Add Rate | 66,666.7 ops/s | 71,428.6 ops/s | +7.1% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 14.75 MiB | 15.84 MiB | +7.4% |
| | Delete Duration | 0.011 s | 0.011 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 14.75 MiB | 15.84 MiB | +7.4% |
| | **Retained Heap vs Baseline** | **+0.24 MiB** | **+0.28 MiB** | **+0.04 MiB** |
|---|---|---|---|---|
| **10,000** | Add Duration | 0.028 s | 0.030 s | +7.1% |
| | Add Rate | 357,142.9 ops/s | 333,333.3 ops/s | -6.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 1.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 16.09 MiB | 17.34 MiB | +7.8% |
| | Delete Duration | 0.011 s | 0.011 s | ~Parity (±1%) |
| | Post-Delete Recompute | 0.0 ms | 0.0 ms | N/A |
| | Post-Delete RSS | 15.93 MiB | 17.32 MiB | +8.7% |
| | **Retained Heap vs Baseline** | **+1.35 MiB** | **+1.84 MiB** | **+0.49 MiB** |
|---|---|---|---|---|

## Port Group (Ports member grouping matching ACL)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.011 s | 0.013 s | +18.2% |
| | Add Rate | 90.9 ops/s | 76.9 ops/s | -15.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 14.55 MiB | 15.49 MiB | +6.5% |
| | Delete Duration | 0.011 s | 0.010 s | -9.1% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.55 MiB | 15.49 MiB | +6.5% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.11 MiB** | **+0.02 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.012 s | 0.014 s | +16.7% |
| | Add Rate | 833.3 ops/s | 714.3 ops/s | -14.3% |
| | Recompute (`ovn-northd-loop`) | 0.0 ms | 1.0 ms | N/A |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.62 MiB | 15.59 MiB | +6.6% |
| | Delete Duration | 0.013 s | 0.012 s | -7.7% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.62 MiB | 15.60 MiB | +6.7% |
| | **Retained Heap vs Baseline** | **+0.15 MiB** | **+0.09 MiB** | **-0.06 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.011 s | 0.014 s | +27.3% |
| | Add Rate | 9,090.9 ops/s | 7,142.9 ops/s | -21.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.08 MiB | 15.89 MiB | +5.4% |
| | Delete Duration | 0.009 s | 0.013 s | +44.4% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.08 MiB | 15.89 MiB | +5.4% |
| | **Retained Heap vs Baseline** | **+0.24 MiB** | **+0.11 MiB** | **-0.13 MiB** |
|---|---|---|---|---|
| **1,000** | Add Duration | 0.022 s | 0.019 s | -13.6% |
| | Add Rate | 45,454.5 ops/s | 52,631.6 ops/s | +15.8% |
| | Recompute (`ovn-northd-loop`) | 3.0 ms | 3.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 1.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 18.73 MiB | 19.56 MiB | +4.4% |
| | Delete Duration | 0.010 s | 0.013 s | +30.0% |
| | Post-Delete Recompute | 4.0 ms | 2.0 ms | -50.0% |
| | Post-Delete RSS | 18.73 MiB | 19.56 MiB | +4.4% |
| | **Retained Heap vs Baseline** | **+0.10 MiB** | **+0.08 MiB** | **-0.02 MiB** |
|---|---|---|---|---|
| **10,000** | Add Duration | 1.148 s | 1.045 s | -9.0% |
| | Add Rate | 8,710.8 ops/s | 9,569.4 ops/s | +9.9% |
| | Recompute (`ovn-northd-loop`) | 49.0 ms | 36.0 ms | -26.5% |
| | Logical Flow (`lflow`) | 2.0 ms | 2.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 57.92 MiB | 57.28 MiB | -1.1% |
| | Delete Duration | 0.017 s | 0.019 s | +11.8% |
| | Post-Delete Recompute | 40.0 ms | 31.0 ms | -22.5% |
| | Post-Delete RSS | 57.92 MiB | 57.28 MiB | -1.1% |
| | **Retained Heap vs Baseline** | **+0.01 MiB** | **+0.05 MiB** | **+0.04 MiB** |
|---|---|---|---|---|

## Load Balancer (VIPs mapped to endpoints)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.012 s | 0.013 s | +8.3% |
| | Add Rate | 83.3 ops/s | 76.9 ops/s | -7.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.59 MiB | 15.62 MiB | +7.1% |
| | Delete Duration | 0.010 s | 0.014 s | +40.0% |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 14.59 MiB | 15.62 MiB | +7.1% |
| | **Retained Heap vs Baseline** | **+0.11 MiB** | **+0.12 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.013 s | 0.013 s | ~Parity (±1%) |
| | Add Rate | 769.2 ops/s | 769.2 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.82 MiB | 15.64 MiB | +5.5% |
| | Delete Duration | 0.012 s | 0.012 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.82 MiB | 15.66 MiB | +5.7% |
| | **Retained Heap vs Baseline** | **+0.26 MiB** | **+0.29 MiB** | **+0.03 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.028 s | 0.032 s | +14.3% |
| | Add Rate | 3,571.4 ops/s | 3,125.0 ops/s | -12.5% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 0.0 ms | -100.0% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.69 MiB | 16.61 MiB | +5.9% |
| | Delete Duration | 0.011 s | 0.014 s | +27.3% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.69 MiB | 16.73 MiB | +6.6% |
| | **Retained Heap vs Baseline** | **+1.15 MiB** | **+1.18 MiB** | **+0.03 MiB** |
|---|---|---|---|---|
| **1,000** | Add Duration | 0.262 s | 0.250 s | -4.6% |
| | Add Rate | 3,816.8 ops/s | 4,000.0 ops/s | +4.8% |
| | Recompute (`ovn-northd-loop`) | 8.0 ms | 6.0 ms | -25.0% |
| | Logical Flow (`lflow`) | 1.0 ms | 2.0 ms | +100.0% |
| | Peak RSS (`VmHWM`) | 24.01 MiB | 23.75 MiB | -1.1% |
| | Delete Duration | 0.092 s | 0.082 s | -10.9% |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 24.02 MiB | 24.02 MiB | ~Parity (±1%) |
| | **Retained Heap vs Baseline** | **+9.56 MiB** | **+8.44 MiB** | **-1.12 MiB** |
|---|---|---|---|---|
| **5,000** | Add Duration | 3.010 s | 2.730 s | -9.3% |
| | Add Rate | 1,661.1 ops/s | 1,831.5 ops/s | +10.3% |
| | Recompute (`ovn-northd-loop`) | 52.0 ms | 36.0 ms | -30.8% |
| | Logical Flow (`lflow`) | 10.0 ms | 7.0 ms | -30.0% |
| | Peak RSS (`VmHWM`) | 62.09 MiB | 56.03 MiB | -9.8% |
| | Delete Duration | 0.936 s | 0.809 s | -13.6% |
| | Post-Delete Recompute | 0.0 ms | 0.0 ms | N/A |
| | Post-Delete RSS | 61.50 MiB | 56.01 MiB | -8.9% |
| | **Retained Heap vs Baseline** | **+47.00 MiB** | **+40.67 MiB** | **-6.33 MiB** |
|---|---|---|---|---|

## OpenFlow Flows (OVS Datapath)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.006 s | +50.0% |
| | Add Rate | 250.0 ops/s | 166.7 ops/s | -33.3% |
| | Peak RSS (`VmHWM`) | 19.48 MiB | 21.57 MiB | +10.7% |
| | Delete Duration | 0.005 s | 0.004 s | -20.0% |
| | Post-Delete RSS | 19.49 MiB | 21.65 MiB | +11.1% |
| | **Retained Heap vs Baseline** | **+0.35 MiB** | **+0.58 MiB** | **+0.23 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.006 s | +50.0% |
| | Add Rate | 2,500.0 ops/s | 1,666.7 ops/s | -33.3% |
| | Peak RSS (`VmHWM`) | 19.40 MiB | 21.57 MiB | +11.2% |
| | Delete Duration | 0.005 s | 0.003 s | -40.0% |
| | Post-Delete RSS | 19.42 MiB | 21.66 MiB | +11.5% |
| | **Retained Heap vs Baseline** | **+0.37 MiB** | **+0.61 MiB** | **+0.24 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.006 s | 0.011 s | +83.3% |
| | Add Rate | 16,666.7 ops/s | 9,090.9 ops/s | -45.5% |
| | Peak RSS (`VmHWM`) | 19.32 MiB | 21.63 MiB | +12.0% |
| | Delete Duration | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete RSS | 19.40 MiB | 21.71 MiB | +11.9% |
| | **Retained Heap vs Baseline** | **+0.42 MiB** | **+0.67 MiB** | **+0.25 MiB** |
|---|---|---|---|---|
| **1,000** | Add Duration | 0.040 s | 0.028 s | -30.0% |
| | Add Rate | 25,000.0 ops/s | 35,714.3 ops/s | +42.9% |
| | Peak RSS (`VmHWM`) | 20.04 MiB | 22.02 MiB | +9.9% |
| | Delete Duration | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete RSS | 20.20 MiB | 22.45 MiB | +11.1% |
| | **Retained Heap vs Baseline** | **+1.14 MiB** | **+1.39 MiB** | **+0.25 MiB** |
|---|---|---|---|---|
| **10,000** | Add Duration | 0.264 s | 0.255 s | -3.4% |
| | Add Rate | 37,878.8 ops/s | 39,215.7 ops/s | +3.5% |
| | Peak RSS (`VmHWM`) | 26.76 MiB | 28.72 MiB | +7.3% |
| | Delete Duration | 0.008 s | 0.006 s | -25.0% |
| | Post-Delete RSS | 27.59 MiB | 30.91 MiB | +12.0% |
| | **Retained Heap vs Baseline** | **+8.54 MiB** | **+9.83 MiB** | **+1.29 MiB** |
|---|---|---|---|---|
| **100,000** | Add Duration | 2.460 s | 2.397 s | -2.6% |
| | Add Rate | 40,650.4 ops/s | 41,718.8 ops/s | +2.6% |
| | Peak RSS (`VmHWM`) | 93.46 MiB | 93.19 MiB | ~Parity (±1%) |
| | Delete Duration | 0.030 s | 0.026 s | -13.3% |
| | Post-Delete RSS | 99.01 MiB | 106.37 MiB | +7.4% |
| | **Retained Heap vs Baseline** | **+79.98 MiB** | **+85.10 MiB** | **+5.12 MiB** |
|---|---|---|---|---|
