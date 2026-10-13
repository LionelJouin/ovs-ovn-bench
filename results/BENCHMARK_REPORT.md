# Isolated Object Scaling Benchmark: glibc malloc vs jemalloc

**Date:** 2026-10-13 16:14:52
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
3. **After Remove (N)**: Time taken to delete all $N$ objects via native OVSDB JSON-RPC, post-deletion recompute latency, and retained heap memory (`Post-Deletion RSS - Baseline RSS`).

---

## Logical Switch Port (LSP with IP, MAC, port_security)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.006 s | 0.007 s | +16.7% |
| | Add Rate | 159 ops/s | 136 ops/s | -14.0% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.57 MiB | 15.57 MiB | +6.9% |
| | Delete Duration | 0.006 s | 0.009 s | +50.0% |
| | Post-Delete Recompute | 12.8 ms | 1.0 ms | -92.2% |
| | Post-Delete RSS | 14.57 MiB | 15.57 MiB | +6.9% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.00 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.008 s | 0.006 s | -25.0% |
| | Add Rate | 1,324 ops/s | 1,699 ops/s | +28.3% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.44 MiB | 15.57 MiB | +7.8% |
| | Delete Duration | 0.006 s | 0.006 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.44 MiB | 15.57 MiB | +7.8% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.00 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.027 s | 0.027 s | ~Parity (±1%) |
| | Add Rate | 3,703 ops/s | 3,703 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 1.0 ms | -50.0% |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 16.56 MiB | 17.49 MiB | +5.6% |
| | Delete Duration | 0.015 s | 0.014 s | -6.7% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 16.56 MiB | 17.54 MiB | +5.9% |
| | **Retained Heap vs Baseline** | **+2.08 MiB** | **+2.12 MiB** | **+0.04 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.456 s | 0.447 s | -2.0% |
| | Add Rate | 2,193 ops/s | 2,237 ops/s | +2.0% |
| | Recompute (`ovn-northd-loop`) | 10.0 ms | 9.0 ms | -10.0% |
| | Logical Flow (`lflow`) | 2.0 ms | 2.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 31.48 MiB | 30.59 MiB | -2.8% |
| | Delete Duration | 0.088 s | 0.062 s | -29.5% |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 31.49 MiB | 30.86 MiB | -2.0% |
| | **Retained Heap vs Baseline** | **+17.02 MiB** | **+15.31 MiB** | **-1.71 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 33.477 s | 32.202 s | -3.8% |
| | Add Rate | 298 ops/s | 310 ops/s | +4.0% |
| | Recompute (`ovn-northd-loop`) | 149.0 ms | 95.0 ms | -36.2% |
| | Logical Flow (`lflow`) | 25.0 ms | 15.0 ms | -40.0% |
| | Peak RSS (`VmHWM`) | 180.27 MiB | 162.32 MiB | -10.0% |
| | Delete Duration | 2.448 s | 2.012 s | -17.8% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 180.28 MiB | 140.89 MiB | -21.8% |
| | **Retained Heap vs Baseline** | **+165.81 MiB** | **+125.47 MiB** | **-40.34 MiB** |
|---|---|---|---|---|

## ACL (Access Control List with stateful allow-related)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.003 s | -25.0% |
| | Add Rate | 284 ops/s | 288 ops/s | +1.5% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 10.57 MiB | 12.36 MiB | +16.9% |
| | Delete Duration | 0.004 s | 0.003 s | -25.0% |
| | Post-Delete Recompute | 4.8 ms | 1.0 ms | -79.4% |
| | Post-Delete RSS | 10.57 MiB | 12.36 MiB | +16.9% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.003 s | 0.004 s | +33.3% |
| | Add Rate | 3,473 ops/s | 2,556 ops/s | -26.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 10.47 MiB | 12.39 MiB | +18.3% |
| | Delete Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Post-Delete Recompute | 5.4 ms | 1.0 ms | -81.6% |
| | Post-Delete RSS | 10.47 MiB | 12.39 MiB | +18.3% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.037 s | 0.032 s | -13.5% |
| | Add Rate | 2,702 ops/s | 3,125 ops/s | +15.6% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 15.19 MiB | 16.27 MiB | +7.1% |
| | Delete Duration | 0.012 s | 0.014 s | +16.7% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.25 MiB | 16.39 MiB | +7.5% |
| | **Retained Heap vs Baseline** | **+0.77 MiB** | **+0.82 MiB** | **+0.05 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.290 s | 0.315 s | +8.6% |
| | Add Rate | 3,448 ops/s | 3,174 ops/s | -7.9% |
| | Recompute (`ovn-northd-loop`) | 5.0 ms | 4.0 ms | -20.0% |
| | Logical Flow (`lflow`) | 2.0 ms | 1.0 ms | -50.0% |
| | Peak RSS (`VmHWM`) | 21.33 MiB | 21.55 MiB | +1.0% |
| | Delete Duration | 0.044 s | 0.035 s | -20.5% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 21.42 MiB | 22.87 MiB | +6.8% |
| | **Retained Heap vs Baseline** | **+6.94 MiB** | **+7.29 MiB** | **+0.35 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 12.057 s | 10.320 s | -14.4% |
| | Add Rate | 829 ops/s | 969 ops/s | +16.8% |
| | Recompute (`ovn-northd-loop`) | 88.0 ms | 68.0 ms | -22.7% |
| | Logical Flow (`lflow`) | 60.0 ms | 45.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 82.80 MiB | 74.42 MiB | -10.1% |
| | Delete Duration | 0.524 s | 0.370 s | -29.4% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 82.58 MiB | 81.47 MiB | -1.3% |
| | **Retained Heap vs Baseline** | **+68.03 MiB** | **+66.09 MiB** | **-1.94 MiB** |
|---|---|---|---|---|

