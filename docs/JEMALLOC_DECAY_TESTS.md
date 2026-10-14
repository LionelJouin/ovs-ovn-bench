# jemalloc Decay & Kernel Page Purging Reproduction Guide

This guide documents how to reproduce and inspect the low-level Linux kernel syscalls (`madvise`) and memory reclamation timelines for jemalloc 5.x across three distinct decay and background thread configurations.

These tests explain why container memory behaves differently under `dirty_decay_ms:0` versus delayed decay timers (`dirty_decay_ms:1000`), the role of `background_thread`, and how thread caching (`tcache`) affects memory release.

---

## Prerequisites

All tests can be executed directly inside the project's Ubuntu container image:

```bash
docker run --rm -it --entrypoint bash ghcr.io/lioneljouin/ovs-ovn-bench:latest
```

Inside the container, install `strace`, `gcc`, and development headers:

```bash
apt-get update && apt-get install -y --no-install-recommends strace gcc libc6-dev libjemalloc-dev
```

---

## Test 1: Immediate Synchronous Purging (`dirty_decay_ms:0, muzzy_decay_ms:0, background_thread:false`)

### Purpose
Demonstrates that setting `dirty_decay_ms:0,muzzy_decay_ms:0` forces jemalloc to completely bypass the intermediate Muzzy (`MADV_FREE`) state and immediately issue `MADV_DONTNEED` synchronously on the thread calling `free()`.

### Source Code (`test1_sync_purge.c`)
```c
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

int main() {
    void *ptrs[100];
    // Allocate 100 blocks of 128 KiB (~13 MiB total)
    for (int i = 0; i < 100; i++) {
        ptrs[i] = malloc(128 * 1024);
        memset(ptrs[i], 1, 128 * 1024);
    }

    fprintf(stderr, "--> Freeing all 100 blocks...\n");
    for (int i = 0; i < 100; i++) {
        free(ptrs[i]);
    }

    fprintf(stderr, "--> Done.\n");
    return 0;
}
```

### Build & Run
```bash
gcc -O2 test1_sync_purge.c -o /tmp/test1

strace -e madvise env \
  LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2 \
  MALLOC_CONF="background_thread:false,dirty_decay_ms:0,muzzy_decay_ms:0" \
  /tmp/test1
```

### Expected Output
You will observe dozens of synchronous `madvise(..., MADV_DONTNEED)` calls firing during the `free()` loop:
```text
--> Freeing all 100 blocks...
madvise(0x7ff82ae1d000, 135168, MADV_DONTNEED) = 0
madvise(0x7ff82ae3e000, 135168, MADV_DONTNEED) = 0
madvise(0x7ff82ae5f000, 135168, MADV_DONTNEED) = 0
...
--> Done.
+++ exited with 0 +++
```
* **Kernel Behavior**: Physical page frames are unmapped immediately. Process `RssAnon` and container cgroup memory drop back to baseline on the spot.

---

## Test 2: Background Thread Asynchronous Decay & Timeline (`dirty_decay_ms:1000, muzzy_decay_ms:0, background_thread:true`)

### Purpose
Measures the exact timeline from `free()` until all memory is unmapped when using a 1,000 ms decay window with jemalloc background purging threads enabled.

### Source Code (`test2_async_decay.c`)
```c
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <stdio.h>

int main() {
    void *ptrs[100];
    for (int i = 0; i < 100; i++) {
        ptrs[i] = malloc(128 * 1024);
        memset(ptrs[i], 1, 128 * 1024);
    }

    for (int i = 0; i < 100; i++) {
        free(ptrs[i]);
    }
    fprintf(stderr, "--> [t=0.000s] Freed all 100 blocks. Sleeping 5s to observe background thread...\n");
    sleep(5);
    fprintf(stderr, "--> [t=5.000s] Finished sleep.\n");
    return 0;
}
```

### Build & Run
Run with `-tt` (microsecond timestamps) and `-f` (trace child/background threads):
```bash
gcc -O2 test2_async_decay.c -o /tmp/test2

strace -tt -f -e madvise env \
  LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2 \
  MALLOC_CONF="background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:0" \
  /tmp/test2
```

