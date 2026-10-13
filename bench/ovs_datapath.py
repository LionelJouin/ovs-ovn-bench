import os
import shutil
import signal
import subprocess
import time
from typing import Dict, Any, List
from .utils import get_process_memory, parse_stopwatch_output, summarize_metrics, terminate_process

class OVNControlPlaneBench:
    def __init__(self, glibc_ovs_build: str, glibc_ovn_build: str,
                 jemalloc_ovs_build: str, jemalloc_ovn_build: str,
                 ovn_src_dir: str):
        self.glibc_ovs = glibc_ovs_build
        self.glibc_ovn = glibc_ovn_build
        self.jemalloc_ovs = jemalloc_ovs_build
        self.jemalloc_ovn = jemalloc_ovn_build
        self.ovn_src = ovn_src_dir

    def run_scale_scenario(self, ovs_build: str, ovn_build: str,
                           n_switches: int = 80, ports_per_switch: int = 25,
                           n_routers: int = 8, n_acls: int = 600,
                           n_threads: int = 4) -> Dict[str, Any]:
        test_dir = f"/tmp/opencode/ovn_scale_{os.getpid()}_{int(time.time()*1000)%100000}"
        if os.path.exists(test_dir):
            shutil.rmtree(test_dir)
        os.makedirs(test_dir, exist_ok=True)

        env = os.environ.copy()
        nb_schema = f"{self.ovn_src}/ovn-nb.ovsschema"
        sb_schema = f"{self.ovn_src}/ovn-sb.ovsschema"

        nb_sock = f"unix:{test_dir}/nb.sock"
        sb_sock = f"unix:{test_dir}/sb.sock"

        # 1. Initialize databases
        subprocess.run([f"{ovs_build}/ovsdb/ovsdb-tool", "create", f"{test_dir}/nb.db", nb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovs_build}/ovsdb/ovsdb-tool", "create", f"{test_dir}/sb.db", sb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # 2. Start ovsdb-servers
        subprocess.Popen([
            f"{ovs_build}/ovsdb/ovsdb-server", "--detach", "--no-chdir",
            f"--pidfile={test_dir}/nb.pid", f"--unixctl={test_dir}/nb.ctl",
            f"--remote=punix:{test_dir}/nb.sock", f"{test_dir}/nb.db"
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        subprocess.Popen([
            f"{ovs_build}/ovsdb/ovsdb-server", "--detach", "--no-chdir",
            f"--pidfile={test_dir}/sb.pid", f"--unixctl={test_dir}/sb.ctl",
            f"--remote=punix:{test_dir}/sb.sock", f"{test_dir}/sb.db"
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        with open(f"{test_dir}/nb.pid") as f:
            nb_pid = int(f.read().strip())
        with open(f"{test_dir}/sb.pid") as f:
            sb_pid = int(f.read().strip())

        # 3. Initialize OVN DBs
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "init"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-sbctl", f"--db={sb_sock}", "init"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # 4. Start ovn-northd
        northd_cmd = [
            f"{ovn_build}/northd/ovn-northd", "--detach", "--no-chdir",
            f"--pidfile={test_dir}/northd.pid", f"--unixctl={test_dir}/northd.ctl",
            f"--ovnnb-db={nb_sock}", f"--ovnsb-db={sb_sock}",
            f"--n-threads={n_threads}"
        ]
        subprocess.Popen(northd_cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        with open(f"{test_dir}/northd.pid") as f:
            northd_pid = int(f.read().strip())

        # Enable stopwatches
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl",
                        "inc-engine/enable-stopwatch"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        t_build_start = time.perf_counter()

        # 5. Populate Topology: Routers, Switches, Ports, NATs, ACLs, Port Groups, Address Sets
        # Routers
        for r in range(n_routers):
            subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "lr-add", f"lr{r}"],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # Port Groups & Address Sets
        for g in range(max(1, n_switches // 2)):
            as_ips = ",".join([f"10.{g}.{p}.2" for p in range(ports_per_switch)])
            subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}",
                            "create", "Address_Set", f"name=as_{g}", f"addresses={as_ips}",
                            "--", "pg-add", f"pg_{g}"],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # Switches and Ports (in batches)
        sw_batch = []
        for s in range(n_switches):
            router_idx = s % n_routers
            lr_name = f"lr{router_idx}"
            lrp_name = f"lrp-sw{s}"
            lsp_router_name = f"sw{s}-lrp"
            subnet = f"10.{s}.0.0/24"
            lr_ip = f"10.{s}.0.1"

            sw_batch.extend([
                "--", "ls-add", f"sw{s}",
                "--", "set", "Logical_Switch", f"sw{s}", f"other_config:subnet={subnet}",
                "--", "lrp-add", lr_name, lrp_name, f"00:00:00:{router_idx:02x}:{s:02x}:01", f"{lr_ip}/24",
                "--", "lsp-add", f"sw{s}", lsp_router_name,
                "--", "set", "Logical_Switch_Port", lsp_router_name, "type=router",
                f"options:router-port={lrp_name}", "addresses=router"
            ])

            pg_idx = s % max(1, n_switches // 2)
            for p in range(ports_per_switch):
                pname = f"sw{s}-p{p}"
                mac = f"00:00:00:{s:02x}:{p:02x}:02"
                ip = f"10.{s}.{p}.2"
                sw_batch.extend([
                    "--", "lsp-add", f"sw{s}", pname,
                    "--", "lsp-set-addresses", pname, f"{mac} {ip}",
                    "--", "pg-set-ports", f"pg_{pg_idx}", pname
                ])

            if len(sw_batch) >= 200 or s == n_switches - 1:
                subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}"] + sw_batch[1:],
                               check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                sw_batch = []

        # ACLs
        acl_batch = []
        for a in range(n_acls):
            sw_idx = a % n_switches
            pg_idx = a % max(1, n_switches // 2)
            match = f"ip4.src == $as_{pg_idx} && tcp.dst == {8000 + (a % 100)}"
            acl_batch.extend([
                "--", "acl-add", f"sw{sw_idx}", "to-lport", str(1000 + (a % 500)), match, "allow-related"
            ])
            if len(acl_batch) >= 200 or a == n_acls - 1:
                subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}"] + acl_batch[1:],
                               check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                acl_batch = []

        # Sync SB
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "--wait=sb", "sync"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        build_time = time.perf_counter() - t_build_start

        mem_northd_populated = get_process_memory(northd_pid)
        mem_nb_populated = get_process_memory(nb_pid)

        # 6. Full Recompute Benchmark
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "stopwatch/reset"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        t_recompute_start = time.perf_counter()
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "inc-engine/recompute"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "--wait=sb", "sync"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        recompute_duration_ms = (time.perf_counter() - t_recompute_start) * 1000.0

        # Read stopwatches
        sw_loop_out = subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl",
                                               "stopwatch/show", "ovn-northd-loop"], text=True)
        sw_lflow_out = subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl",
                                                "stopwatch/show", "lflow"], text=True)
        sw_northd_out = subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl",
                                                 "stopwatch/show", "northd"], text=True)

        sw_loop = parse_stopwatch_output(sw_loop_out)
        sw_lflow = parse_stopwatch_output(sw_lflow_out)
        sw_northd = parse_stopwatch_output(sw_northd_out)

        mem_northd_post_recompute = get_process_memory(northd_pid)

        # 7. Incremental Processing Test: Add 100 new ports across switches
        t_inc_start = time.perf_counter()
        inc_cmd = []
        for i in range(100):
            sw_idx = i % n_switches
            pname = f"inc-port-{i}"
            inc_cmd.extend(["--", "lsp-add", f"sw{sw_idx}", pname,
                            "--", "lsp-set-addresses", pname, f"00:00:ff:00:{sw_idx:02x}:{i:02x} 10.{sw_idx}.{i+100}.2"])
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}"] + inc_cmd[1:],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "--wait=sb", "sync"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        incremental_duration_ms = (time.perf_counter() - t_inc_start) * 1000.0

        # 8. Memory Fragmentation / Purge Test: Delete 50% of switches and ports
        del_cmd = []
        for s in range(0, n_switches, 2):
            del_cmd.extend(["--", "ls-del", f"sw{s}"])
        if del_cmd:
            subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}"] + del_cmd[1:],
                           check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "--wait=sb", "sync"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        # Recompute after deletion
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "inc-engine/recompute"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "--wait=sb", "sync"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        # Wait for allocator background decay (jemalloc purges dirty pages)
        time.sleep(3.0)
        mem_northd_after_del = get_process_memory(northd_pid)
        mem_nb_after_del = get_process_memory(nb_pid)

        # Clean up
        terminate_process(pid_file=f"{test_dir}/northd.pid")
        terminate_process(pid_file=f"{test_dir}/nb.pid")
        terminate_process(pid_file=f"{test_dir}/sb.pid")
        shutil.rmtree(test_dir, ignore_errors=True)

        return {
            "build_time_sec": build_time,
            "recompute_wall_ms": recompute_duration_ms,
            "stopwatch_northd_loop_ms": sw_loop["max_ms"],
            "stopwatch_lflow_ms": sw_lflow["max_ms"],
            "stopwatch_northd_ms": sw_northd["max_ms"],
            "incremental_ms": incremental_duration_ms,
            "northd_peak_rss_mib": mem_northd_populated["VmHWM"],
            "northd_post_del_rss_mib": mem_northd_after_del["VmRSS"],
            "northd_memory_freed_pct": (1.0 - (mem_northd_after_del["VmRSS"] / max(mem_northd_populated["VmHWM"], 0.001))) * 100.0,
            "ovsdb_nb_peak_rss_mib": mem_nb_populated["VmHWM"],
            "ovsdb_nb_post_del_rss_mib": mem_nb_after_del["VmRSS"],
        }

    def run_suite(self, iterations: int = 3) -> Dict[str, Any]:
        """Runs single-threaded and multi-threaded OVN scale tests."""
        scenarios = [
            {
                "name": "ovn_scale_medium_4threads",
                "switches": 80, "ports": 25, "routers": 8, "acls": 600, "threads": 4,
                "desc": "Medium Scale (2,000 Ports, 600 ACLs, 8 Routers, 4 Threads)"
            },
            {
                "name": "ovn_scale_medium_1thread",
                "switches": 80, "ports": 25, "routers": 8, "acls": 600, "threads": 1,
                "desc": "Medium Scale (2,000 Ports, 600 ACLs, 8 Routers, 1 Thread)"
            },
            {
                "name": "ovn_scale_large_8threads",
                "switches": 140, "ports": 30, "routers": 12, "acls": 1000, "threads": 8,
                "desc": "Large Scale (4,200 Ports, 1,000 ACLs, 12 Routers, 8 Threads)"
            }
        ]

        metrics_keys = [
            "build_time_sec", "recompute_wall_ms", "stopwatch_northd_loop_ms",
            "stopwatch_lflow_ms", "stopwatch_northd_ms", "incremental_ms",
            "northd_peak_rss_mib", "northd_post_del_rss_mib", "northd_memory_freed_pct",
            "ovsdb_nb_peak_rss_mib", "ovsdb_nb_post_del_rss_mib"
        ]

        full_results = {}

        for sc in scenarios:
            s_name = sc["name"]
            print(f"--- Running OVN Benchmark: {sc['desc']} ({iterations} iterations) ---")
            full_results[s_name] = {
                "desc": sc["desc"],
                "glibc": {k: [] for k in metrics_keys},
                "jemalloc": {k: [] for k in metrics_keys}
            }

            for it in range(1, iterations + 1):
                print(f"  Iteration {it}/{iterations} - glibc...")
                r_glibc = self.run_scale_scenario(
                    self.glibc_ovs, self.glibc_ovn,
                    n_switches=sc["switches"], ports_per_switch=sc["ports"],
                    n_routers=sc["routers"], n_acls=sc["acls"],
                    n_threads=sc["threads"]
                )
                for k, v in r_glibc.items(): full_results[s_name]["glibc"][k].append(v)

                print(f"  Iteration {it}/{iterations} - jemalloc...")
                r_jemalloc = self.run_scale_scenario(
                    self.jemalloc_ovs, self.jemalloc_ovn,
                    n_switches=sc["switches"], ports_per_switch=sc["ports"],
                    n_routers=sc["routers"], n_acls=sc["acls"],
                    n_threads=sc["threads"]
                )
                for k, v in r_jemalloc.items(): full_results[s_name]["jemalloc"][k].append(v)

            summary = {"desc": sc["desc"], "glibc": {}, "jemalloc": {}}
            for alloc in ["glibc", "jemalloc"]:
                for k in metrics_keys:
                    summary[alloc][k] = summarize_metrics(full_results[s_name][alloc][k])
            full_results[s_name]["summary"] = summary

        return full_results