## Address Set (IP list matching ACL)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.008 s | 0.006 s | -25.0% |
| | Add Rate | 132 ops/s | 155 ops/s | +17.3% |
| | Recompute (`ovn-northd-loop`) | 10.6 ms | 10.5 ms | -1.1% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.47 MiB | 15.55 MiB | +7.5% |
| | Delete Duration | 0.007 s | 0.005 s | -28.6% |
| | Post-Delete Recompute | 1.0 ms | 8.3 ms | +728.5% |
| | Post-Delete RSS | 14.47 MiB | 15.55 MiB | +7.5% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.01 MiB** | **+0.01 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.006 s | 0.007 s | +16.7% |
| | Add Rate | 1,737 ops/s | 1,368 ops/s | -21.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.48 MiB | 15.52 MiB | +7.2% |
| | Delete Duration | 0.005 s | 0.006 s | +20.0% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.48 MiB | 15.52 MiB | +7.2% |
| | **Retained Heap vs Baseline** | **+0.00 MiB** | **+0.00 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.010 s | 0.010 s | ~Parity (±1%) |
| | Add Rate | 10,000 ops/s | 10,000 ops/s | ~Parity (±1%) |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.64 MiB | 15.49 MiB | +5.8% |
| | Delete Duration | 0.011 s | 0.013 s | +18.2% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.64 MiB | 15.49 MiB | +5.8% |
| | **Retained Heap vs Baseline** | **+0.16 MiB** | **+0.08 MiB** | **-0.08 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.015 s | 0.014 s | -6.7% |
| | Add Rate | 66,666 ops/s | 71,428 ops/s | +7.1% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 14.75 MiB | 15.84 MiB | +7.4% |
| | Delete Duration | 0.011 s | 0.011 s | ~Parity (±1%) |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 14.75 MiB | 15.84 MiB | +7.4% |
| | **Retained Heap vs Baseline** | **+0.24 MiB** | **+0.28 MiB** | **+0.04 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 0.028 s | 0.030 s | +7.1% |
| | Add Rate | 357,142 ops/s | 333,333 ops/s | -6.7% |
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
| | Add Rate | 90 ops/s | 76 ops/s | -15.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 0.0 ms | -100.0% |
| | Peak RSS (`VmHWM`) | 14.55 MiB | 15.49 MiB | +6.5% |
| | Delete Duration | 0.011 s | 0.010 s | -9.1% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.55 MiB | 15.49 MiB | +6.5% |
| | **Retained Heap vs Baseline** | **+0.09 MiB** | **+0.11 MiB** | **+0.02 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.012 s | 0.014 s | +16.7% |
| | Add Rate | 833 ops/s | 714 ops/s | -14.3% |
| | Recompute (`ovn-northd-loop`) | 0.0 ms | 1.0 ms | N/A |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.62 MiB | 15.59 MiB | +6.6% |
| | Delete Duration | 0.013 s | 0.012 s | -7.7% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.62 MiB | 15.60 MiB | +6.7% |
| | **Retained Heap vs Baseline** | **+0.15 MiB** | **+0.09 MiB** | **-0.06 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.011 s | 0.014 s | +27.3% |
| | Add Rate | 9,090 ops/s | 7,142 ops/s | -21.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.08 MiB | 15.89 MiB | +5.4% |
| | Delete Duration | 0.009 s | 0.013 s | +44.4% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.08 MiB | 15.89 MiB | +5.4% |
| | **Retained Heap vs Baseline** | **+0.24 MiB** | **+0.11 MiB** | **-0.13 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.022 s | 0.019 s | -13.6% |
| | Add Rate | 45,454 ops/s | 52,631 ops/s | +15.8% |
| | Recompute (`ovn-northd-loop`) | 3.0 ms | 3.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 1.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 18.73 MiB | 19.56 MiB | +4.4% |
| | Delete Duration | 0.010 s | 0.013 s | +30.0% |
| | Post-Delete Recompute | 4.0 ms | 2.0 ms | -50.0% |
| | Post-Delete RSS | 18.73 MiB | 19.56 MiB | +4.4% |
| | **Retained Heap vs Baseline** | **+0.10 MiB** | **+0.08 MiB** | **-0.02 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 1.148 s | 1.045 s | -9.0% |
| | Add Rate | 8,710 ops/s | 9,569 ops/s | +9.9% |
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
| **1** | Add Duration | 0.005 s | 0.007 s | +40.0% |
| | Add Rate | 192 ops/s | 151 ops/s | -21.4% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.50 MiB | 15.59 MiB | +7.5% |
| | Delete Duration | 0.008 s | 0.007 s | -12.5% |
| | Post-Delete Recompute | 9.6 ms | 1.0 ms | -89.6% |
| | Post-Delete RSS | 14.51 MiB | 15.59 MiB | +7.4% |
| | **Retained Heap vs Baseline** | **+0.07 MiB** | **+0.07 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.007 s | 0.005 s | -28.6% |
| | Add Rate | 1,358 ops/s | 1,931 ops/s | +42.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 14.55 MiB | 15.60 MiB | +7.2% |
| | Delete Duration | 0.005 s | 0.007 s | +40.0% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 14.55 MiB | 15.60 MiB | +7.2% |
| | **Retained Heap vs Baseline** | **+0.07 MiB** | **+0.07 MiB** | **+0.00 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.028 s | 0.032 s | +14.3% |
| | Add Rate | 3,571 ops/s | 3,125 ops/s | -12.5% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 0.0 ms | -100.0% |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.69 MiB | 16.61 MiB | +5.9% |
| | Delete Duration | 0.011 s | 0.014 s | +27.3% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.69 MiB | 16.73 MiB | +6.6% |
| | **Retained Heap vs Baseline** | **+1.15 MiB** | **+1.18 MiB** | **+0.03 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.262 s | 0.250 s | -4.6% |
| | Add Rate | 3,816 ops/s | 4,000 ops/s | +4.8% |
| | Recompute (`ovn-northd-loop`) | 8.0 ms | 6.0 ms | -25.0% |
| | Logical Flow (`lflow`) | 1.0 ms | 2.0 ms | +100.0% |
| | Peak RSS (`VmHWM`) | 24.01 MiB | 23.75 MiB | -1.1% |
| | Delete Duration | 0.092 s | 0.082 s | -10.9% |
| | Post-Delete Recompute | 1.0 ms | 0.0 ms | -100.0% |
| | Post-Delete RSS | 24.02 MiB | 24.02 MiB | ~Parity (±1%) |
| | **Retained Heap vs Baseline** | **+9.56 MiB** | **+8.44 MiB** | **-1.12 MiB** |
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

