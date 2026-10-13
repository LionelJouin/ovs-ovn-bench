# Isolated Object Scaling Benchmark: glibc malloc vs jemalloc

**Date:** 2026-10-13 17:51:20
**System:** Linux
**CPU:** Intel(R) Core(TM) Ultra 7 165H (22 vCPUs)
**Memory:** 62Gi
**Glibc Version:** ldd (GNU libc) 2.43
**Jemalloc Version:** 5.3.0

---

## Methodology

Each benchmark evaluates an isolated OVN/OVS object type across powers of 10 (`1, 10, 100, 1,000, 10,000, 100,000`) through 3 lifecycle phases using **native libovsdb** for database transactions:
1. **Baseline (Idle)**: Clean process RSS before creating objects.
2. **After Add (N)**: Time taken to commit $N$ objects via native OVSDB JSON-RPC, full recompute latency (`ovn-northd-loop` stopwatch), and peak RSS (`VmHWM`).
3. **After Remove (N)**: Time taken to delete all $N$ objects via native OVSDB JSON-RPC, post-deletion recompute latency, and retained heap memory (`Post-Deletion RSS - Baseline RSS`) measured after a 5-second wait allowing background page decay (`dirty_decay_ms:1000,muzzy_decay_ms:1000`).

---

## Logical Switch Port (LSP with IP, MAC, port_security)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.003 s | -25.0% |
| | Add Rate | 245 ops/s | 311 ops/s | +26.8% |
| | Recompute (`ovn-northd-loop`) | 4.4 ms | 5.9 ms | +34.3% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.58 MiB | 12.38 MiB | +17.0% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 10.58 MiB | 12.39 MiB | +17.1% |
| | **Retained Heap vs Baseline** | **+0.06 MiB** | **+0.05 MiB** | **-0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.005 s | 0.004 s | -20.0% |
| | Add Rate | 1,961 ops/s | 2,782 ops/s | +41.8% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.71 MiB | 12.70 MiB | +18.6% |
| | Delete Duration | 0.003 s | 0.004 s | +33.3% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 10.72 MiB | 12.71 MiB | +18.6% |
| | **Retained Heap vs Baseline** | **+0.16 MiB** | **+0.30 MiB** | **+0.14 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.023 s | 0.017 s | -26.1% |
| | Add Rate | 4,413 ops/s | 5,899 ops/s | +33.7% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 1.0 ms | -50.0% |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 13.45 MiB | 15.21 MiB | +13.1% |
| | Delete Duration | 0.004 s | 0.005 s | +25.0% |
| | Post-Delete Recompute | 2.0 ms | 2.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 13.45 MiB | 15.30 MiB | +13.8% |
| | **Retained Heap vs Baseline** | **+2.84 MiB** | **+2.84 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.227 s | 0.169 s | -25.6% |
| | Add Rate | 4,415 ops/s | 5,905 ops/s | +33.8% |
| | Recompute (`ovn-northd-loop`) | 14.0 ms | 8.0 ms | -42.9% |
| | Logical Flow (`lflow`) | 5.0 ms | 4.0 ms | -20.0% |
| | Peak RSS (`VmHWM`) | 36.86 MiB | 36.11 MiB | -2.0% |
| | Delete Duration | 0.027 s | 0.025 s | -7.4% |
| | Post-Delete Recompute | 17.0 ms | 14.0 ms | -17.6% |
| | Post-Delete RSS | 36.88 MiB | 36.23 MiB | -1.8% |
| | **Retained Heap vs Baseline** | **+26.25 MiB** | **+23.77 MiB** | **-2.48 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 3.501 s | 2.277 s | -35.0% |
| | Add Rate | 2,856 ops/s | 4,391 ops/s | +53.7% |
| | Recompute (`ovn-northd-loop`) | 253.0 ms | 133.0 ms | -47.4% |
| | Logical Flow (`lflow`) | 138.0 ms | 76.0 ms | -44.9% |
| | Peak RSS (`VmHWM`) | 273.27 MiB | 234.18 MiB | -14.3% |
| | Delete Duration | 3.356 s | 2.903 s | -13.5% |
| | Post-Delete Recompute | 162.0 ms | 100.0 ms | -38.3% |
| | Post-Delete RSS | 273.27 MiB | 208.23 MiB | -23.8% |
| | **Retained Heap vs Baseline** | **+262.67 MiB** | **+195.90 MiB** | **-66.77 MiB** |
|---|---|---|---|---|

