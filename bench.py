#!/usr/bin/env python3
"""
OVS & OVN Allocator Benchmark: glibc malloc vs jemalloc
Systematic isolated object scaling (1, 10, 100, 1000, 10000, 100000)
Measuring 3-phase lifecycle: Baseline (idle) -> After Add -> After Remove
"""

import argparse
import datetime
import json
import os
import platform
import shutil
import signal
import subprocess
import time
from typing import Dict, Any, List, Optional

OVS_SRC = "/home/ljouin/workspaces/openvswitch/ovs"
OVN_SRC = "/home/ljouin/workspaces/ovn-org/ovn"
BUILD_GLIBC_OVS = "/home/ljouin/workspaces/LionelJouin/ovs-ovn-bench/build/ovs-glibc"
BUILD_GLIBC_OVN = "/home/ljouin/workspaces/LionelJouin/ovs-ovn-bench/build/ovn-glibc"
BUILD_JEMALLOC_OVS = "/home/ljouin/workspaces/LionelJouin/ovs-ovn-bench/build/ovs-jemalloc"
BUILD_JEMALLOC_OVN = "/home/ljouin/workspaces/LionelJouin/ovs-ovn-bench/build/ovn-jemalloc"

def get_proc_mem(pid: int) -> Dict[str, float]:
    """Read VmRSS and VmHWM in MiB from /proc/[pid]/status."""
    res = {"VmRSS": 0.0, "VmHWM": 0.0}
    try:
        with open(f"/proc/{pid}/status") as f:
            for line in f:
                parts = line.split()
                if len(parts) >= 2:
                    k = parts[0].rstrip(":")
                    if k in res:
                        res[k] = round(float(parts[1]) / 1024.0, 2)
    except Exception:
        pass
    return res

def parse_stopwatch(out: str) -> float:
    """Parse Maximum from ovn-appctl stopwatch/show."""
    for line in out.splitlines():
        if "Maximum:" in line:
            parts = line.split(":")
            if len(parts) >= 2:
                try:
                    return float(parts[1].strip().split()[0])
                except Exception:
                    pass
    return 0.0

def kill_pid(pid_file: str):
    if os.path.exists(pid_file):
        try:
            with open(pid_file) as f:
                pid = int(f.read().strip())
            os.kill(pid, signal.SIGTERM)
            for _ in range(20):
                time.sleep(0.05)
                try:
                    os.kill(pid, 0)
                except ProcessLookupError:
                    return
            os.kill(pid, signal.SIGKILL)
        except Exception:
            pass