## OpenFlow Flows (OVS Datapath)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.004 s | 0.004 s | ~Parity (±1%) |
| | Add Rate | 250 ops/s | 250 ops/s | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 21.47 MiB | 26.35 MiB | +22.7% |
| | Delete Duration | 0.003 s | 0.005 s | +66.7% |
| | Post-Delete RSS | 21.48 MiB | 26.37 MiB | +22.8% |
| | **Retained Heap vs Baseline** | **+0.19 MiB** | **+0.35 MiB** | **+0.16 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.004 s | 0.006 s | +50.0% |
| | Add Rate | 2,500 ops/s | 1,666 ops/s | -33.3% |
| | Peak RSS (`VmHWM`) | 21.59 MiB | 26.17 MiB | +21.2% |
| | Delete Duration | 0.003 s | 0.004 s | +33.3% |
| | Post-Delete RSS | 21.62 MiB | 26.24 MiB | +21.4% |
| | **Retained Heap vs Baseline** | **+0.20 MiB** | **+0.47 MiB** | **+0.27 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.006 s | 0.011 s | +83.3% |
| | Add Rate | 16,666 ops/s | 9,090 ops/s | -45.5% |
| | Peak RSS (`VmHWM`) | 19.32 MiB | 21.63 MiB | +12.0% |
| | Delete Duration | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete RSS | 19.40 MiB | 21.71 MiB | +11.9% |
| | **Retained Heap vs Baseline** | **+0.42 MiB** | **+0.67 MiB** | **+0.25 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 0.040 s | 0.028 s | -30.0% |
| | Add Rate | 25,000 ops/s | 35,714 ops/s | +42.9% |
| | Peak RSS (`VmHWM`) | 20.04 MiB | 22.02 MiB | +9.9% |
| | Delete Duration | 0.005 s | 0.005 s | ~Parity (±1%) |
| | Post-Delete RSS | 20.20 MiB | 22.45 MiB | +11.1% |
| | **Retained Heap vs Baseline** | **+1.14 MiB** | **+1.39 MiB** | **+0.25 MiB** |
|---|---|---|---|---|
| **10000** | Add Duration | 0.264 s | 0.255 s | -3.4% |
| | Add Rate | 37,878 ops/s | 39,215 ops/s | +3.5% |
| | Peak RSS (`VmHWM`) | 26.76 MiB | 28.72 MiB | +7.3% |
| | Delete Duration | 0.008 s | 0.006 s | -25.0% |
| | Post-Delete RSS | 27.59 MiB | 30.91 MiB | +12.0% |
| | **Retained Heap vs Baseline** | **+8.54 MiB** | **+9.83 MiB** | **+1.29 MiB** |
|---|---|---|---|---|
| **100000** | Add Duration | 3.191 s | 2.990 s | -6.3% |
| | Add Rate | 31,338 ops/s | 33,444 ops/s | +6.7% |
| | Peak RSS (`VmHWM`) | 95.19 MiB | 97.55 MiB | +2.5% |
| | Delete Duration | 0.030 s | 0.028 s | -6.7% |
| | Post-Delete RSS | 100.75 MiB | 106.79 MiB | +6.0% |
| | **Retained Heap vs Baseline** | **+79.45 MiB** | **+81.04 MiB** | **+1.59 MiB** |
|---|---|---|---|---|