## ACL (Access Control List with stateful allow-related)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 230 ops/s | 255 ops/s | +10.5% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 10.66 MiB | 12.56 MiB | +17.8% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 4.8 ms | 5.6 ms | +16.3% |
| | Post-Delete RSS | 10.66 MiB | 12.57 MiB | +17.9% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.11 MiB** | **+0.02 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 2,274 ops/s | 2,501 ops/s | +10.0% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.77 MiB | 12.50 MiB | +16.1% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 5.2 ms | 1.0 ms | -80.7% |
| | Post-Delete RSS | 10.77 MiB | 12.50 MiB | +16.1% |
| | **Retained Heap vs Baseline** | **+0.21 MiB** | **+0.22 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.014 s | 0.010 s | -28.6% |
| | Add Rate | 7,277 ops/s | 10,452 ops/s | +43.6% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 11.78 MiB | 13.66 MiB | +16.0% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 11.78 MiB | 13.67 MiB | +16.0% |
| | **Retained Heap vs Baseline** | **+1.26 MiB** | **+1.34 MiB** | **+0.08 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.080 s | 0.073 s | -8.8% |
| | Add Rate | 12,446 ops/s | 13,621 ops/s | +9.4% |
| | Recompute (`ovn-northd-loop`) | 5.0 ms | 3.0 ms | -40.0% |
| | Logical Flow (`lflow`) | 4.0 ms | 3.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 20.63 MiB | 21.39 MiB | +3.7% |
| | Delete Duration | 0.006 s | 0.005 s | -16.7% |
| | Post-Delete Recompute | 5.0 ms | 4.0 ms | -20.0% |
| | Post-Delete RSS | 20.63 MiB | 21.42 MiB | +3.8% |
| | **Retained Heap vs Baseline** | **+10.04 MiB** | **+9.00 MiB** | **-1.04 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 1.213 s | 0.738 s | -39.2% |
| | Add Rate | 8,242 ops/s | 13,552 ops/s | +64.4% |
| | Recompute (`ovn-northd-loop`) | 34.0 ms | 29.0 ms | -14.7% |
| | Logical Flow (`lflow`) | 30.0 ms | 26.0 ms | -13.3% |
| | Peak RSS (`VmHWM`) | 101.53 MiB | 99.22 MiB | -2.3% |
| | Delete Duration | 0.016 s | 0.012 s | -25.0% |
| | Post-Delete Recompute | 35.0 ms | 31.0 ms | -11.4% |
| | Post-Delete RSS | 98.97 MiB | 93.63 MiB | -5.4% |
| | **Retained Heap vs Baseline** | **+88.35 MiB** | **+81.15 MiB** | **-7.20 MiB** |
|---|---|---|---|---|