class OVNEnvironment:
    """Manages an isolated sandboxed OVN environment (NBDB, SBDB, ovn-northd)."""
    def __init__(self, ovs_build: str, ovn_build: str, test_dir: str, n_threads: int = 4):
        self.ovs_build = ovs_build
        self.ovn_build = ovn_build
        self.test_dir = test_dir
        self.n_threads = n_threads
        self.nb_sock = f"unix:{test_dir}/nb.sock"
        self.sb_sock = f"unix:{test_dir}/sb.sock"

    def start(self):
        os.system(f"rm -rf {self.test_dir} && mkdir -p {self.test_dir}")
        nb_schema = f"{OVN_SRC}/ovn-nb.ovsschema"
        sb_schema = f"{OVN_SRC}/ovn-sb.ovsschema"

        subprocess.run([f"{self.ovs_build}/ovsdb/ovsdb-tool", "create", f"{self.test_dir}/nb.db", nb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{self.ovs_build}/ovsdb/ovsdb-tool", "create", f"{self.test_dir}/sb.db", sb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        subprocess.Popen([f"{self.ovs_build}/ovsdb/ovsdb-server", "--detach", "--no-chdir",
                          f"--pidfile={self.test_dir}/nb.pid", f"--unixctl={self.test_dir}/nb.ctl",
                          f"--remote=punix:{self.test_dir}/nb.sock", f"{self.test_dir}/nb.db"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        subprocess.Popen([f"{self.ovs_build}/ovsdb/ovsdb-server", "--detach", "--no-chdir",
                          f"--pidfile={self.test_dir}/sb.pid", f"--unixctl={self.test_dir}/sb.ctl",
                          f"--remote=punix:{self.test_dir}/sb.sock", f"{self.test_dir}/sb.db"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        subprocess.run([f"{self.ovn_build}/utilities/ovn-nbctl", f"--db={self.nb_sock}", "init"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{self.ovn_build}/utilities/ovn-sbctl", f"--db={self.sb_sock}", "init"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        subprocess.Popen([f"{self.ovn_build}/northd/ovn-northd", "--detach", "--no-chdir",
                          f"--pidfile={self.test_dir}/northd.pid", f"--unixctl={self.test_dir}/northd.ctl",
                          f"--ovnnb-db={self.nb_sock}", f"--ovnsb-db={self.sb_sock}",
                          f"--n-threads={self.n_threads}"],
                         stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        with open(f"{self.test_dir}/northd.pid") as f:
            self.northd_pid = int(f.read().strip())

        subprocess.run([f"{self.ovn_build}/utilities/ovn-appctl", "-t", f"{self.test_dir}/northd.ctl",
                        "inc-engine/enable-stopwatch"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def nbctl(self, args: List[str]):
        return subprocess.run([f"{self.ovn_build}/utilities/ovn-nbctl", f"--db={self.nb_sock}"] + args,
                              check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

    def sync(self):
        self.nbctl(["--wait=sb", "sync"])

    def recompute(self) -> Dict[str, float]:
        subprocess.run([f"{self.ovn_build}/utilities/ovn-appctl", "-t", f"{self.test_dir}/northd.ctl",
                        "stopwatch/reset"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        t0 = time.perf_counter()
        subprocess.run([f"{self.ovn_build}/utilities/ovn-appctl", "-t", f"{self.test_dir}/northd.ctl",
                        "inc-engine/recompute"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.sync()
        wall_ms = round((time.perf_counter() - t0) * 1000.0, 2)

        out_loop = subprocess.check_output([f"{self.ovn_build}/utilities/ovn-appctl", "-t", f"{self.test_dir}/northd.ctl",
                                            "stopwatch/show", "ovn-northd-loop"], text=True)
        out_lflow = subprocess.check_output([f"{self.ovn_build}/utilities/ovn-appctl", "-t", f"{self.test_dir}/northd.ctl",
                                             "stopwatch/show", "lflow"], text=True)
        return {
            "wall_ms": wall_ms,
            "northd_loop_ms": parse_stopwatch(out_loop),
            "lflow_ms": parse_stopwatch(out_lflow),
        }

    def stop(self):
        kill_pid(f"{self.test_dir}/northd.pid")
        kill_pid(f"{self.test_dir}/nb.pid")
        kill_pid(f"{self.test_dir}/sb.pid")
        shutil.rmtree(self.test_dir, ignore_errors=True)

# ---------------------------------------------------------------------------
# Isolated Benchmark Functions: 3-Phase Lifecycle
# ---------------------------------------------------------------------------

def bench_port(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    env.nbctl(["ls-add", "bench-ls"])
    env.sync()
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    # Phase 2: Add N ports
    t0 = time.perf_counter()
    batch = []
    for i in range(n):
        pname = f"p-{i}"
        mac = f"00:00:{(i>>8)&255:02x}:{i&255:02x}:00:02"
        ip = f"10.{(i>>8)&255}.{i&255}.2"
        batch.extend([
            "--", "lsp-add", "bench-ls", pname,
            "--", "lsp-set-addresses", pname, f"{mac} {ip}",
            "--", "lsp-set-port-security", pname, f"{mac} {ip}"
        ])
        if len(batch) >= 600 or i == n - 1:
            env.nbctl(batch[1:])
            batch = []
    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove N ports
    t1 = time.perf_counter()
    del_batch = []
    for i in range(n):
        del_batch.extend(["--", "lsp-del", f"p-{i}"])
        if len(del_batch) >= 600 or i == n - 1:
            env.nbctl(del_batch[1:])
            del_batch = []
    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)  # Allow allocator page decay
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_acl(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    env.nbctl(["ls-add", "bench-ls"])
    env.sync()
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    # Phase 2: Add N ACLs
    t0 = time.perf_counter()
    batch = []
    for i in range(n):
        batch.extend([
            "--", "acl-add", "bench-ls", "to-lport", str(1000 + (i % 500)),
            f"ip4.src == 10.{(i>>8)&255}.{i&255}.1 && tcp.dst == {1000 + (i % 1000)}", "allow-related"
        ])
        if len(batch) >= 300 or i == n - 1:
            env.nbctl(batch[1:])
            batch = []
    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove N ACLs
    t1 = time.perf_counter()
    env.nbctl(["acl-del", "bench-ls"])
    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_address_set(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    env.nbctl(["ls-add", "bench-ls"])
    env.sync()
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    # Phase 2: Create Address Set with N IPs and attach matching ACL
    t0 = time.perf_counter()
    # Chunk IPs into comma-separated string (all strictly unique and valid host IPs)
    ips = ",".join([f"10.{(i//62500)&255}.{((i//250)%250)+1}.{(i%250)+1}" for i in range(n)])
    env.nbctl(["create", "Address_Set", "name=bench_as", f"addresses={ips}",
               "--", "acl-add", "bench-ls", "to-lport", "1000", "ip4.src == $bench_as", "allow-related"])
    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove Address Set & ACL
    t1 = time.perf_counter()
    env.nbctl(["destroy", "Address_Set", "bench_as", "--", "acl-del", "bench-ls"])
    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_port_group(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    # Setup switch and N member ports
    env.nbctl(["ls-add", "bench-ls"])
    batch = []
    for i in range(n):
        batch.extend(["--", "lsp-add", "bench-ls", f"p-{i}"])
        if len(batch) >= 300 or i == n - 1:
            env.nbctl(batch[1:])
            batch = []
    env.sync()
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    # Phase 2: Create Port_Group with N ports & attach ACL
    t0 = time.perf_counter()
    env.nbctl(["pg-add", "bench_pg"] + [f"p-{i}" for i in range(n)] + [
        "--", "acl-add", "bench_pg", "to-lport", "1000", "outport == @bench_pg", "allow-related"
    ])
    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove Port_Group
    t1 = time.perf_counter()
    env.nbctl(["pg-del", "bench_pg"])
    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_load_balancer(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    env.nbctl(["ls-add", "bench-ls"])
    env.sync()
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    # Phase 2: Add N Load Balancers attached to switch
    t0 = time.perf_counter()
    batch = []
    for i in range(n):
        vip = f"10.96.{(i//250)&255}.{(i%250)+1}:80"
        backends = f"10.244.0.1:8080,10.244.0.2:8080"
        batch.extend(["--", "lb-add", f"lb-{i}", vip, backends,
                      "--", "ls-lb-add", "bench-ls", f"lb-{i}"])
        if len(batch) >= 400 or i == n - 1:
            env.nbctl(batch[1:])
            batch = []
    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove N Load Balancers
    t1 = time.perf_counter()
    del_batch = []
    for i in range(n):
        del_batch.extend(["--", "lb-del", f"lb-{i}"])
        if len(del_batch) >= 400 or i == n - 1:
            env.nbctl(del_batch[1:])
            del_batch = []
    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_flow(ovs_build: str, n: int) -> Dict[str, Any]:
    test_dir = f"/tmp/opencode/ovs_flow_bench_{os.getpid()}_{n}"
    os.system(f"rm -rf {test_dir} && mkdir -p {test_dir}")
    env = os.environ.copy()
    env["OVS_RUNDIR"] = test_dir
    env["OVS_LOGDIR"] = test_dir

    schema = f"{OVS_SRC}/vswitchd/vswitch.ovsschema"
    conf_db = f"{test_dir}/conf.db"
    sock = f"unix:{test_dir}/db.sock"

    subprocess.run([f"{ovs_build}/ovsdb/ovsdb-tool", "create", conf_db, schema],
                   env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.Popen([f"{ovs_build}/ovsdb/ovsdb-server", "--detach", "--no-chdir",
                      f"--pidfile={test_dir}/ovsdb.pid", f"--remote=punix:{test_dir}/db.sock", conf_db],
                     env=env).wait()
    subprocess.run([f"{ovs_build}/utilities/ovs-vsctl", f"--db={sock}", "--no-wait", "init"],
                   env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.Popen([f"{ovs_build}/vswitchd/ovs-vswitchd", "--enable-dummy=override", "--detach", "--no-chdir",
                      f"--pidfile={test_dir}/vswitchd.pid", sock], env=env).wait()

    with open(f"{test_dir}/vswitchd.pid") as f:
        vpid = int(f.read().strip())

    subprocess.run([f"{ovs_build}/utilities/ovs-vsctl", f"--db={sock}", "add-br", "br0",
                    "--", "set", "bridge", "br0", "datapath_type=dummy"], env=env, check=True, stdout=subprocess.DEVNULL)

    baseline = get_proc_mem(vpid)

    # Phase 2: Add N OpenFlow rules
    flows = []
    for i in range(n):
        flows.append(f"table={i%8},priority={(i%100)+10},ip,nw_src=10.{(i>>16)&255}.{(i>>8)&255}.{i&255},actions=output:{(i%4)+1}\n")
    flow_input = "".join(flows)

    t0 = time.perf_counter()
    subprocess.run([f"{ovs_build}/utilities/ovs-ofctl", "add-flows", "br0", "-"],
                   input=flow_input, text=True, env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    add_sec = round(time.perf_counter() - t0, 3)
    peak = get_proc_mem(vpid)

    # Phase 3: Remove N flows
    t1 = time.perf_counter()
    subprocess.run([f"{ovs_build}/utilities/ovs-ofctl", "del-flows", "br0"],
                   env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    del_sec = round(time.perf_counter() - t1, 3)

    time.sleep(2.0)
    post_del = get_proc_mem(vpid)

    kill_pid(f"{test_dir}/vswitchd.pid")
    kill_pid(f"{test_dir}/ovsdb.pid")
    shutil.rmtree(test_dir, ignore_errors=True)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": 0.0,
        "add_lflow_ms": 0.0,
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": 0.0,
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

def bench_combined(env: OVNEnvironment, n: int) -> Dict[str, Any]:
    """Integrated scenario deploying all object types proportionally to scale n."""
    env.recompute()
    baseline = get_proc_mem(env.northd_pid)

    t0 = time.perf_counter()
    # 1 router, 10 switches (or proportional)
    n_switches = max(1, n // 100)
    n_ports = n
    n_acls = n
    n_lbs = max(1, n // 20)

    cmd = []
    cmd.extend(["lr-add", "r-main"])
    for s in range(n_switches):
        sw = f"sw-{s}"
        cmd.extend(["--", "ls-add", sw,
                    "--", "lrp-add", "r-main", f"rp-{s}", f"00:00:ff:{s:02x}:00:01", f"10.{s}.0.1/24",
                    "--", "lsp-add", sw, f"sp-{s}",
                    "--", "set", "Logical_Switch_Port", f"sp-{s}", "type=router", f"options:router-port=rp-{s}", "addresses=router"])
    env.nbctl(cmd)

    # Ports
    batch = []
    for i in range(n_ports):
        sw = f"sw-{i % n_switches}"
        pname = f"p-{i}"
        mac = f"00:00:{(i>>8)&255:02x}:{i&255:02x}:00:02"
        ip = f"10.{i % n_switches}.{(i>>8)&255}.{i&255}"
        batch.extend(["--", "lsp-add", sw, pname,
                      "--", "lsp-set-addresses", pname, f"{mac} {ip}",
                      "--", "lsp-set-port-security", pname, f"{mac} {ip}"])
        if len(batch) >= 200 or i == n_ports - 1:
            env.nbctl(batch[1:])
            batch = []

    # ACLs
    acl_batch = []
    for i in range(n_acls):
        sw = f"sw-{i % n_switches}"
        acl_batch.extend(["--", "acl-add", sw, "to-lport", str(1000 + (i % 500)),
                          f"ip4.src == 10.0.{(i>>8)&255}.{i&255} && tcp.dst == 80", "allow-related"])
        if len(acl_batch) >= 200 or i == n_acls - 1:
            env.nbctl(acl_batch[1:])
            acl_batch = []

    # Load Balancers
    lb_batch = []
    for i in range(n_lbs):
        vip = f"10.96.{(i//250)&255}.{(i%250)+1}:80"
        backends = "10.0.0.1:8080,10.0.0.2:8080"
        lb_batch.extend(["--", "lb-add", f"lb-{i}", vip, backends,
                         "--", "ls-lb-add", f"sw-{i % n_switches}", f"lb-{i}"])
        if len(lb_batch) >= 200 or i == n_lbs - 1:
            env.nbctl(lb_batch[1:])
            lb_batch = []

    env.sync()
    add_sec = round(time.perf_counter() - t0, 3)

    rec_add = env.recompute()
    peak = get_proc_mem(env.northd_pid)

    # Phase 3: Remove all
    t1 = time.perf_counter()
    env.nbctl(["lr-del", "r-main"])
    del_sw = []
    for s in range(n_switches):
        del_sw.extend(["--", "ls-del", f"sw-{s}"])
        if len(del_sw) >= 300 or s == n_switches - 1:
            env.nbctl(del_sw[1:])
            del_sw = []

    del_lb = []
    for i in range(n_lbs):
        del_lb.extend(["--", "lb-del", f"lb-{i}"])
        if len(del_lb) >= 300 or i == n_lbs - 1:
            env.nbctl(del_lb[1:])
            del_lb = []

    env.sync()
    del_sec = round(time.perf_counter() - t1, 3)

    rec_del = env.recompute()
    time.sleep(2.0)
    post_del = get_proc_mem(env.northd_pid)

    return {
        "baseline_rss_mib": baseline["VmRSS"],
        "add_sec": add_sec,
        "add_rate": round(n / max(add_sec, 0.001), 1),
        "add_loop_ms": rec_add["northd_loop_ms"],
        "add_lflow_ms": rec_add["lflow_ms"],
        "peak_rss_mib": peak["VmHWM"],
        "del_sec": del_sec,
        "del_loop_ms": rec_del["northd_loop_ms"],
        "post_del_rss_mib": post_del["VmRSS"],
        "retained_rss_mib": round(post_del["VmRSS"] - baseline["VmRSS"], 2)
    }

# ---------------------------------------------------------------------------
# Runner and Formatter
# ---------------------------------------------------------------------------

def calc_pct(v_glibc: float, v_jemalloc: float) -> str:
    if v_glibc == 0:
        return "N/A"
    diff = ((v_jemalloc - v_glibc) / v_glibc) * 100.0
    if abs(diff) < 1.0:
        return "~Parity (±1%)"
    return f"{diff:+.1f}%"

def generate_report(results: Dict[str, Any], sys_info: Dict[str, str]) -> str:
    lines = []
    lines.append("# Isolated Object Scaling Benchmark: glibc malloc vs jemalloc")
    lines.append("")
    lines.append(f"**Date:** {datetime.datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"**System:** {sys_info.get('os')}")
    lines.append(f"**CPU:** {sys_info.get('cpu_model')} ({sys_info.get('cpu_cores')} vCPUs)")
    lines.append(f"**Memory:** {sys_info.get('total_ram')}")
    lines.append(f"**Glibc Version:** {sys_info.get('glibc_version')}")
    lines.append(f"**Jemalloc Version:** {sys_info.get('jemalloc_version')}")
    lines.append("")
    lines.append("---")
    lines.append("")
    lines.append("## Methodology")
    lines.append("")
    lines.append("Each benchmark evaluates an isolated OVN/OVS object type across powers of 10 (`1, 10, 100, 1,000, 10,000, 100,000`) through 3 lifecycle phases:")
    lines.append("1. **Baseline (Idle)**: Clean process RSS before creating objects.")
    lines.append("2. **After Add (N)**: Time taken to commit $N$ objects, full recompute latency (`ovn-northd-loop` stopwatch), and peak RSS (`VmHWM`).")
    lines.append("3. **After Remove (N)**: Time taken to delete all $N$ objects, post-deletion recompute latency, and retained heap memory (`Post-Deletion RSS - Baseline RSS`).")
    lines.append("")
    lines.append("---")
    lines.append("")

    obj_titles = {
        "port": "Logical Switch Port (LSP with IP, MAC, port_security)",
        "acl": "ACL (Access Control List with stateful allow-related)",
        "address_set": "Address Set (IP list matching ACL)",
        "port_group": "Port Group (Ports member grouping matching ACL)",
        "load_balancer": "Load Balancer (VIPs mapped to endpoints)",
        "flow": "OpenFlow Flows (OVS Datapath)",
        "combined": "Combined Workload (All objects scaled proportionally)"
    }

    for obj, scales_data in results.items():
        title = obj_titles.get(obj, obj.upper())
        lines.append(f"## {title}")
        lines.append("")
        lines.append("| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |")
        lines.append("|---|---|---|---|---|")

        for scale_str, alloc_data in scales_data.items():
            g = alloc_data.get("glibc", {})
            j = alloc_data.get("jemalloc", {})
            n_val = int(scale_str)

            # Add Duration
            lines.append(f"| **{n_val:,}** | Add Duration | {g.get('add_sec', 0):.3f} s | {j.get('add_sec', 0):.3f} s | {calc_pct(g.get('add_sec', 0), j.get('add_sec', 0))} |")
            lines.append(f"| | Add Rate | {g.get('add_rate', 0):,.1f} ops/s | {j.get('add_rate', 0):,.1f} ops/s | {calc_pct(g.get('add_rate', 0), j.get('add_rate', 0))} |")

            if obj != "flow":
                lines.append(f"| | Recompute (`ovn-northd-loop`) | {g.get('add_loop_ms', 0):.1f} ms | {j.get('add_loop_ms', 0):.1f} ms | {calc_pct(g.get('add_loop_ms', 0), j.get('add_loop_ms', 0))} |")
                lines.append(f"| | Logical Flow (`lflow`) | {g.get('add_lflow_ms', 0):.1f} ms | {j.get('add_lflow_ms', 0):.1f} ms | {calc_pct(g.get('add_lflow_ms', 0), j.get('add_lflow_ms', 0))} |")

            lines.append(f"| | Peak RSS (`VmHWM`) | {g.get('peak_rss_mib', 0):.2f} MiB | {j.get('peak_rss_mib', 0):.2f} MiB | {calc_pct(g.get('peak_rss_mib', 0), j.get('peak_rss_mib', 0))} |")
            lines.append(f"| | Delete Duration | {g.get('del_sec', 0):.3f} s | {j.get('del_sec', 0):.3f} s | {calc_pct(g.get('del_sec', 0), j.get('del_sec', 0))} |")

            if obj != "flow":
                lines.append(f"| | Post-Delete Recompute | {g.get('del_loop_ms', 0):.1f} ms | {j.get('del_loop_ms', 0):.1f} ms | {calc_pct(g.get('del_loop_ms', 0), j.get('del_loop_ms', 0))} |")

            lines.append(f"| | Post-Delete RSS | {g.get('post_del_rss_mib', 0):.2f} MiB | {j.get('post_del_rss_mib', 0):.2f} MiB | {calc_pct(g.get('post_del_rss_mib', 0), j.get('post_del_rss_mib', 0))} |")
            lines.append(f"| | **Retained Heap vs Baseline** | **{g.get('retained_rss_mib', 0):+.2f} MiB** | **{j.get('retained_rss_mib', 0):+.2f} MiB** | **{j.get('retained_rss_mib', 0) - g.get('retained_rss_mib', 0):+.2f} MiB** |")
            lines.append("|---|---|---|---|---|")

        lines.append("")

    return "\n".join(lines)

def main():
    parser = argparse.ArgumentParser(description="Isolated Object Scale Benchmark: glibc vs jemalloc")
    parser.add_argument("--scales", nargs="+", type=int, default=[1, 10, 100, 1000, 10000],
                        help="Scale powers of 10 to evaluate")
    parser.add_argument("--objects", nargs="+",
                        choices=["port", "acl", "address_set", "port_group", "load_balancer", "flow", "combined", "all"],
                        default=["all"], help="Which object benchmarks to run")
    parser.add_argument("--flow-max", type=int, default=100000, help="Max scale for OpenFlow rules")
    args = parser.parse_args()

    target_objects = args.objects
    if "all" in target_objects:
        target_objects = ["port", "acl", "address_set", "port_group", "load_balancer", "flow", "combined"]

    sys_info = {
        "os": platform.platform(),
        "cpu_model": subprocess.check_output("lscpu | grep 'Model name' | head -1", shell=True, text=True).split(":")[1].strip(),
        "cpu_cores": subprocess.check_output("nproc", shell=True, text=True).strip(),
        "total_ram": subprocess.check_output("free -h | grep 'Mem:'", shell=True, text=True).split()[1],
        "glibc_version": subprocess.check_output("ldd --version | head -1", shell=True, text=True).strip(),
        "jemalloc_version": subprocess.check_output("rpm -q jemalloc 2>/dev/null || echo '5.3.0'", shell=True, text=True).strip()
    }

    print(f"System: {sys_info['cpu_model']} | {sys_info['cpu_cores']} cores | {sys_info['total_ram']} RAM")
    print(f"Testing Objects: {target_objects}")
    print(f"Scales: {args.scales}\n")

    results = {}
    json_path = "results/benchmark_data.json"
    if os.path.exists(json_path):
        try:
            with open(json_path) as f:
                prev = json.load(f)
                if "data" in prev:
                    results = prev["data"]
        except Exception:
            pass

    for obj in target_objects:
        print(f"=== Benchmarking Object: {obj.upper()} ===")
        if obj not in results:
            results[obj] = {}
        scales = list(args.scales)
        if obj == "flow" and args.flow_max not in scales:
            scales.append(args.flow_max)
        if obj == "combined":
            # Cap combined scale to 5000 if 10000 is too large
            scales = [s for s in scales if s <= 5000]

        for n in scales:
            if str(n) not in results[obj]:
                results[obj][str(n)] = {}
            print(f"  Scale N = {n:,}...")

            for alloc, ovs_b, ovn_b in [("glibc", BUILD_GLIBC_OVS, BUILD_GLIBC_OVN),
                                         ("jemalloc", BUILD_JEMALLOC_OVS, BUILD_JEMALLOC_OVN)]:
                if obj == "flow":
                    r = bench_flow(ovs_b, n)
                else:
                    env = OVNEnvironment(ovs_b, ovn_b, f"/tmp/opencode/bench_{alloc}_{obj}_{n}")
                    env.start()
                    try:
                        if obj == "port": r = bench_port(env, n)
                        elif obj == "acl": r = bench_acl(env, n)
                        elif obj == "address_set": r = bench_address_set(env, n)
                        elif obj == "port_group": r = bench_port_group(env, n)
                        elif obj == "load_balancer": r = bench_load_balancer(env, n)
                        elif obj == "combined": r = bench_combined(env, n)
                    finally:
                        env.stop()

                results[obj][str(n)][alloc] = r
                add_rate = r["add_rate"]
                peak_rss = r["peak_rss_mib"]
                ret_rss = r["retained_rss_mib"]
                print(f"    [{alloc}] Add: {r['add_sec']}s ({add_rate:,} ops/s) | Loop: {r['add_loop_ms']}ms | Peak RSS: {peak_rss}MB | Retained: {ret_rss:+}MB")

    # Save JSON
    os.makedirs("results", exist_ok=True)
    with open("results/benchmark_data.json", "w") as f:
        json.dump({"system": sys_info, "data": results}, f, indent=2)
    print("\nSaved results/benchmark_data.json")

    # Generate Report
    report_md = generate_report(results, sys_info)
    with open("results/BENCHMARK_REPORT.md", "w") as f:
        f.write(report_md)
    print("Saved results/BENCHMARK_REPORT.md")

if __name__ == "__main__":
    main()