## OVS Classifier Multi-Threaded Lookups

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
|---|---|---|---|---|
|---|---|---|---|---|

## Combined Workload (All objects scaled proportionally)

| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|
| **1** | Add Duration | 0.036 s | 0.031 s | -13.9% |
| | Add Rate | 27 ops/s | 32 ops/s | +16.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.05 MiB | 16.30 MiB | +8.3% |
| | Delete Duration | 0.028 s | 0.022 s | -21.4% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.05 MiB | 16.36 MiB | +8.7% |
| | **Retained Heap vs Baseline** | **+0.93 MiB** | **+1.17 MiB** | **+0.24 MiB** |
|---|---|---|---|---|
| **10** | Add Duration | 0.032 s | 0.031 s | -3.1% |
| | Add Rate | 312 ops/s | 322 ops/s | +3.2% |
| | Recompute (`ovn-northd-loop`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 0.0 ms | 0.0 ms | N/A |
| | Peak RSS (`VmHWM`) | 15.22 MiB | 16.41 MiB | +7.8% |
| | Delete Duration | 0.022 s | 0.023 s | +4.5% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 15.23 MiB | 16.45 MiB | +8.0% |
| | **Retained Heap vs Baseline** | **+1.09 MiB** | **+1.40 MiB** | **+0.31 MiB** |
|---|---|---|---|---|
| **100** | Add Duration | 0.103 s | 0.095 s | -7.8% |
| | Add Rate | 970 ops/s | 1,052 ops/s | +8.4% |
| | Recompute (`ovn-northd-loop`) | 2.0 ms | 2.0 ms | ~Parity (±1%) |
| | Logical Flow (`lflow`) | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Peak RSS (`VmHWM`) | 17.49 MiB | 18.07 MiB | +3.3% |
| | Delete Duration | 0.021 s | 0.022 s | +4.8% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 17.57 MiB | 18.60 MiB | +5.9% |
| | **Retained Heap vs Baseline** | **+3.45 MiB** | **+3.55 MiB** | **+0.10 MiB** |
|---|---|---|---|---|
| **1000** | Add Duration | 1.205 s | 1.116 s | -7.4% |
| | Add Rate | 829 ops/s | 896 ops/s | +8.0% |
| | Recompute (`ovn-northd-loop`) | 21.0 ms | 12.0 ms | -42.9% |
| | Logical Flow (`lflow`) | 4.0 ms | 3.0 ms | -25.0% |
| | Peak RSS (`VmHWM`) | 41.09 MiB | 38.30 MiB | -6.8% |
| | Delete Duration | 0.124 s | 0.083 s | -33.1% |
| | Post-Delete Recompute | 1.0 ms | 1.0 ms | ~Parity (±1%) |
| | Post-Delete RSS | 41.51 MiB | 42.32 MiB | +2.0% |
| | **Retained Heap vs Baseline** | **+27.39 MiB** | **+27.30 MiB** | **-0.09 MiB** |
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