## Address Set (IP list matching ACL)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 280 ops/s | 272 ops/s | -2.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.68 MiB | 12.47 MiB | +16.8% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 4.7 ms | 1.0 ms | -78.9% |
| | Post-Delete RSS | 10.68 MiB | 12.48 MiB | +16.9% |
| | **Retained Heap vs Baseline** | **+0.10 MiB** | **+0.10 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 2,570 ops/s | 2,600 ops/s | +1.1% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 5.3 ms | +434.5% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.65 MiB | 12.42 MiB | +16.6% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 6.0 ms | 5.3 ms | -11.9% |
| | Post-Delete RSS | 10.65 MiB | 12.43 MiB | +16.7% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.12 MiB** | **+0.03 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.003 s | 0.003 s | ~Parity (±1%) |
| | Add Rate | 28,835 ops/s | 31,065 ops/s | +7.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.68 MiB | 12.39 MiB | +16.0% |
| | Delete Duration | 0.003 s | 0.004 s | +33.3% |
| | Post-Delete Recompute | 6.1 ms | 1.0 ms | -83.6% |
| | Post-Delete RSS | 10.68 MiB | 12.40 MiB | +16.1% |
| | **Retained Heap vs Baseline** | **+0.11 MiB** | **+0.10 MiB** | **-0.01 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.007 s | 0.005 s | -28.6% |
| | Add Rate | 145,751 ops/s | 191,058 ops/s | +31.1% |
| | Recompute (`ovn-northd-loop`) | 5.3 ms | 1.0 ms | -81.0% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.87 MiB | 12.78 MiB | +17.6% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 5.4 ms | +437.9% |
| | Post-Delete RSS | 10.87 MiB | 12.79 MiB | +17.7% |
| | **Retained Heap vs Baseline** | **+0.32 MiB** | **+0.35 MiB** | **+0.03 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 0.025 s | 0.020 s | -20.0% |
| | Add Rate | 395,961 ops/s | 489,787 ops/s | +23.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 12.64 MiB | 14.55 MiB | +15.1% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 12.48 MiB | 14.55 MiB | +16.6% |
| | **Retained Heap vs Baseline** | **+1.97 MiB** | **+2.14 MiB** | **+0.17 MiB** |
|---|---|---|---|---|

## Port Group (Ports member grouping matching ACL)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.003 s | 0.003 s | ~Parity (±1%) |
| | Add Rate | 295 ops/s | 302 ops/s | +2.5% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 10.57 MiB | 12.50 MiB | +18.3% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 10.57 MiB | 12.50 MiB | +18.3% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.003 s | -25.0% |
| | Add Rate | 2,574 ops/s | 3,034 ops/s | +17.8% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.57 MiB | 12.41 MiB | +17.4% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 4.7 ms | 1.0 ms | -78.8% |
| | Post-Delete RSS | 10.57 MiB | 12.41 MiB | +17.4% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 24,624 ops/s | 26,232 ops/s | +6.5% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 1.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.94 MiB | 12.94 MiB | +18.3% |
| | Delete Duration | 0.005 s | 0.004 s | -20.0% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 10.94 MiB | 12.94 MiB | +18.3% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.007 s | 0.007 s | ~Parity (±1%) |
| | Add Rate | 150,647 ops/s | 150,172 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 3.0 ms | 5.0 ms | +66.7% |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 15.69 MiB | 16.31 MiB | +4.0% |
| | Delete Duration | 0.028 s | 0.024 s | -14.3% |
| | Post-Delete Recompute | 4.0 ms | 3.0 ms | -25.0% |
| | Post-Delete RSS | 15.69 MiB | 16.31 MiB | +4.0% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.00 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 0.021 s | 0.018 s | -14.3% |
| | Add Rate | 483,325 ops/s | 565,674 ops/s | +17.0% |
| | Recompute (`ovn-northd-loop`) | 46.0 ms | 34.0 ms | -26.1% |
| | Logical Flow (`lflow`) | 8.0 ms | 6.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 54.99 MiB | 55.47 MiB | ~Parity (±1%) |
| | Delete Duration | 3.571 s | 3.087 s | -13.6% |
| | Post-Delete Recompute | 49.0 ms | 37.0 ms | -24.5% |
| | Post-Delete RSS | 54.99 MiB | 54.36 MiB | -1.1% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **-1.11 MiB** | **-1.11 MiB** |
|---|---|---|---|---|

