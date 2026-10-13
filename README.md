# OVS and OVN Allocator Benchmark: glibc malloc vs jemalloc

This repository provides an automated, reproducible benchmarking framework comparing **GNU C Library allocator (`glibc malloc / ptmalloc3`)** against **jemalloc (5.3.0)** on **Open vSwitch (OVS)** and **OVN (Open Virtual Network)**.

---

## Benchmark Design & Methodology

Instead of arbitrary synthetic bundles, the benchmark isolates each fundamental OVS/OVN object type across orders of magnitude:
$$\text{Scales: } 1,\; 10,\; 100,\; 1{,}000,\; 10{,}000,\; 100{,}000$$

For every object type and scale $N$, measurements follow a strict **3-phase lifecycle**:

1. **Phase 1: Baseline (Idle)**
   - Initial clean daemon Resident Set Size (`VmRSS` in MiB) before any objects exist.
2. **Phase 2: After Add ($N$)**
   - **Addition Duration & Rate**: Time to commit $N$ objects and transaction throughput (ops/sec).
   - **Recompute Latency**: Full incremental-engine recompute measured via high-resolution stopwatches (`ovn-northd-loop`, `lflow`).
   - **Peak Memory**: High water mark resident memory (`VmHWM` in MiB) under peak object load.
3. **Phase 3: After Remove ($N$)**
   - **Deletion Duration**: Time to delete all $N$ objects and synchronize.
   - **Post-Deletion Recompute**: Execution time of cleanup engine iteration.
   - **Retained Heap ($\Delta$ RSS)**: `Post-Deletion VmRSS - Baseline VmRSS` measured after background allocator decay timers (quantifies heap fragmentation and memory locked in arenas).

---

## Evaluated Object Types

| Object Type | Description | Production Relevance (e.g. OVN-Kubernetes) |
|---|---|---|
| **Port (`port`)** | Logical switch ports with MAC, IP, and `port_security` | Pod network creation (`CNI ADD`) |
| **ACL (`acl`)** | Stateful Access Control Lists (`allow-related`) | Kubernetes `NetworkPolicy` and `AdminNetworkPolicy` |
| **Address Set (`address_set`)** | Dynamic IP sets matching ACLs | CIDR peer sets and Pod IP selectors |
| **Port Group (`port_group`)** | Group of logical ports applied to ACLs | NetworkPolicy applied-to pod groupings |
| **Load Balancer (`load_balancer`)** | VIPs mapped to backend pod endpoints | Kubernetes Services & EndpointSlices |
| **OpenFlow Flow (`flow`)** | OpenFlow rules across tables in OVS (`ovs-vswitchd`) | Datapath packet matching, MegaFlow caching |
| **Combined (`combined`)** | Proportional combination of Routers, Switches, Ports, LBs, ACLs | Full SDN cluster topology simulation |

---

## Measured Results Summary

Full per-object scale tables are recorded in [`results/BENCHMARK_REPORT.md`](results/BENCHMARK_REPORT.md). A high-level comparison at scale ($N = 1{,}000$ to $10{,}000$):

| Object Type | Scale ($N$) | Metric | glibc malloc | jemalloc | Relative Difference |
|---|---|---|---|---|---|
| **Port (LSP)** | 10,000 | Add Duration | 32.49 s | 30.22 s | -7.0% |
| | | Recompute (`ovn-northd-loop`) | 127.0 ms | 94.0 ms | **-26.0%** |
| | | Peak RSS (`VmHWM`) | 180.16 MiB | 162.70 MiB | **-9.7%** |
| | | Retained Heap after Delete | +165.74 MiB | +116.14 MiB | **-49.60 MiB (-29.9%)** |
| **ACL** | 10,000 | Add Duration | 11.85 s | 10.14 s | -14.4% |
| | | Recompute (`ovn-northd-loop`) | 87.0 ms | 56.0 ms | **-35.6%** |
| | | Peak RSS (`VmHWM`) | 82.80 MiB | 74.39 MiB | **-10.2%** |
| **Port Group** | 10,000 | Add Duration | 1.15 s | 1.05 s | -8.7% |
| | | Recompute (`ovn-northd-loop`) | 49.0 ms | 36.0 ms | **-26.5%** |
| **Load Balancer** | 5,000 | Add Duration | 3.01 s | 2.73 s | -9.3% |
| | | Recompute (`ovn-northd-loop`) | 52.0 ms | 36.0 ms | **-30.8%** |
| | | Peak RSS (`VmHWM`) | 62.09 MiB | 56.03 MiB | **-9.8%** |
| | | Retained Heap after Delete | +47.00 MiB | +40.67 MiB | **-6.33 MiB (-13.5%)** |
| **OpenFlow Flow** | 100,000 | Add Duration | 2.46 s | 2.40 s | -2.4% |
| | | Insertion Rate | 40,650 flows/s | 41,719 flows/s | +2.6% |
| | | Peak RSS (`VmHWM`) | 93.46 MiB | 93.19 MiB | ~Parity (±1%) |
| **Combined** | 5,000 | Add Duration | 16.42 s | 14.03 s | -14.5% |
| | | Recompute (`ovn-northd-loop`) | 85.0 ms | 68.0 ms | **-20.0%** |
| | | Logical Flow (`lflow`) | 24.0 ms | 15.0 ms | **-37.5%** |
| | | Peak RSS (`VmHWM`) | 141.71 MiB | 124.41 MiB | **-12.2%** |
| | | Retained Heap after Delete | +132.02 MiB | +117.97 MiB | **-14.05 MiB (-10.6%)** |

---

## How to Run the Benchmark

### 1. Prerequisites & Builds
The repository builds OVS and OVN side-by-side with identical compiler flags:

```bash
# glibc build (default system allocator)
build/ovs-glibc
build/ovn-glibc

# jemalloc build (linked with -ljemalloc -fno-lto)
build/ovs-jemalloc
build/ovn-jemalloc
```

### 2. Executing the Benchmark Suite
The entire suite is controlled by a single, self-contained Python script ([`bench.py`](bench.py)):

```bash
# Run all isolated object benchmarks across all scales
./bench.py --scales 1 10 100 1000 10000 --objects all

# Run specific object types
./bench.py --scales 100 1000 10000 --objects port acl load_balancer

# Run OpenFlow datapath rules up to 100,000 flows
./bench.py --scales 1000 10000 100000 --objects flow
```

All results are written to:
- [`results/BENCHMARK_REPORT.md`](results/BENCHMARK_REPORT.md): Markdown comparison tables with relative percentages.
- [`results/benchmark_data.json`](results/benchmark_data.json): Machine-readable raw metrics.

---

## Evaluating on Live OVN-Kubernetes via `LD_PRELOAD`

In live Kubernetes/OpenShift clusters running OVN-Kubernetes, you do not need to recompile binaries or rebuild container images. Because `ovn-northd`, `ovsdb-server`, `ovn-controller`, and `ovs-vswitchd` are dynamically linked C executables, `LD_PRELOAD` intercepts all memory allocation calls:

```yaml
# Add to container env in ovnkube-control-plane / ovnkube-node DaemonSets:
env:
  - name: LD_PRELOAD
    value: "/usr/lib64/libjemalloc.so.2"
  - name: MALLOC_CONF
    value: "background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:3000,narenas:4"
```

To run end-to-end Kubernetes scale tests against the running cluster, use `kube-burner` from `ovn-kubernetes/contrib/perf`:

```bash
cd /path/to/ovn-kubernetes/contrib/perf
kube-burner init -c workloads/kubelet-density-cni.yml
python3 generate_perf_report.py --workload kubelet-density-cni
```