### Expected Output & Timing Breakdown
```text
20:45:12.990 [t = 0.000s]  Freed all 100 blocks. Main thread sleeps.
                             (1000ms decay window passes in silence...)
20:45:14.439 [t = +1.449s] [pid 423] madvise(0x7f2046222000, 1892352, MADV_DONTNEED) = 0
20:45:14.439 [t = +1.449s] [pid 423] madvise(0x7f204537f000, 2568192, MADV_DONTNEED) = 0
20:45:14.439 [t = +1.449s] [pid 423] madvise(0x7f204507f000, 3108864, MADV_DONTNEED) = 0
20:45:14.632 [t = +1.642s] [pid 423] madvise(0x7f2044cff000, 3649536, MADV_DONTNEED) = 0
20:45:15.633 [t = +2.643s] [pid 423] madvise(0x7f2044800000, 2297856, MADV_DONTNEED) = 0
                             --> ALL MEMORY UNMAPPED
20:45:17.997 [t = +5.007s]  Main thread wakes up. Process exits.
```

### Timeline Summary:
1. **$t = 0.00\text{ s} \rightarrow 1.45\text{ s}$**: No syscalls. Pages remain dirty in userspace cache.
2. **$t = 1.45\text{ s}$**: The background thread (`pid 423`) wakes up (~450 ms after the 1,000 ms decay window) and begins unmapping batches via `MADV_DONTNEED`.
3. **$t = 2.64\text{ s}$**: The final extent is unmapped. Total time from `free()` to full OS return: **2.64 seconds**.

---

## Test 3: Natural Passive Decay Driven by Application Activity (`background_thread:false`)

### Purpose
Demonstrates how jemalloc's passive decay engine triggers **purely through natural application traffic without any manual `mallctl` calls or background threads**:
1. When background threads are disabled (`background_thread:false`), jemalloc advances arena decay ticks during ongoing allocation/deallocation events.
2. Under `dirty_decay_ms:1000, muzzy_decay_ms:0`: As background traffic continues, once the 1,000 ms decay window has passed, the ongoing allocations drive the decay sweep and trigger `madvise(..., MADV_DONTNEED)` naturally.
3. Under `dirty_decay_ms:1000, muzzy_decay_ms:1000`: Demonstrates the exact two-stage kernel transition:
   - At $t \approx 1.5\text{s}$: `madvise(..., MADV_FREE)` fires (dirty $\rightarrow$ muzzy).
   - At $t \approx 2.7\text{s}$: `madvise(..., MADV_DONTNEED)` fires (muzzy $\rightarrow$ clean/retained).

### Source Code (`test3_natural_decay.c`)
```c
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <stdio.h>

int main() {
    // 1. Initial burst: Allocate and free 100 blocks of 128 KiB (~13 MiB)
    void *ptrs[100];
    for (int i = 0; i < 100; i++) {
        ptrs[i] = malloc(128 * 1024);
        memset(ptrs[i], 1, 128 * 1024);
    }
    for (int i = 0; i < 100; i++) {
        free(ptrs[i]);
    }
    fprintf(stderr, "--> [t=0.000s] Freed 100 blocks (13 MiB). Simulating ongoing application traffic...\n");

    // 2. Simulate ongoing background application traffic (e.g. informers, probes, packet handling)
    // across 4 seconds (> 1000ms decay window)
    for (int step = 0; step < 40; step++) {
        usleep(100000); // 100ms interval
        void *bg[50];
        for (int j = 0; j < 50; j++) {
            bg[j] = malloc(32 * 1024);
            memset(bg[j], 2, 32 * 1024);
        }
        for (int j = 0; j < 50; j++) {
            free(bg[j]);
        }
    }

    fprintf(stderr, "--> [t=4.000s] Completed background workload.\n");
    return 0;
}
```

### Build & Run

#### A. Direct Purging (`dirty_decay_ms:1000, muzzy_decay_ms:0`)
```bash
gcc -O2 test3_natural_decay.c -o /tmp/test3

strace -tt -e madvise env \
  LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2 \
  MALLOC_CONF="background_thread:false,dirty_decay_ms:1000,muzzy_decay_ms:0" \
  /tmp/test3 2>&1 | grep -v "4096"
```