## Load Balancer (VIPs mapped to endpoints)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 240 ops/s | 240 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.71 MiB | 12.51 MiB | +16.8% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 1.0 ms | 5.6 ms | +463.4% |
| | Post-Delete RSS | 10.71 MiB | 12.54 MiB | +17.1% |
| | **Retained Heap vs Baseline** | **+0.12 MiB** | **+0.13 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.005 s | 0.004 s | -20.0% |
| | Add Rate | 1,931 ops/s | 2,486 ops/s | +28.7% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 11.03 MiB | 12.85 MiB | +16.5% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 11.03 MiB | 12.87 MiB | +16.7% |
| | **Retained Heap vs Baseline** | **+0.31 MiB** | **+0.41 MiB** | **+0.10 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.015 s | 0.012 s | -20.0% |
| | Add Rate | 6,874 ops/s | 8,598 ops/s | +25.1% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 2.0 ms | +100.0% |
| | Logical Flow (`lflow`) | 0.0 ms | 1.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 12.63 MiB | 14.33 MiB | +13.5% |
| | Delete Duration | 0.003 s | 0.004 s | +33.3% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 12.64 MiB | 14.40 MiB | +13.9% |
| | **Retained Heap vs Baseline** | **+2.03 MiB** | **+1.93 MiB** | **-0.10 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.118 s | 0.091 s | -22.9% |
| | Add Rate | 8,491 ops/s | 10,946 ops/s | +28.9% |
| | Recompute (`ovn-northd-loop`) | 9.0 ms | 6.0 ms | -33.3% |
| | Logical Flow (`lflow`) | 3.0 ms | 2.0 ms | -33.3% |
| | Peak RSS (`VmHWM`) | 27.51 MiB | 27.07 MiB | -1.6% |
| | Delete Duration | 0.005 s | 0.004 s | -20.0% |
| | Post-Delete Recompute | 10.0 ms | 8.0 ms | -20.0% |
| | Post-Delete RSS | 27.52 MiB | 27.17 MiB | -1.3% |
| | **Retained Heap vs Baseline** | **+16.79 MiB** | **+14.72 MiB** | **-2.07 MiB** |
|---|---|---|---|---|
| **5000** | Add Duration | 3.010 s | 2.730 s | -9.3% |
| | Add Rate | 1,661 ops/s | 1,831 ops/s | +10.3% |
| | Recompute (`ovn-northd-loop`) | 52.0 ms | 36.0 ms | -30.8% |
| | Logical Flow (`lflow`) | 10.0 ms | 7.0 ms | -30.0% |
| | Peak RSS (`VmHWM`) | 62.09 MiB | 56.03 MiB | -9.8% |
| | Delete Duration | 0.936 s | 0.809 s | -13.6% |
| | Post-Delete Recompute | 0.0 ms | 0.0 ms | N/A |
| | Post-Delete RSS | 61.50 MiB | 56.01 MiB | -8.9% |
| | **Retained Heap vs Baseline** | **+47.00 MiB** | **+40.67 MiB** | **-6.33 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 2.086 s | 1.178 s | -43.5% |
| | Add Rate | 4,793 ops/s | 8,488 ops/s | +77.1% |
| | Recompute (`ovn-northd-loop`) | 181.0 ms | 88.0 ms | -51.4% |
| | Logical Flow (`lflow`) | 55.0 ms | 25.0 ms | -54.5% |
| | Peak RSS (`VmHWM`) | 182.63 MiB | 132.97 MiB | -27.2% |
| | Delete Duration | 0.017 s | 0.014 s | -17.6% |
| | Post-Delete Recompute | 177.0 ms | 82.0 ms | -53.7% |
| | Post-Delete RSS | 182.70 MiB | 133.20 MiB | -27.1% |
| | **Retained Heap vs Baseline** | **+172.13 MiB** | **+120.86 MiB** | **-51.27 MiB** |
|---|---|---|---|---|

