# OVS and OVN Allocator Benchmark: glibc malloc vs jemalloc

This repository provides an automated, reproducible benchmarking framework comparing **GNU C Library allocator (`glibc malloc / ptmalloc3`)** against **jemalloc (5.3.0)** on **Open vSwitch (OVS)** and **OVN (Open Virtual Network)**.

---

## Benchmark Design & Methodology

Instead of arbitrary synthetic bundles or shell CLI wrappers, the benchmark evaluates isolated OVS/OVN object types across orders of magnitude:
$$\text{Scales: } 1,\; 10,\; 100,\; 1{,}000,\; 10{,}000,\; 100{,}000$$

All OVN Northbound database transactions are executed via **native [`github.com/ovn-kubernetes/libovsdb`](https://github.com/ovn-kubernetes/libovsdb)** JSON-RPC over Unix domain sockets, accurately mirroring the exact client path used by `ovnkube-master` in production.

For every object type and scale $N$, measurements follow a strict **3-phase lifecycle**:

1. **Phase 1: Baseline (Idle)**
   - Initial clean daemon Resident Set Size (`VmRSS` in MiB) before any objects exist.
2. **Phase 2: After Add ($N$)**
   - **Addition Duration & Rate**: Time to commit $N$ objects and transaction throughput (ops/sec).
   - **Recompute Latency**: Full incremental-engine recompute measured via high-resolution stopwatches (`ovn-northd-loop`, `lflow`) across worker thread counts (`--threads 1 4 8`).
   - **Peak Memory**: High water mark resident memory (`VmHWM` in MiB) under peak object load.
3. **Phase 3: After Remove ($N$)**
   - **Deletion Duration**: Time to delete all $N$ objects and synchronize.
   - **Post-Deletion Recompute**: Execution time of cleanup engine iteration.
   - **Retained Heap ($\Delta$ RSS)**: `Post-Deletion VmRSS - Baseline VmRSS` measured after background allocator decay timers (quantifies heap fragmentation and memory locked in internal arenas).

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
| **Classifier (`classifier`)** | Flow classification lookup scaling across worker threads (1T, 4T, 8T, 16T) | Datapath lookup concurrency and lock contention |
| **Combined (`combined`)** | Proportional combination of Routers, Switches, Ports, LBs, ACLs | Full SDN cluster topology simulation |

---

## Benchmark Results

All measured comparative data points, timings, recompute latencies, and memory footprint tables are recorded in the comprehensive benchmark report:

👉 **[Detailed Benchmark Report (`results/BENCHMARK_REPORT.md`)](results/BENCHMARK_REPORT.md)**

Raw machine-readable JSON metrics are also available in:
- [`results/benchmark_data.json`](results/benchmark_data.json)

---

## How to Run the Benchmarks

The benchmark runs inside an isolated container using distribution packages and dynamically toggles between `glibc malloc` and `jemalloc` at runtime using `LD_PRELOAD`. No local compilation, build directory, or host OVS/OVN dependencies are required.

### Prerequisites
- `docker` or `podman` installed and running.

### Execution

```bash
# Run the complete isolated object benchmark suite
./run-in-container.sh

# Pass custom options to the runner:
./run-in-container.sh -scales 1,10,100,1000 -threads 4 -objects port,acl,load_balancer
./run-in-container.sh -scales 1000,10000,100000 -objects flow
```

The runner automatically builds a multi-stage container (`Dockerfile`): compiles the static Go benchmark binary using `golang:1.24`, packages it into Ubuntu 24.04 with official packages (`openvswitch-switch`, `ovn-central`, `libjemalloc2`), runs the benchmarks, and writes the output directly to `./results/` on your host:
- [`results/BENCHMARK_REPORT.md`](results/BENCHMARK_REPORT.md): Detailed comparison tables with relative percentages.
- [`results/benchmark_data.json`](results/benchmark_data.json): Machine-readable raw metrics.

---

## Evaluating on Live OVN-Kubernetes via `LD_PRELOAD`

In live Kubernetes/OpenShift clusters running OVN-Kubernetes, binaries do not need to be recompiled. Because `ovn-northd`, `ovsdb-server`, `ovn-controller`, and `ovs-vswitchd` are dynamically linked C executables, `LD_PRELOAD` intercepts all memory allocation calls:

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
