# OVS and OVN Allocator Benchmark: glibc malloc vs jemalloc

Benchmarking framework comparing **GNU C Library allocator (`glibc malloc / ptmalloc3`)** against **jemalloc (5.3.0)** on **Open vSwitch (OVS)** and **OVN (Open Virtual Network)**.

---

## Methodology

The benchmark evaluates isolated OVS and OVN object types across orders of magnitude:
$$\text{Scales: } 1,\; 10,\; 100,\; 1{,}000,\; 10{,}000,\; 100{,}000$$

All OVN Northbound database transactions are executed via `libovsdb` JSON-RPC over Unix domain sockets.

For every object type and scale $N$, measurements follow a 3-phase lifecycle:

1. **Phase 1: Baseline (Idle)**: Daemon Resident Set Size (`VmRSS` in MiB) before creating objects.
2. **Phase 2: After Add ($N$)**:
   - **Duration & Rate**: Time to commit $N$ objects and throughput (ops/sec).
   - **Recompute Latency**: Full engine recompute measured via stopwatches (`ovn-northd-loop`, `lflow`) across thread counts (`-threads 1,4,8`).
   - **Peak Memory**: High water mark resident memory (`VmHWM` in MiB).
3. **Phase 3: After Remove ($N$)**:
   - **Deletion Duration**: Time to delete all $N$ objects and synchronize.
   - **Post-Deletion Recompute**: Execution time of cleanup engine iteration.
   - **Retained Heap ($\Delta$ RSS)**: `Post-Deletion VmRSS - Baseline VmRSS` measured after 5 seconds to allow background thread decay (`dirty_decay_ms:1000,muzzy_decay_ms:1000`).

---

## Evaluated Object Types

| Object Type | Description |
|---|---|
| **`port`** | Logical switch ports with MAC, IP, and `port_security` |
| **`acl`** | Stateful Access Control Lists (`allow-related`) |
| **`address_set`** | Dynamic IP sets matching ACLs |
| **`port_group`** | Logical port groupings matching ACLs |
| **`load_balancer`** | VIPs mapped to backend endpoints |
| **`flow`** | OpenFlow rules across tables in `ovs-vswitchd` |
| **`datapath`** | Datapath packet forwarding and megaflow caching via `netdev-dummy/receive` |
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
./run-in-container.sh -scales 1000,10000,100000 -objects flow,datapath
```

Results are written to `./results/` on the host:
- `results/BENCHMARK_REPORT.md`
- `results/benchmark_data.json`

---

## Using `LD_PRELOAD` on OVN-Kubernetes

To evaluate jemalloc on a running cluster without recompiling:

```yaml
env:
  - name: LD_PRELOAD
    value: "/usr/lib64/libjemalloc.so.2"
  - name: MALLOC_CONF
    value: "background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:1000,narenas:4"
```

To run end-to-end tests via `kube-burner`:

```bash
cd /path/to/ovn-kubernetes/contrib/perf
kube-burner init -c workloads/kubelet-density-cni.yml
python3 generate_perf_report.py --workload kubelet-density-cni
```