## OpenFlow Flows (OVS Control Plane)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.003 s | 0.005 s | +66.7% |
| | Add Rate | 291 ops/s | 212 ops/s | -27.3% |
| | Peak RSS (`VmHWM`) | 21.61 MiB | 26.19 MiB | +21.2% |
| | Delete Duration | 0.002 s | 0.004 s | +100.0% |
| | Post-Delete RSS | 21.62 MiB | 26.32 MiB | +21.7% |
| | **Retained Heap vs Baseline** | **+0.25 MiB** | **+0.46 MiB** | **+0.21 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.006 s | +50.0% |
| | Add Rate | 2,448 ops/s | 1,603 ops/s | -34.5% |
| | Peak RSS (`VmHWM`) | 21.48 MiB | 26.28 MiB | +22.3% |
| | Delete Duration | 0.003 s | 0.004 s | +33.3% |
| | Post-Delete RSS | 21.52 MiB | 26.30 MiB | +22.2% |
| | **Retained Heap vs Baseline** | **+0.28 MiB** | **+0.42 MiB** | **+0.14 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.006 s | 0.008 s | +33.3% |
| | Add Rate | 17,256 ops/s | 13,151 ops/s | -23.8% |
| | Peak RSS (`VmHWM`) | 21.53 MiB | 26.26 MiB | +22.0% |
| | Delete Duration | 0.002 s | 0.003 s | +50.0% |
| | Post-Delete RSS | 21.63 MiB | 26.39 MiB | +22.0% |
| | **Retained Heap vs Baseline** | **+0.34 MiB** | **+0.60 MiB** | **+0.26 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.033 s | 0.041 s | +24.2% |
| | Add Rate | 30,408 ops/s | 24,429 ops/s | -19.7% |
| | Peak RSS (`VmHWM`) | 22.13 MiB | 26.75 MiB | +20.9% |
| | Delete Duration | 0.003 s | 0.006 s | +100.0% |
| | Post-Delete RSS | 22.36 MiB | 27.15 MiB | +21.4% |
| | **Retained Heap vs Baseline** | **+1.11 MiB** | **+1.30 MiB** | **+0.19 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 0.330 s | 0.292 s | -11.5% |
| | Add Rate | 30,269 ops/s | 34,244 ops/s | +13.1% |
| | Peak RSS (`VmHWM`) | 28.80 MiB | 33.37 MiB | +15.9% |
| | Delete Duration | 0.006 s | 0.007 s | +16.7% |
| | Post-Delete RSS | 29.64 MiB | 35.57 MiB | +20.0% |
| | **Retained Heap vs Baseline** | **+8.34 MiB** | **+9.74 MiB** | **+1.40 MiB** |
|---|---|---|---|---|
| **100000** | Add Duration | 3.111 s | 2.936 s | -5.6% |
| | Add Rate | 32,142 ops/s | 34,060 ops/s | +6.0% |
| | Peak RSS (`VmHWM`) | 95.25 MiB | 97.48 MiB | +2.3% |
| | Delete Duration | 0.033 s | 0.027 s | -18.2% |
| | Post-Delete RSS | 100.82 MiB | 110.54 MiB | +9.6% |
| | **Retained Heap vs Baseline** | **+79.46 MiB** | **+84.76 MiB** | **+5.30 MiB** |
|---|---|---|---|---|

## OVS Datapath Packet Forwarding & Megaflow Cache

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **100** | Forwarding Duration | 0.005 s | 0.006 s | +20.0% |
| | Forwarding Rate | 19,527 pkts/s | 15,878 pkts/s | -18.7% |
| | Peak RSS (`VmHWM`) | 19.61 MiB | 21.91 MiB | +11.7% |
| | Flow Flush Duration (`dpctl/del-flows`) | 0.005 s | 0.004 s | -20.0% |
| | Post-Delete RSS | 19.61 MiB | 21.91 MiB | +11.7% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.13 MiB** | **+0.04 MiB** |
|---|---|---|---|---|
| **1000** | Forwarding Duration | 0.011 s | 0.012 s | +9.1% |
| | Forwarding Rate | 90,399 pkts/s | 83,132 pkts/s | -8.0% |
| | Peak RSS (`VmHWM`) | 19.56 MiB | 21.96 MiB | +12.3% |
| | Flow Flush Duration (`dpctl/del-flows`) | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete RSS | 19.56 MiB | 21.97 MiB | +12.3% |
| | **Retained Heap vs Baseline** | **+0.15 MiB** | **+0.23 MiB** | **+0.08 MiB** |
|---|---|---|---|---|
| **100000** | Forwarding Duration | 0.943 s | 1.098 s | +16.4% |
| | Forwarding Rate | 106,060 pkts/s | 91,111 pkts/s | -14.1% |
| | Peak RSS (`VmHWM`) | 19.61 MiB | 21.87 MiB | +11.5% |
| | Flow Flush Duration (`dpctl/del-flows`) | 0.003 s | 0.006 s | +100.0% |
| | Post-Delete RSS | 19.61 MiB | 21.93 MiB | +11.8% |
| | **Retained Heap vs Baseline** | **+0.18 MiB** | **+0.03 MiB** | **-0.15 MiB** |
|---|---|---|---|---|