**Expected Output:**
```text
--> [t=0.000s] Freed 100 blocks (13 MiB). Simulating ongoing application traffic...
21:23:48.246 madvise(0x7f70c1780000, 2568192, MADV_DONTNEED) = 0
21:23:48.247 madvise(0x7f70c1480000, 3108864, MADV_DONTNEED) = 0
21:23:48.247 madvise(0x7f70c1100000, 3649536, MADV_DONTNEED) = 0
21:23:48.248 madvise(0x7f70c0c00000, 2297856, MADV_DONTNEED) = 0
21:23:48.248 madvise(0x7f70c1e1d000, 1105920, MADV_DONTNEED) = 0
--> [t=4.000s] Completed background workload.
+++ exited with 0 +++
```
* **Result**: Without background threads and with **zero `mallctl` calls**, the ongoing application traffic naturally drove the arena decay ticks. At $t \approx 1.5\text{s}$, jemalloc automatically issued `MADV_DONTNEED` and unmapped all 13 MiB!

---

#### B. Two-Stage Kernel Transition (`dirty_decay_ms:1000, muzzy_decay_ms:1000`)
```bash
strace -tt -e madvise env \
  LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2 \
  MALLOC_CONF="background_thread:false,dirty_decay_ms:1000,muzzy_decay_ms:1000" \
  /tmp/test3 2>&1 | grep -v "4096"
```

**Expected Output:**
```text
--> [t=0.000s] Freed 100 blocks. Simulating ongoing application traffic...
21:24:33.215 madvise(0x7f673ef80000, 2568192, MADV_FREE) = 0        <-- Phase 1: dirty -> muzzy (t ≈ 1.5s)
21:24:33.216 madvise(0x7f673ec80000, 3108864, MADV_FREE) = 0
21:24:33.216 madvise(0x7f673e900000, 3649536, MADV_FREE) = 0
21:24:33.216 madvise(0x7f673e400000, 2297856, MADV_FREE) = 0

21:24:34.420 madvise(0x7f673ef80000, 2568192, MADV_DONTNEED) = 0    <-- Phase 2: muzzy -> clean (t ≈ 2.7s)
21:24:34.420 madvise(0x7f673ec80000, 3108864, MADV_DONTNEED) = 0
21:24:34.421 madvise(0x7f673e900000, 3649536, MADV_DONTNEED) = 0
21:24:34.421 madvise(0x7f673e400000, 2297856, MADV_DONTNEED) = 0
--> [t=4.000s] Completed background workload.
+++ exited with 0 +++
```
* **Result**: Perfectly captures the full lifecycle on Linux:
  1. At $t \approx 1.5\text{s}$, the 1,000 ms dirty decay triggers `MADV_FREE`.
  2. At $t \approx 2.7\text{s}$, the subsequent 1,000 ms muzzy decay triggers `MADV_DONTNEED`, returning physical pages and reducing cgroup RSS.

---

## Comparison Summary

| Configuration | Purge Mechanism | Time to Return Memory | Driven By | Safe for Forked Daemons? |
|---|---|---|---|---|
| **`dirty_decay_ms:0, muzzy_decay_ms:0`** | Synchronous `MADV_DONTNEED` inside `free()` | **0 ms** (Instant) | Direct `free()` call | **Yes** (immune to `fork()` thread loss) |
| **`dirty_decay_ms:1000, muzzy_decay_ms:0` + `bg:true`** | Asynchronous `MADV_DONTNEED` via background thread | **~2.6 seconds** | Background worker pthread | **No** (background thread disabled after `fork()`) |
| **`dirty_decay_ms:1000, muzzy_decay_ms:0` + `bg:false`** | Passive decay via application allocation activity | **~1.5 seconds** | Ongoing `malloc()`/`free()` events | **Yes** (driven by the daemon's own threads) |
| **`dirty_decay_ms:1000, muzzy_decay_ms:1000` + `bg:false`** | Two-stage: `MADV_FREE` (1.5s) $\rightarrow$ `MADV_DONTNEED` (2.7s) | **~2.7 seconds** | Ongoing `malloc()`/`free()` events | **Yes** (matches real Kubernetes cluster behavior) |
