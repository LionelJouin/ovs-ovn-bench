import os
import re
import signal
import subprocess
import time
from typing import Dict, Any, Optional, List
import statistics

def get_process_memory(pid: int) -> Dict[str, float]:
    """Reads memory metrics in MiB from /proc/[pid]/status."""
    res = {"VmRSS": 0.0, "VmHWM": 0.0, "VmSize": 0.0}
    try:
        with open(f"/proc/{pid}/status") as f:
            for line in f:
                parts = line.split()
                if len(parts) >= 2:
                    key = parts[0].rstrip(":")
                    if key in res:
                        # Value is in kB
                        res[key] = float(parts[1]) / 1024.0
    except (FileNotFoundError, ProcessLookupError, PermissionError):
        pass
    return res

def parse_stopwatch_output(output: str) -> Dict[str, float]:
    """Parses ovn-appctl stopwatch/show output."""
    res = {
        "samples": 0.0,
        "max_ms": 0.0,
        "min_ms": 0.0,
        "p95_ms": 0.0,
        "short_avg_ms": 0.0,
        "long_avg_ms": 0.0
    }
    for line in output.splitlines():
        line = line.strip()
        if line.startswith("Total samples:"):
            res["samples"] = float(line.split(":")[1].strip().split()[0])
        elif line.startswith("Maximum:"):
            res["max_ms"] = float(line.split(":")[1].strip().split()[0])
        elif line.startswith("Minimum:"):
            res["min_ms"] = float(line.split(":")[1].strip().split()[0])
        elif line.startswith("95th percentile:"):
            res["p95_ms"] = float(line.split(":")[1].strip().split()[0])
        elif line.startswith("Short term average:"):
            res["short_avg_ms"] = float(line.split(":")[1].strip().split()[0])
        elif line.startswith("Long term average:"):
            res["long_avg_ms"] = float(line.split(":")[1].strip().split()[0])
    return res

def summarize_metrics(values: List[float]) -> Dict[str, float]:
    if not values:
        return {"mean": 0.0, "median": 0.0, "std": 0.0, "min": 0.0, "max": 0.0}
    if len(values) == 1:
        return {
            "mean": round(values[0], 2),
            "median": round(values[0], 2),
            "std": 0.0,
            "min": round(values[0], 2),
            "max": round(values[0], 2)
        }
    return {
        "mean": round(statistics.mean(values), 2),
        "median": round(statistics.median(values), 2),
        "std": round(statistics.stdev(values), 2),
        "min": round(min(values), 2),
        "max": round(max(values), 2)
    }

def terminate_process(proc: Optional[subprocess.Popen] = None, pid_file: Optional[str] = None):
    pid = None
    if pid_file and os.path.exists(pid_file):
        try:
            with open(pid_file) as f:
                pid = int(f.read().strip())
        except Exception:
            pass
    elif proc:
        pid = proc.pid

    if pid:
        try:
            os.kill(pid, signal.SIGTERM)
            for _ in range(50):
                time.sleep(0.05)
                try:
                    os.kill(pid, 0)
                except ProcessLookupError:
                    return
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        except Exception as e:
            print(f"Error terminating process {pid}: {e}")