## Combined Workload (All objects scaled proportionally)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.007 s | 0.007 s | ~Parity (±1%) |
| | Add Rate | 141 ops/s | 152 ops/s | +7.9% |
| | Recompute (`ovn-northd-loop`) | 5.9 ms | 5.3 ms | -10.2% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.99 MiB | 13.05 MiB | +18.7% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 5.8 ms | 1.0 ms | -82.7% |
| | Post-Delete RSS | 10.99 MiB | 13.05 MiB | +18.7% |
| | **Retained Heap vs Baseline** | **+0.73 MiB** | **+0.89 MiB** | **+0.16 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.012 s | 0.009 s | -25.0% |
| | Add Rate | 824 ops/s | 1,103 ops/s | +33.8% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 11.40 MiB | 13.66 MiB | +19.8% |
| | Delete Duration | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete Recompute | 4.9 ms | 5.6 ms | +14.8% |
| | Post-Delete RSS | 11.40 MiB | 13.70 MiB | +20.2% |
| | **Retained Heap vs Baseline** | **+1.18 MiB** | **+1.59 MiB** | **+0.41 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.039 s | 0.026 s | -33.3% |
| | Add Rate | 2,548 ops/s | 3,867 ops/s | +51.7% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 1.0 ms | -50.0% |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 13.80 MiB | 15.49 MiB | +12.2% |
| | Delete Duration | 0.021 s | 0.011 s | -47.6% |
| | Post-Delete Recompute | 1.0 ms | 6.3 ms | +526.8% |
| | Post-Delete RSS | 14.01 MiB | 15.70 MiB | +12.1% |
| | **Retained Heap vs Baseline** | **+3.70 MiB** | **+3.74 MiB** | **+0.04 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.373 s | 0.249 s | -33.2% |
| | Add Rate | 2,678 ops/s | 4,019 ops/s | +50.1% |
| | Recompute (`ovn-northd-loop`) | 16.0 ms | 14.0 ms | -12.5% |
| | Logical Flow (`lflow`) | 8.0 ms | 6.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 48.16 MiB | 45.57 MiB | -5.4% |
| | Delete Duration | 0.217 s | 0.093 s | -57.1% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 47.18 MiB | 45.57 MiB | -3.4% |
| | **Retained Heap vs Baseline** | **+36.99 MiB** | **+33.44 MiB** | **-3.55 MiB** |
|---|---|---|---|---|
| **5000** | Add Duration | 16.420 s | 14.032 s | -14.5% |
| | Add Rate | 304 ops/s | 356 ops/s | +17.0% |
| | Recompute (`ovn-northd-loop`) | 85.0 ms | 68.0 ms | -20.0% |
| | Logical Flow (`lflow`) | 24.0 ms | 15.0 ms | -37.5% |
| | Peak RSS (`VmHWM`) | 141.71 MiB | 124.41 MiB | -12.2% |
| | Delete Duration | 0.927 s | 0.491 s | -47.0% |
| | Post-Delete Recompute | 0.0 ms | 1.0 ms | N/A |
| | Post-Delete RSS | 146.10 MiB | 133.06 MiB | -8.9% |
| | **Retained Heap vs Baseline** | **+132.02 MiB** | **+117.97 MiB** | **-14.05 MiB** |
|---|---|---|---|---|
