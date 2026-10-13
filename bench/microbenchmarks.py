import os
import re
import subprocess
import time
from typing import Dict, Any, List
from .utils import summarize_metrics

class Microbenchmarks:
    def __init__(self, glibc_bin: str, jemalloc_bin: str):
        self.glibc_bin = glibc_bin
        self.jemalloc_bin = jemalloc_bin

    def run_command(self, bin_path: str, args: List[str]) -> str:
        cmd = [bin_path] + args
        res = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=True)
        return res.stdout

    def run_classifier_suite(self, iterations: int = 5) -> Dict[str, Any]:
        """Runs classifier benchmarks under different thread counts."""
        test_cases = [
            {"name": "classifier_1t", "rules": "20000", "prio": "20", "tables": "10", "threads": "1", "lookups": "200000"},
            {"name": "classifier_8t", "rules": "20000", "prio": "20", "tables": "10", "threads": "8", "lookups": "200000"},
            {"name": "classifier_16t", "rules": "20000", "prio": "20", "tables": "10", "threads": "16", "lookups": "200000"},
        ]
        results = {}

        for tc in test_cases:
            tc_name = tc["name"]
            results[tc_name] = {
                "glibc": {"no_wc_lookups_per_sec": [], "wc_lookups_per_sec": [], "no_wc_time_ms": [], "wc_time_ms": []},
                "jemalloc": {"no_wc_lookups_per_sec": [], "wc_lookups_per_sec": [], "no_wc_time_ms": [], "wc_time_ms": []}
            }
            args = ["test-classifier", "benchmark", tc["rules"], tc["prio"], tc["tables"], tc["threads"], tc["lookups"]]

            for _ in range(iterations):
                for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                    out = self.run_command(bin_path, args)
                    sections = out.split("Without wildcards:")
                    if len(sections) > 1:
                        no_wc_part = sections[1].split("With wildcards:")[0]
                        wc_part = sections[1].split("With wildcards:")[1]
                        
                        m1 = re.search(r"classifier lookups:\s+(\d+)\s+ms,\s+(\d+)\s+lookups/sec", no_wc_part)
                        m2 = re.search(r"classifier lookups:\s+(\d+)\s+ms,\s+(\d+)\s+lookups/sec", wc_part)
                        if m1 and m2:
                            results[tc_name][alloc]["no_wc_time_ms"].append(float(m1.group(1)))
                            results[tc_name][alloc]["no_wc_lookups_per_sec"].append(float(m1.group(2)))
                            results[tc_name][alloc]["wc_time_ms"].append(float(m2.group(1)))
                            results[tc_name][alloc]["wc_lookups_per_sec"].append(float(m2.group(2)))

            # Summarize
            for alloc in ["glibc", "jemalloc"]:
                for metric in ["no_wc_lookups_per_sec", "wc_lookups_per_sec", "no_wc_time_ms", "wc_time_ms"]:
                    results[tc_name][alloc][metric] = summarize_metrics(results[tc_name][alloc][metric])

        return results

    def run_cmap_suite(self, iterations: int = 5) -> Dict[str, Any]:
        """Runs cmap/hmap concurrent benchmarks."""
        results = {
            "cmap_8t": {"glibc": {}, "jemalloc": {}},
            "cmap_16t": {"glibc": {}, "jemalloc": {}}
        }
        test_cases = [
            ("cmap_8t", ["test-cmap", "benchmark", "200000", "8", "10", "10"]),
            ("cmap_16t", ["test-cmap", "benchmark", "200000", "16", "10", "10"])
        ]

        for name, args in test_cases:
            data = {
                "glibc": {"cmap_insert_ms": [], "cmap_search_ms": [], "cmap_destroy_ms": [], "hmap_search_ms": []},
                "jemalloc": {"cmap_insert_ms": [], "cmap_search_ms": [], "cmap_destroy_ms": [], "hmap_search_ms": []}
            }
            for _ in range(iterations):
                for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                    out = self.run_command(bin_path, args)
                    m_insert = re.search(r"cmap insert:\s+(\d+)\s+ms", out)
                    m_search = re.search(r"cmap search:\s+(\d+)\s+ms", out)
                    m_destroy = re.search(r"cmap destroy:\s+(\d+)\s+ms", out)
                    m_hmap = re.search(r"hmap search:\s+(\d+)\s+ms", out)
                    if m_insert: data[alloc]["cmap_insert_ms"].append(float(m_insert.group(1)))
                    if m_search: data[alloc]["cmap_search_ms"].append(float(m_search.group(1)))
                    if m_destroy: data[alloc]["cmap_destroy_ms"].append(float(m_destroy.group(1)))
                    if m_hmap: data[alloc]["hmap_search_ms"].append(float(m_hmap.group(1)))

            for alloc in ["glibc", "jemalloc"]:
                for k in ["cmap_insert_ms", "cmap_search_ms", "cmap_destroy_ms", "hmap_search_ms"]:
                    results[name][alloc][k] = summarize_metrics(data[alloc][k])

        return results

    def run_ccmap_suite(self, iterations: int = 5) -> Dict[str, Any]:
        """Runs counting concurrent map benchmarks."""
        results = {"ccmap_8t": {"glibc": {}, "jemalloc": {}}, "ccmap_16t": {"glibc": {}, "jemalloc": {}}}
        test_cases = [
            ("ccmap_8t", ["test-ccmap", "benchmark", "200000", "8", "10"]),
            ("ccmap_16t", ["test-ccmap", "benchmark", "200000", "16", "10"])
        ]
        for name, args in test_cases:
            data = {
                "glibc": {"insert_ms": [], "search_ms": [], "destroy_ms": []},
                "jemalloc": {"insert_ms": [], "search_ms": [], "destroy_ms": []}
            }
            for _ in range(iterations):
                for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                    out = self.run_command(bin_path, args)
                    m_ins = re.search(r"ccmap insert:\s+(\d+)\s+ms", out)
                    m_srch = re.search(r"ccmap search:\s+(\d+)\s+ms", out)
                    m_dst = re.search(r"ccmap destroy:\s+(\d+)\s+ms", out)
                    if m_ins: data[alloc]["insert_ms"].append(float(m_ins.group(1)))
                    if m_srch: data[alloc]["search_ms"].append(float(m_srch.group(1)))
                    if m_dst: data[alloc]["destroy_ms"].append(float(m_dst.group(1)))

            for alloc in ["glibc", "jemalloc"]:
                for k in ["insert_ms", "search_ms", "destroy_ms"]:
                    results[name][alloc][k] = summarize_metrics(data[alloc][k])

        return results

    def run_mpsc_suite(self, iterations: int = 5) -> Dict[str, Any]:
        """Runs multi-producer single-consumer queue benchmarks."""
        results = {"mpsc_queue": {"glibc": {}, "jemalloc": {}}}
        args = ["test-mpsc-queue", "benchmark", "500000", "8"]
        data = {
            "glibc": {"reader_ms": [], "avg_ms": [], "guarded_list_avg_ms": []},
            "jemalloc": {"reader_ms": [], "avg_ms": [], "guarded_list_avg_ms": []}
        }
        for _ in range(iterations):
            for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                out = self.run_command(bin_path, args)
                m_mpsc = re.search(r"mpsc-queue:\s+(\d+)\s+.*?\s+(\d+)\s+ms", out)
                m_glist = re.search(r"guarded list:\s+.*?\s+(\d+)\s+ms", out)
                if m_mpsc:
                    data[alloc]["reader_ms"].append(float(m_mpsc.group(1)))
                    data[alloc]["avg_ms"].append(float(m_mpsc.group(2)))
                if m_glist:
                    data[alloc]["guarded_list_avg_ms"].append(float(m_glist.group(1)))

        for alloc in ["glibc", "jemalloc"]:
            for k in ["reader_ms", "avg_ms", "guarded_list_avg_ms"]:
                results["mpsc_queue"][alloc][k] = summarize_metrics(data[alloc][k])

        return results

    def run_json_suite(self, iterations: int = 1) -> Dict[str, Any]:
        """Runs JSON serialization and parsing benchmark (already internally tests 100M objects)."""
        actual_iterations = 1
        results = {"json_10M": {"glibc": {}, "jemalloc": {}}, "json_100M": {"glibc": {}, "jemalloc": {}}}
        data = {
            "json_10M": {"glibc": {"to_ms": [], "from_ms": []}, "jemalloc": {"to_ms": [], "from_ms": []}},
            "json_100M": {"glibc": {"to_ms": [], "from_ms": []}, "jemalloc": {"to_ms": [], "from_ms": []}}
        }
        args = ["json-string-benchmark"]
        for _ in range(actual_iterations):
            for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                out = self.run_command(bin_path, args)
                for line in out.splitlines():
                    m = re.match(r"^\s*(\d+)\s+(\d+)\s+(\d+)\s*:\s+([0-9.]+)\s+ms\s+([0-9.]+)\s+ms", line)
                    if m:
                        size, q, s, to_str, from_str = m.groups()
                        if size == "10000000" and q == "0":
                            data["json_10M"][alloc]["to_ms"].append(float(to_str))
                            data["json_10M"][alloc]["from_ms"].append(float(from_str))
                        elif size == "100000000" and q == "0":
                            data["json_100M"][alloc]["to_ms"].append(float(to_str))
                            data["json_100M"][alloc]["from_ms"].append(float(from_str))

        for target in ["json_10M", "json_100M"]:
            for alloc in ["glibc", "jemalloc"]:
                for k in ["to_ms", "from_ms"]:
                    results[target][alloc][k] = summarize_metrics(data[target][alloc][k])

        return results

    def run_conntrack_suite(self, iterations: int = 5) -> Dict[str, Any]:
        """Runs conntrack benchmark."""
        results = {"conntrack_8t": {"glibc": {}, "jemalloc": {}}}
        data = {"glibc": {"time_ms": []}, "jemalloc": {"time_ms": []}}
        args = ["test-conntrack", "benchmark", "8", "2000000", "32", "1"]
        for _ in range(iterations):
            for alloc, bin_path in [("glibc", self.glibc_bin), ("jemalloc", self.jemalloc_bin)]:
                out = self.run_command(bin_path, args)
                m = re.search(r"conntrack:\s+(\d+)\s+ms", out)
                if m:
                    data[alloc]["time_ms"].append(float(m.group(1)))

        for alloc in ["glibc", "jemalloc"]:
            results["conntrack_8t"][alloc]["time_ms"] = summarize_metrics(data[alloc]["time_ms"])

        return results

    def run_all(self, iterations: int = 5) -> Dict[str, Any]:
        print(f"--- Running OVS Microbenchmarks ({iterations} iterations per test) ---")
        out = {}
        print("  Running classifier suite...")
        out["classifier"] = self.run_classifier_suite(iterations)
        print("  Running cmap suite...")
        out["cmap"] = self.run_cmap_suite(iterations)
        print("  Running ccmap suite...")
        out["ccmap"] = self.run_ccmap_suite(iterations)
        print("  Running mpsc queue suite...")
        out["mpsc"] = self.run_mpsc_suite(iterations)
        print("  Running JSON suite...")
        out["json"] = self.run_json_suite(iterations)
        print("  Running conntrack suite...")
        out["conntrack"] = self.run_conntrack_suite(iterations)
        return out
