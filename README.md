# OVS and OVN Allocator Benchmark: glibc malloc vs jemalloc

Benchmarking framework comparing **GNU C Library allocator (`glibc malloc / ptmalloc3`)** against **jemalloc (5.3.0)** on **Open vSwitch (OVS)** and **OVN (Open Virtual Network)**.

---

## Methodology

The benchmark evaluates individual OVN/OVS object types, OpenFlow rule scaling, and combined workloads across orders of magnitude:
$$\text{Scales: } 1,\; 10,\; 100,\; 1{,}000,\; 10{,}000,\; 100{,}000$$

All OVN Northbound database transactions are executed via `libovsdb` JSON-RPC over Unix domain sockets.

For every workload and scale $N$, measurements follow a 3-phase lifecycle across all pod components (`ovn-northd`, `ovn-controller`, `ovs-vswitchd`, and `ovsdb-servers`):

1. **Phase 1: Baseline (Idle)**: Total Pod and per-daemon Resident Set Size (`VmRSS` in MiB) before creating objects.
2. **Phase 2: After Add ($N$)**:
   - **Duration & Rate**: Time to commit $N$ objects and throughput (ops/sec).
   - **Recompute Latency**: Full engine recompute measured via stopwatches (`ovn-northd-loop`, `lflow`) across thread counts (`-threads 1,4,8`).
   - **Peak Memory**: High water mark resident memory (`VmHWM` in MiB) for the total pod and per-daemon breakdown.
3. **Phase 3: After Remove ($N$)**:
   - **Deletion Duration**: Time to delete all $N$ objects and synchronize.
   - **Post-Deletion Recompute**: Execution time of cleanup engine iteration.
   - **Retained Heap ($\Delta$ RSS)**: `Post-Deletion VmRSS - Baseline VmRSS` measured for the total pod and per daemon after page decay (`dirty_decay_ms:0,muzzy_decay_ms:0`).

---

## Evaluated Workloads & Object Types

| Object Type | Description |
|---|---|
| **`port`** | Logical switch ports with MAC, IP, and `port_security` |
| **`acl`** | Stateful Access Control Lists (`allow-related`) |
| **`address_set`** | Dynamic IP sets matching ACLs |
| **`port_group`** | Logical port groupings matching ACLs |
| **`load_balancer`** | VIPs mapped to backend endpoints |
| **`flow`** | OpenFlow rules across tables in `ovs-vswitchd` |
| **`combined`** | Scaled combination of Routers, Switches, Ports, LBs, and ACLs |

---

## Benchmark Results

All measured comparative data points, timings, recompute latencies, and memory footprint tables are recorded in:

- **[Benchmark Report (`results/BENCHMARK_REPORT.md`)](results/BENCHMARK_REPORT.md)**
- **Raw JSON metrics**: [`results/benchmark_data.json`](results/benchmark_data.json)

---

## Running the Benchmark

The benchmark runs inside a container using distribution packages and toggles `glibc malloc` and `jemalloc` at runtime using `LD_PRELOAD`.

### Prerequisites
- `docker` or `podman`

### Execution

```bash
# Run complete benchmark suite
./run-in-container.sh

# Run specific objects and scales
./run-in-container.sh -scales 1,10,100,1000 -threads 4 -objects port,acl,load_balancer
./run-in-container.sh -scales 1000,10000,100000 -objects flow
./run-in-container.sh -scales 10000 -objects port -runs 3
```

Results are written to `./results/` on the host:
- `results/BENCHMARK_REPORT.md`
- `results/benchmark_data.json`

---

## Allocator Memory Behavior

* **glibc malloc (`ptmalloc3`)**: Allocates memory in per-thread arenas using `brk()` and `mmap()`. Heap fragmentation and top-of-heap boundaries generally prevent physical memory from being unmapped when objects are deleted. As a result, daemon resident memory (`VmRSS`) typically remains pinned near peak.
* **jemalloc**: Manages allocations across size-class slabs and extents, allowing unused pages to be returned to the OS kernel:
  * **`muzzy_decay_ms:0`**: Forces jemalloc to use `madvise(MADV_DONTNEED)` instead of `MADV_FREE`. On Linux, `MADV_FREE` pages are retained by the kernel as `LazyFree` and continue to count towards container memory limits. `MADV_DONTNEED` immediately unmaps physical pages and lowers container RSS.
  * **`dirty_decay_ms:0`**: Synchronously purges empty slabs upon `free()`. Because network daemons often enter `epoll_wait()` and sit idle after cleanup, immediate purging avoids relying on future allocation activity to advance decay timers.

---

## OVN Built-in Memory Trimming Features

OVN provides native memory trimming capabilities designed to mitigate glibc memory retention by invoking `malloc_trim(0)` during inactivity or when caches contract:

### 1. `ovn-northd` Memory Trimming
* **`options:northd_trim_timeout`** (configured in the `NB_Global` table):
  * **Default**: `30000` ms (30 seconds). Minimum allowable value is `1000` ms.
  * **Mechanism**: When `ovn-northd` experiences inactivity lasting longer than this timeout, its internal `memory_trimmer` calls `malloc_trim(0)` to force glibc to release unused arena memory back to the Linux kernel.

### 2. `ovn-controller` Memory & Cache Trimming
* **`external_ids:ovn-trim-timeout-ms`** (configured on the local `Open_vSwitch` table):
  * **Default**: `30000` ms (30 seconds). Inactivity timeout before `ovn-controller` invokes `malloc_trim(0)`.
* **`external_ids:ovn-trim-limit-lflow-cache`**:
  * Minimum size of the logical flow cache (number of entries) required before trimming is permitted (default `1`).
* **`external_ids:ovn-trim-wmark-perc-lflow-cache`**:
  * High-watermark percentage (default `50`%). If the logical flow cache size falls below this percentage of its historical peak, automatic memory trimming is triggered immediately upon cache reduction rather than waiting for inactivity.

---

## Using `LD_PRELOAD` on OVN-Kubernetes

To evaluate jemalloc on a running cluster without recompiling:

```yaml
env:
  - name: LD_PRELOAD
    value: "/usr/lib64/libjemalloc.so.2"
  - name: MALLOC_CONF
    value: "background_thread:true,dirty_decay_ms:0,muzzy_decay_ms:0,narenas:4"
```

To run end-to-end tests via `kube-burner`:

```bash
cd /path/to/ovn-kubernetes/contrib/perf
kube-burner init -c workloads/kubelet-density-cni.yml
python3 generate_perf_report.py --workload kubelet-density-cni
```
