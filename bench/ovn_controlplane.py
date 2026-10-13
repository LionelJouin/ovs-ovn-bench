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

    def run_ovnkube_scenario(self, ovs_build: str, ovn_build: str,
                             n_nodes: int = 40, pods_per_node: int = 40,
                             n_services: int = 100, n_threads: int = 4) -> Dict[str, Any]:
        """Simulates full OVN-Kubernetes model: Cluster Router, Node Switches, Pods with Port Security,
        Load Balancer Groups (Services/VIPs), Port Groups & Address Sets (Network Policies), and Pod Churn."""
        test_dir = f"/tmp/opencode/ovnkube_bench_{os.getpid()}_{int(time.time()*1000)%100000}"
        if os.path.exists(test_dir):
            shutil.rmtree(test_dir)
        os.makedirs(test_dir, exist_ok=True)

        nb_schema = f"{self.ovn_src}/ovn-nb.ovsschema"
        sb_schema = f"{self.ovn_src}/ovn-sb.ovsschema"
        nb_sock = f"unix:{test_dir}/nb.sock"
        sb_sock = f"unix:{test_dir}/sb.sock"

        # 1. DB setup
        subprocess.run([f"{ovs_build}/ovsdb/ovsdb-tool", "create", f"{test_dir}/nb.db", nb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([f"{ovs_build}/ovsdb/ovsdb-tool", "create", f"{test_dir}/sb.db", sb_schema],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

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

        with open(f"{test_dir}/nb.pid") as f: nb_pid = int(f.read().strip())
        with open(f"{test_dir}/sb.pid") as f: sb_pid = int(f.read().strip())

        subprocess.run([f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}", "init"], check=True, stdout=subprocess.DEVNULL)
        subprocess.run([f"{ovn_build}/utilities/ovn-sbctl", f"--db={sb_sock}", "init"], check=True, stdout=subprocess.DEVNULL)

        # 2. Start northd
        subprocess.Popen([
            f"{ovn_build}/northd/ovn-northd", "--detach", "--no-chdir",
            f"--pidfile={test_dir}/northd.pid", f"--unixctl={test_dir}/northd.ctl",
            f"--ovnnb-db={nb_sock}", f"--ovnsb-db={sb_sock}",
            f"--n-threads={n_threads}"
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).wait()

        with open(f"{test_dir}/northd.pid") as f: northd_pid = int(f.read().strip())

        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "inc-engine/enable-stopwatch"],
                       check=True, stdout=subprocess.DEVNULL)

        t_build_start = time.perf_counter()
        cmd = [f"{ovn_build}/utilities/ovn-nbctl", f"--db={nb_sock}"]

        # A. Cluster Router & Load Balancer Group
        subprocess.run(cmd + ["lr-add", "ovn_cluster_router"], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(cmd + ["create", "Load_Balancer_Group", "name=k8s-lb-group"], check=True, stdout=subprocess.DEVNULL)
        lbg_uuid = subprocess.check_output(cmd + ["get", "Load_Balancer_Group", "k8s-lb-group", "_uuid"], text=True).strip()
        subprocess.run(cmd + ["set", "Logical_Router", "ovn_cluster_router", f"load_balancer_group={lbg_uuid}"], check=True, stdout=subprocess.DEVNULL)

        # B. Services (Load Balancers)
        lb_batch = []
        for svc in range(n_services):
            vip = f"10.96.{svc//256}.{(svc%256)+1}:80"
            backends = f"10.244.1.{svc%40+2}:8080,10.244.2.{svc%40+2}:8080"
            subprocess.run(cmd + ["lb-add", f"svc-{svc}", vip, backends], check=True, stdout=subprocess.DEVNULL)
            lb_uuid = subprocess.check_output(cmd + ["get", "Load_Balancer", f"svc-{svc}", "_uuid"], text=True).strip()
            lb_batch.extend(["--", "add", "Load_Balancer_Group", "k8s-lb-group", "load_balancer", lb_uuid])
            if len(lb_batch) >= 100 or svc == n_services - 1:
                subprocess.run(cmd + lb_batch[1:], check=True, stdout=subprocess.DEVNULL)
                lb_batch = []

        # C. Namespaces (Port Groups and Address Sets)
        for ns in range(n_nodes):
            as_ips = ",".join([f"10.244.{ns}.{p+2}" for p in range(pods_per_node)])
            subprocess.run(cmd + ["create", "Address_Set", f"name=as_ns_{ns}", f"addresses={as_ips}",
                                  "--", "pg-add", f"pg_ns_{ns}"], check=True, stdout=subprocess.DEVNULL)

        # D. Node Switches and Pod Ports (with port_security)
        sw_batch = []
        for node in range(n_nodes):
            sw = f"node-{node}"
            lrp = f"rtos-{sw}"
            lsp = f"stor-{sw}"
            sw_batch.extend([
                "--", "ls-add", sw,
                "--", "set", "Logical_Switch", sw, f"load_balancer_group={lbg_uuid}",
                "--", "lrp-add", "ovn_cluster_router", lrp, f"00:00:00:ff:{node:02x}:01", f"10.244.{node}.1/24",
                "--", "lsp-add", sw, lsp,
                "--", "set", "Logical_Switch_Port", lsp, "type=router", f"options:router-port={lrp}", "addresses=router"
            ])
            for p in range(pods_per_node):
                pod = f"{sw}-pod-{p}"
                mac = f"00:00:00:{node:02x}:{p:02x}:02"
                ip = f"10.244.{node}.{p+2}"
                sw_batch.extend([
                    "--", "lsp-add", sw, pod,
                    "--", "lsp-set-addresses", pod, f"{mac} {ip}",
                    "--", "lsp-set-port-security", pod, f"{mac} {ip}",
                    "--", "pg-set-ports", f"pg_ns_{node}", pod
                ])
            if len(sw_batch) >= 200 or node == n_nodes - 1:
                subprocess.run(cmd + sw_batch[1:], check=True, stdout=subprocess.DEVNULL)
                sw_batch = []

        # E. Network Policies: default-deny + allow-related matching Address Sets
        acl_batch = []
        for ns in range(n_nodes):
            acl_batch.extend([
                "--", "acl-add", f"pg_ns_{ns}", "to-lport", "1000", f"outport == @pg_ns_{ns}", "drop",
                "--", "acl-add", f"pg_ns_{ns}", "to-lport", "2000", f"ip4.src == $as_ns_{ns} && outport == @pg_ns_{ns}", "allow-related"
            ])
            if len(acl_batch) >= 200 or ns == n_nodes - 1:
                subprocess.run(cmd + acl_batch[1:], check=True, stdout=subprocess.DEVNULL)
                acl_batch = []

        # SB Sync
        subprocess.run(cmd + ["--wait=sb", "sync"], check=True, stdout=subprocess.DEVNULL)
        build_time = time.perf_counter() - t_build_start

        mem_northd_populated = get_process_memory(northd_pid)
        mem_nb_populated = get_process_memory(nb_pid)

        # 3. Full Recompute Stopwatch
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "stopwatch/reset"], check=True, stdout=subprocess.DEVNULL)
        t_rec_start = time.perf_counter()
        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "inc-engine/recompute"], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(cmd + ["--wait=sb", "sync"], check=True, stdout=subprocess.DEVNULL)
        recompute_duration_ms = (time.perf_counter() - t_rec_start) * 1000.0

        sw_loop = parse_stopwatch_output(subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "stopwatch/show", "ovn-northd-loop"], text=True))
        sw_lflow = parse_stopwatch_output(subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "stopwatch/show", "lflow"], text=True))
        sw_northd = parse_stopwatch_output(subprocess.check_output([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "stopwatch/show", "northd"], text=True))

        # 4. Incremental Pod Add (add 100 pods)
        t_inc_start = time.perf_counter()
        inc_batch = []
        for i in range(100):
            node_idx = i % n_nodes
            pod_name = f"inc-pod-{i}"
            mac = f"00:00:ff:00:{node_idx:02x}:{i:02x}"
            ip = f"10.244.{node_idx}.{i+100}"
            inc_batch.extend([
                "--", "lsp-add", f"node-{node_idx}", pod_name,
                "--", "lsp-set-addresses", pod_name, f"{mac} {ip}",
                "--", "lsp-set-port-security", pod_name, f"{mac} {ip}"
            ])
        subprocess.run(cmd + inc_batch[1:], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(cmd + ["--wait=sb", "sync"], check=True, stdout=subprocess.DEVNULL)
        incremental_duration_ms = (time.perf_counter() - t_inc_start) * 1000.0

        # 5. Pod Scale-Down / Memory Churn: Delete 25% of nodes and all their pods
        del_batch = []
        for n in range(0, n_nodes, 4):
            del_batch.extend(["--", "ls-del", f"node-{n}"])
        if del_batch:
            subprocess.run(cmd + del_batch[1:], check=True, stdout=subprocess.DEVNULL)
            subprocess.run(cmd + ["--wait=sb", "sync"], check=True, stdout=subprocess.DEVNULL)

        subprocess.run([f"{ovn_build}/utilities/ovn-appctl", "-t", f"{test_dir}/northd.ctl", "inc-engine/recompute"], check=True, stdout=subprocess.DEVNULL)
        subprocess.run(cmd + ["--wait=sb", "sync"], check=True, stdout=subprocess.DEVNULL)

        time.sleep(3.0)
        mem_northd_after_del = get_process_memory(northd_pid)
        mem_nb_after_del = get_process_memory(nb_pid)

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
                "name": "ovnkube_density_k8s",
                "desc": "OVN-Kubernetes Model (40 Nodes, 1,600 Pods, 100 Services/LBs, NetPols, Port Security)",
                "is_ovnkube": True,
                "nodes": 40, "pods": 40, "services": 80, "threads": 4
            },
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
                if sc.get("is_ovnkube"):
                    r_glibc = self.run_ovnkube_scenario(
                        self.glibc_ovs, self.glibc_ovn,
                        n_nodes=sc["nodes"], pods_per_node=sc["pods"],
                        n_services=sc["services"], n_threads=sc["threads"]
                    )
                else:
                    r_glibc = self.run_scale_scenario(
                        self.glibc_ovs, self.glibc_ovn,
                        n_switches=sc["switches"], ports_per_switch=sc["ports"],
                        n_routers=sc["routers"], n_acls=sc["acls"],
                        n_threads=sc["threads"]
                    )
                for k, v in r_glibc.items(): full_results[s_name]["glibc"][k].append(v)

                print(f"  Iteration {it}/{iterations} - jemalloc...")
                if sc.get("is_ovnkube"):
                    r_jemalloc = self.run_ovnkube_scenario(
                        self.jemalloc_ovs, self.jemalloc_ovn,
                        n_nodes=sc["nodes"], pods_per_node=sc["pods"],
                        n_services=sc["services"], n_threads=sc["threads"]
                    )
                else:
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
