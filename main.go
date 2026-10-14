package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// ---------------------------------------------------------------------------
// OVN Northbound Database Models for libovsdb
// ---------------------------------------------------------------------------

type LogicalSwitch struct {
	UUID         string            `ovsdb:"_uuid"`
	Name         string            `ovsdb:"name"`
	Ports        []string          `ovsdb:"ports"`
	ACLs         []string          `ovsdb:"acls"`
	OtherConfig  map[string]string `ovsdb:"other_config"`
	LoadBalancer []string          `ovsdb:"load_balancer"`
}

type LogicalSwitchPort struct {
	UUID         string            `ovsdb:"_uuid"`
	Name         string            `ovsdb:"name"`
	Type         string            `ovsdb:"type"`
	Addresses    []string          `ovsdb:"addresses"`
	PortSecurity []string          `ovsdb:"port_security"`
	Options      map[string]string `ovsdb:"options"`
}

type ACL struct {
	UUID      string `ovsdb:"_uuid"`
	Action    string `ovsdb:"action"`
	Direction string `ovsdb:"direction"`
	Match     string `ovsdb:"match"`
	Priority  int    `ovsdb:"priority"`
}

type AddressSet struct {
	UUID      string   `ovsdb:"_uuid"`
	Name      string   `ovsdb:"name"`
	Addresses []string `ovsdb:"addresses"`
}

type PortGroup struct {
	UUID  string   `ovsdb:"_uuid"`
	Name  string   `ovsdb:"name"`
	Ports []string `ovsdb:"ports"`
	ACLs  []string `ovsdb:"acls"`
}

type LoadBalancer struct {
	UUID     string            `ovsdb:"_uuid"`
	Name     string            `ovsdb:"name"`
	Protocol *string           `ovsdb:"protocol"`
	Vips     map[string]string `ovsdb:"vips"`
}

type LogicalRouter struct {
	UUID  string   `ovsdb:"_uuid"`
	Name  string   `ovsdb:"name"`
	Ports []string `ovsdb:"ports"`
}

type LogicalRouterPort struct {
	UUID     string   `ovsdb:"_uuid"`
	Name     string   `ovsdb:"name"`
	MAC      string   `ovsdb:"mac"`
	Networks []string `ovsdb:"networks"`
}

type ProcMem struct {
	RssAnon float64 `json:"rss_anon_mib"`
	VmRSS   float64 `json:"vm_rss_mib"`
	VmHWM   float64 `json:"vm_hwm_mib"`
}

type ComponentMem struct {
	BaselineRSS float64 `json:"baseline_rss_mib"`
	PeakRSS     float64 `json:"peak_rss_mib"`
	PostDelRSS  float64 `json:"post_del_rss_mib"`
	RetainedRSS float64 `json:"retained_rss_mib"`
}

type BenchResult struct {
	BaselineRSS  float64                 `json:"baseline_rss_mib"`
	AddSec       float64                 `json:"add_sec"`
	AddRate      float64                 `json:"add_rate"`
	AddLoopMs    float64                 `json:"add_loop_ms"`
	AddLflowMs   float64                 `json:"add_lflow_ms"`
	PeakRSS      float64                 `json:"peak_rss_mib"`
	DelSec       float64                 `json:"del_sec"`
	DelLoopMs    float64                 `json:"del_loop_ms"`
	PostDelRSS   float64                 `json:"post_del_rss_mib"`
	RetainedRSS  float64                 `json:"retained_rss_mib"`
	Components   map[string]ComponentMem `json:"components,omitempty"`
	ThreadLoops  map[string]float64      `json:"thread_loops,omitempty"`
	ThreadLflows map[string]float64      `json:"thread_lflows,omitempty"`
}

func getDBModel() (model.ClientDBModel, error) {
	return model.NewClientDBModel("OVN_Northbound", map[string]model.Model{
		"Logical_Switch":      &LogicalSwitch{},
		"Logical_Switch_Port": &LogicalSwitchPort{},
		"ACL":                 &ACL{},
		"Address_Set":         &AddressSet{},
		"Port_Group":          &PortGroup{},
		"Load_Balancer":       &LoadBalancer{},
		"Logical_Router":      &LogicalRouter{},
		"Logical_Router_Port": &LogicalRouterPort{},
	})
}

// ---------------------------------------------------------------------------
// Path & Binary Resolvers
// ---------------------------------------------------------------------------

func findBinary(name, preferDir string) (string, error) {
	if preferDir != "" {
		subs := []string{"", "vswitchd", "ovsdb", "utilities", "northd", "controller", "tests"}
		for _, s := range subs {
			p := filepath.Join(preferDir, s, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/usr/local/sbin", "/usr/local/bin"} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("binary %q not found", name)
}

func findSchema(name string) (string, error) {
	searchPaths := []string{
		"/usr/share/ovn/" + name,
		"/usr/local/share/ovn/" + name,
		"/usr/share/openvswitch/" + name,
		"/usr/local/share/openvswitch/" + name,
		filepath.Join(".", name),
		filepath.Join("..", name),
	}
	if envPath := os.Getenv("OVN_SCHEMA_DIR"); envPath != "" {
		searchPaths = append([]string{filepath.Join(envPath, name)}, searchPaths...)
	}
	if envPath := os.Getenv("OVS_SCHEMA_DIR"); envPath != "" {
		searchPaths = append([]string{filepath.Join(envPath, name)}, searchPaths...)
	}
	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Also check adjacent checkout directories if running in a multi-repo workspace
	candidateGlobs := []string{
		filepath.Join("..", "*", name),
		filepath.Join("..", "*", "*", name),
		filepath.Join("..", "..", "*", name),
		filepath.Join("..", "..", "*", "*", name),
		filepath.Join("..", "..", "*", "*", "*", name),
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		candidateGlobs = append(candidateGlobs,
			filepath.Join(homeDir, "workspaces", "*", name),
			filepath.Join(homeDir, "workspaces", "*", "*", name),
			filepath.Join(homeDir, "workspaces", "*", "*", "*", name),
		)
	}
	for _, pattern := range candidateGlobs {
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("schema %q not found", name)
}

func findJemallocLib() string {
	candidates := []string{
		"/usr/lib64/libjemalloc.so.2",
		"/usr/lib/x86_64-linux-gnu/libjemalloc.so.2",
		"/usr/lib/aarch64-linux-gnu/libjemalloc.so.2",
		"/usr/lib/libjemalloc.so.2",
		"/usr/local/lib/libjemalloc.so.2",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	out, err := exec.Command("sh", "-c", "ldconfig -p 2>/dev/null | grep libjemalloc.so.2 | head -1").Output()
	if err == nil && strings.Contains(string(out), "=>") {
		parts := strings.Split(string(out), "=>")
		if len(parts) >= 2 {
			p := strings.TrimSpace(parts[1])
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func getProcMem(pid int) ProcMem {
	res := ProcMem{}
	if pid <= 0 {
		return res
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return res
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			k := strings.TrimSuffix(fields[0], ":")
			v, err := strconv.ParseFloat(fields[1], 64)
			if err == nil {
				switch k {
				case "RssAnon":
					res.RssAnon = math.Round((v/1024.0)*100) / 100
				case "VmRSS":
					res.VmRSS = math.Round((v/1024.0)*100) / 100
				case "VmHWM":
					res.VmHWM = math.Round((v/1024.0)*100) / 100
				}
			}
		}
	}
	return res
}

func parseStopwatch(out string) float64 {
	// First preference: Maximum (captures actual duration of the single recompute loop after reset)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Maximum:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				f := strings.Fields(parts[1])
				if len(f) > 0 {
					if v, err := strconv.ParseFloat(f[0], 64); err == nil && v > 0 {
						return round2(v)
					}
				}
			}
		}
	}
	// Second preference: 95th percentile
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "95th percentile:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				f := strings.Fields(parts[1])
				if len(f) > 0 {
					if v, err := strconv.ParseFloat(f[0], 64); err == nil && v > 0 {
						return round2(v)
					}
				}
			}
		}
	}
	// Fallback: Short term average or Average
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Short term average:") || strings.Contains(line, "Average:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				f := strings.Fields(parts[1])
				if len(f) > 0 {
					if v, err := strconv.ParseFloat(f[0], 64); err == nil {
						return round2(v)
					}
				}
			}
		}
	}
	return 0.0
}

func killPidFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

func waitForPidFile(path string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil && pid > 0 {
				if err := syscall.Kill(pid, 0); err == nil {
					return pid, nil
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, fmt.Errorf("timeout waiting for pidfile %q", path)
}

// ---------------------------------------------------------------------------
// OVN Environment & libovsdb Client Management
// ---------------------------------------------------------------------------

const (
	defaultJemallocConf = "background_thread:false,dirty_decay_ms:0,muzzy_decay_ms:0,thp:never"
	decayWaitTime       = 5 * time.Second
)

var (
	activeEnvsMu sync.Mutex
	activeEnvs   = make(map[*OVNEnvironment]bool)
)

type OVNEnvironment struct {
	alloc         string
	jemallocConf  string
	ovsDir        string
	ovnDir        string
	testDir       string
	nThreads      int
	trimTimeoutMs int
	nbSock        string
	sbSock        string
	ovsSock       string
	northdPid     int
	controllerPid int
	vswitchdPid   int
	nbServerPid   int
	sbServerPid   int
	ovsServerPid  int
	ovsdbTool     string
	ovsdbServer   string
	ovsVsctl      string
	ovsOfctl      string
	ovsVswitchd   string
	ovnNbctl      string
	ovnSbctl      string
	ovnNorthd     string
	ovnController string
	ovnAppctl     string
	ovsdbClient   client.Client
}

func NewOVNEnvironment(alloc, jemallocConf, ovsDir, ovnDir, testDir string, nThreads, trimTimeoutMs int) (*OVNEnvironment, error) {
	tool, err := findBinary("ovsdb-tool", ovsDir)
	if err != nil {
		return nil, err
	}
	server, err := findBinary("ovsdb-server", ovsDir)
	if err != nil {
		return nil, err
	}
	vsctl, err := findBinary("ovs-vsctl", ovsDir)
	if err != nil {
		return nil, err
	}
	ofctl, err := findBinary("ovs-ofctl", ovsDir)
	if err != nil {
		return nil, err
	}
	vswitchd, err := findBinary("ovs-vswitchd", ovsDir)
	if err != nil {
		return nil, err
	}
	nbctl, err := findBinary("ovn-nbctl", ovnDir)
	if err != nil {
		return nil, err
	}
	sbctl, err := findBinary("ovn-sbctl", ovnDir)
	if err != nil {
		return nil, err
	}
	northd, err := findBinary("ovn-northd", ovnDir)
	if err != nil {
		return nil, err
	}
	controller, err := findBinary("ovn-controller", ovnDir)
	if err != nil {
		return nil, err
	}
	appctl, err := findBinary("ovn-appctl", ovnDir)
	if err != nil {
		appctl, _ = findBinary("ovs-appctl", ovsDir)
	}

	return &OVNEnvironment{
		alloc:         alloc,
		jemallocConf:  jemallocConf,
		ovsDir:        ovsDir,
		ovnDir:        ovnDir,
		testDir:       testDir,
		nThreads:      nThreads,
		trimTimeoutMs: trimTimeoutMs,
		nbSock:        "unix:" + filepath.Join(testDir, "nb.sock"),
		sbSock:        "unix:" + filepath.Join(testDir, "sb.sock"),
		ovsSock:       "unix:" + filepath.Join(testDir, "ovs.sock"),
		ovsdbTool:     tool,
		ovsdbServer:   server,
		ovsVsctl:      vsctl,
		ovsOfctl:      ofctl,
		ovsVswitchd:   vswitchd,
		ovnNbctl:      nbctl,
		ovnSbctl:      sbctl,
		ovnNorthd:     northd,
		ovnController: controller,
		ovnAppctl:     appctl,
	}, nil
}

func verifyJemallocLoaded(pid int) error {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return fmt.Errorf("failed to read /proc/%d/maps: %w", pid, err)
	}
	if !strings.Contains(string(data), "jemalloc") {
		return fmt.Errorf("process %d does not have libjemalloc loaded in its memory map", pid)
	}
	return nil
}

func verifyJemallocNotLoaded(pid int) error {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return fmt.Errorf("failed to read /proc/%d/maps: %w", pid, err)
	}
	if strings.Contains(string(data), "jemalloc") {
		return fmt.Errorf("process %d unexpectedly has libjemalloc loaded during glibc benchmark", pid)
	}
	return nil
}

func (e *OVNEnvironment) Start() error {
	_ = os.RemoveAll(e.testDir)
	if err := os.MkdirAll(e.testDir, 0755); err != nil {
		return err
	}

	nbSchema, err := findSchema("ovn-nb.ovsschema")
	if err != nil {
		return err
	}
	sbSchema, err := findSchema("ovn-sb.ovsschema")
	if err != nil {
		return err
	}
	ovsSchema, err := findSchema("vswitch.ovsschema")
	if err != nil {
		return err
	}

	_ = exec.Command(e.ovsdbTool, "create", filepath.Join(e.testDir, "nb.db"), nbSchema).Run()
	_ = exec.Command(e.ovsdbTool, "create", filepath.Join(e.testDir, "sb.db"), sbSchema).Run()
	_ = exec.Command(e.ovsdbTool, "create", filepath.Join(e.testDir, "ovs.db"), ovsSchema).Run()

	os.Setenv("OVS_RUNDIR", e.testDir)
	os.Setenv("OVS_LOGDIR", e.testDir)
	os.Setenv("OVN_RUNDIR", e.testDir)
	os.Setenv("OVN_LOGDIR", e.testDir)
	envVars := os.Environ()
	if e.alloc == "jemalloc" {
		jlib := findJemallocLib()
		if jlib == "" {
			return fmt.Errorf("allocator 'jemalloc' requested, but libjemalloc.so.2 was not found on system or LD library paths")
		}
		conf := e.jemallocConf
		if conf == "" {
			conf = defaultJemallocConf
		}
		if !strings.Contains(conf, "narenas") {
			// Automatically scale narenas to the active thread count to prevent jemalloc from
			// querying the host hardware topology and allocating 512+ arenas on cloud VMs (e.g. 64-core AMD EPYC)
			narenas := e.nThreads
			if narenas < 2 {
				narenas = 2
			}
			conf = fmt.Sprintf("%s,narenas:%d", conf, narenas)
		}
		if !strings.Contains(conf, "thp") {
			// Forbid Transparent Huge Pages (THP) so cloud runners (e.g. Azure with THP=[always])
			// do not promote 4 KiB pages into 2 MiB physical allocations
			conf = fmt.Sprintf("%s,thp:never", conf)
		}
		envVars = append(envVars, "LD_PRELOAD="+jlib, "MALLOC_CONF="+conf)
	}

	cmdNb := exec.Command(e.ovsdbServer, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "nb.pid"),
		"--unixctl="+filepath.Join(e.testDir, "nb.ctl"),
		"--remote=punix:"+filepath.Join(e.testDir, "nb.sock"),
		filepath.Join(e.testDir, "nb.db"))
	cmdNb.Env = envVars
	if err := cmdNb.Run(); err != nil {
		return err
	}

	cmdSb := exec.Command(e.ovsdbServer, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "sb.pid"),
		"--unixctl="+filepath.Join(e.testDir, "sb.ctl"),
		"--remote=punix:"+filepath.Join(e.testDir, "sb.sock"),
		filepath.Join(e.testDir, "sb.db"))
	cmdSb.Env = envVars
	if err := cmdSb.Run(); err != nil {
		return err
	}

	cmdOvs := exec.Command(e.ovsdbServer, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "ovs.pid"),
		"--unixctl="+filepath.Join(e.testDir, "ovs.ctl"),
		"--remote=punix:"+filepath.Join(e.testDir, "ovs.sock"),
		filepath.Join(e.testDir, "ovs.db"))
	cmdOvs.Env = envVars
	if err := cmdOvs.Run(); err != nil {
		return err
	}

	time.Sleep(100 * time.Millisecond)

	// Enable memory trimming on compaction across all 3 OVSDB servers
	for _, ctl := range []string{"nb.ctl", "sb.ctl", "ovs.ctl"} {
		ctlPath := filepath.Join(e.testDir, ctl)
		_ = exec.Command(e.ovnAppctl, "-t", ctlPath, "ovsdb-server/memory-trim-on-compaction", "on").Run()
	}

	_ = exec.Command(e.ovnNbctl, "--db="+e.nbSock, "init").Run()
	_ = exec.Command(e.ovsVsctl, "--db="+e.ovsSock, "--no-wait", "init").Run()

	if e.trimTimeoutMs > 0 {
		_ = exec.Command(e.ovnNbctl, "--db="+e.nbSock, "set", "NB_Global", ".",
			fmt.Sprintf("options:northd_trim_timeout=%d", e.trimTimeoutMs)).Run()
		_ = exec.Command(e.ovsVsctl, "--db="+e.ovsSock, "--no-wait", "set", "Open_vSwitch", ".",
			fmt.Sprintf("external_ids:ovn-trim-timeout-ms=%d", e.trimTimeoutMs),
			"external_ids:ovn-trim-limit-lflow-cache=1",
			"external_ids:ovn-trim-wmark-perc-lflow-cache=50").Run()
	}

	_ = exec.Command(e.ovsVsctl, "--db="+e.ovsSock, "--no-wait",
		"set", "Open_vSwitch", ".",
		"external_ids:system-id=chassis-1",
		"external_ids:ovn-remote="+e.sbSock,
		"external_ids:ovn-encap-type=geneve",
		"external_ids:ovn-encap-ip=127.0.0.1",
		"external_ids:ovn-bridge=br-int",
	).Run()

	_ = exec.Command(e.ovsVsctl, "--db="+e.ovsSock, "--no-wait",
		"add-br", "br-int", "--",
		"set", "bridge", "br-int", "fail-mode=secure", "datapath_type=dummy").Run()

	cmdVswitchd := exec.Command(e.ovsVswitchd, "--enable-dummy=override", "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "vswitchd.pid"),
		"--unixctl="+filepath.Join(e.testDir, "vswitchd.ctl"),
		e.ovsSock)
	cmdVswitchd.Env = envVars
	if err := cmdVswitchd.Run(); err != nil {
		return err
	}

	cmdNorthd := exec.Command(e.ovnNorthd, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "northd.pid"),
		"--unixctl="+filepath.Join(e.testDir, "northd.ctl"),
		"--ovnnb-db="+e.nbSock, "--ovnsb-db="+e.sbSock,
		fmt.Sprintf("--n-threads=%d", e.nThreads),
		"-vfile:off", "-vconsole:off")
	cmdNorthd.Env = envVars
	if err := cmdNorthd.Run(); err != nil {
		return err
	}

	_ = os.MkdirAll("/var/run/openvswitch", 0755)
	_ = os.MkdirAll("/var/run/ovn", 0755)
	_ = os.MkdirAll("/var/log/openvswitch", 0755)
	_ = os.MkdirAll("/var/log/ovn", 0755)

	cmdController := exec.Command(e.ovnController, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "controller.pid"),
		"-vfile:off", "-vconsole:off",
		e.ovsSock)
	cmdController.Env = envVars
	if err := cmdController.Run(); err != nil {
		return err
	}

	var errPid error
	if e.northdPid, errPid = waitForPidFile(filepath.Join(e.testDir, "northd.pid"), 5*time.Second); errPid != nil {
		return errPid
	}
	if e.controllerPid, errPid = waitForPidFile(filepath.Join(e.testDir, "controller.pid"), 5*time.Second); errPid != nil {
		return errPid
	}
	if e.vswitchdPid, errPid = waitForPidFile(filepath.Join(e.testDir, "vswitchd.pid"), 5*time.Second); errPid != nil {
		return errPid
	}
	if e.nbServerPid, errPid = waitForPidFile(filepath.Join(e.testDir, "nb.pid"), 5*time.Second); errPid != nil {
		return errPid
	}
	if e.sbServerPid, errPid = waitForPidFile(filepath.Join(e.testDir, "sb.pid"), 5*time.Second); errPid != nil {
		return errPid
	}
	if e.ovsServerPid, errPid = waitForPidFile(filepath.Join(e.testDir, "ovs.pid"), 5*time.Second); errPid != nil {
		return errPid
	}

	pids := map[string]int{
		"ovn-northd":     e.northdPid,
		"ovn-controller": e.controllerPid,
		"ovs-vswitchd":   e.vswitchdPid,
		"ovsdb-nb":       e.nbServerPid,
		"ovsdb-sb":       e.sbServerPid,
		"ovsdb-ovs":      e.ovsServerPid,
	}
	for name, pid := range pids {
		if e.alloc == "jemalloc" {
			if err := verifyJemallocLoaded(pid); err != nil {
				return fmt.Errorf("%s (pid %d): %w", name, pid, err)
			}
		} else {
			if err := verifyJemallocNotLoaded(pid); err != nil {
				return fmt.Errorf("%s (pid %d): %w", name, pid, err)
			}
		}
	}

	// Initialize libovsdb native client to OVN Northbound DB
	dbModel, err := getDBModel()
	if err != nil {
		return err
	}
	ovsdbCli, err := client.NewOVSDBClient(dbModel, client.WithEndpoint(e.nbSock))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ovsdbCli.Connect(ctx); err != nil {
		return err
	}
	e.ovsdbClient = ovsdbCli

	activeEnvsMu.Lock()
	activeEnvs[e] = true
	activeEnvsMu.Unlock()

	// Perform a steady-state warmup cycle to initialize caches and dynamic linker symbols
	e.warmup()

	return nil
}

func (e *OVNEnvironment) warmup() {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "warmup-ls"}
	opsSw, _ := e.ovsdbClient.Create(sw)
	resSw, _ := e.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	port := &LogicalSwitchPort{UUID: "warmup_p", Name: "warmup-p"}
	opsP, _ := e.ovsdbClient.Create(port)
	mutateOps, _ := e.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
		Field:   &sw.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{"warmup_p"},
	})
	_, _ = e.Transact(ctx, append(opsP, mutateOps...)...)
	e.Sync()
	e.Recompute()

	// Clean up warmup objects
	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "warmup-ls")},
		Row: map[string]interface{}{
			"ports": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delPortOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "warmup-p")},
	}
	delSwOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "warmup-ls")},
	}
	_, _ = e.Transact(ctx, clearSwOp, delPortOp, delSwOp)
	e.Sync()
	e.Recompute()
	time.Sleep(500 * time.Millisecond)
}

type PodMemSnapshot struct {
	TotalRSS  float64
	TotalPeak float64
	RSS       map[string]float64
	Peak      map[string]float64
}

func (e *OVNEnvironment) sampleMemory() PodMemSnapshot {
	pids := map[string]int{
		"ovn-northd":     e.northdPid,
		"ovn-controller": e.controllerPid,
		"ovs-vswitchd":   e.vswitchdPid,
		"ovsdb-nb":       e.nbServerPid,
		"ovsdb-sb":       e.sbServerPid,
		"ovsdb-ovs":      e.ovsServerPid,
	}
	snap := PodMemSnapshot{
		RSS:  make(map[string]float64),
		Peak: make(map[string]float64),
	}
	for name, pid := range pids {
		mem := getProcMem(pid)
		// RssAnon isolates private heap allocations (preventing 6x multi-counting of shared libraries).
		// VmHWM captures the kernel-recorded high-water mark of resident memory.
		// Subtract constant file-backed / shared library pages (VmRSS - RssAnon) from VmHWM to derive
		// the true kernel-tracked peak private heap allocation.
		sharedFileRSS := math.Max(0, mem.VmRSS-mem.RssAnon)
		peakAnon := round2(math.Max(mem.RssAnon, mem.VmHWM-sharedFileRSS))
		snap.RSS[name] = mem.RssAnon
		snap.Peak[name] = peakAnon
		snap.TotalRSS += mem.RssAnon
		snap.TotalPeak += peakAnon
	}
	snap.TotalRSS = round2(snap.TotalRSS)
	snap.TotalPeak = round2(snap.TotalPeak)
	return snap
}

func buildBenchResult(base, peak, post PodMemSnapshot, addSec, addRate, addLoop, addLflow, delSec, delLoop float64) BenchResult {
	totalPeak := math.Max(peak.TotalPeak, math.Max(post.TotalRSS, base.TotalRSS))
	res := BenchResult{
		BaselineRSS: round2(base.TotalRSS),
		PeakRSS:     round2(totalPeak),
		PostDelRSS:  round2(post.TotalRSS),
		RetainedRSS: round2(post.TotalRSS - base.TotalRSS),
		AddSec:      round3(addSec),
		AddRate:     round1(addRate),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		Components:  make(map[string]ComponentMem),
	}
	for name := range base.RSS {
		compPeak := math.Max(peak.Peak[name], math.Max(post.RSS[name], base.RSS[name]))
		res.Components[name] = ComponentMem{
			BaselineRSS: round2(base.RSS[name]),
			PeakRSS:     round2(compPeak),
			PostDelRSS:  round2(post.RSS[name]),
			RetainedRSS: round2(post.RSS[name] - base.RSS[name]),
		}
	}
	return res
}

func (e *OVNEnvironment) Transact(ctx context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	res, err := e.ovsdbClient.Transact(ctx, ops...)
	if err != nil {
		fmt.Printf("  [libovsdb] Transact error: %v\n", err)
	}
	for i, r := range res {
		if r.Error != "" {
			fmt.Printf("  [libovsdb] Op %d failed: %s (%s)\n", i, r.Error, r.Details)
		}
	}
	return res, err
}

func (e *OVNEnvironment) decayWaitTime() time.Duration {
	wait := decayWaitTime
	if e.trimTimeoutMs > 0 {
		req := time.Duration(e.trimTimeoutMs)*time.Millisecond + 1500*time.Millisecond
		if req > wait {
			wait = req
		}
	}
	return wait
}

func (e *OVNEnvironment) Sync() {
	_ = exec.Command(e.ovnNbctl, "--db="+e.nbSock, "--wait=hv", "sync").Run()
}

func (e *OVNEnvironment) TrimMemory() {
	// Trigger database compaction and malloc_trim(0) on all 3 OVSDB servers
	for _, ctl := range []string{"nb.ctl", "sb.ctl", "ovs.ctl"} {
		ctlPath := filepath.Join(e.testDir, ctl)
		_ = exec.Command(e.ovnAppctl, "-t", ctlPath, "ovsdb-server/compact").Run()
	}
	// Purge datapath flow cache on ovs-vswitchd
	vswCtl := filepath.Join(e.testDir, "vswitchd.ctl")
	_ = exec.Command(e.ovnAppctl, "-t", vswCtl, "revalidator/purge").Run()
	// Flush logical flow cache on ovn-controller
	ctrlCtl := filepath.Join(e.testDir, fmt.Sprintf("ovn-controller.%d.ctl", e.controllerPid))
	_ = exec.Command(e.ovnAppctl, "-t", ctrlCtl, "lflow-cache/flush").Run()
}

func (e *OVNEnvironment) Recompute() (wallMs, loopMs, lflowMs float64) {
	_ = exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "stopwatch/reset").Run()
	t0 := time.Now()
	_ = exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "inc-engine/recompute").Run()
	e.Sync()
	wallMs = float64(time.Since(t0).Microseconds()) / 1000.0

	outLoop, _ := exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "stopwatch/show", "ovn-northd-loop").CombinedOutput()
	loopMs = parseStopwatch(string(outLoop))

	outLflow, err := exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "stopwatch/show", "lflow").CombinedOutput()
	if err != nil || len(outLflow) == 0 {
		outLflow, _ = exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "stopwatch/show", "build_lflows").CombinedOutput()
	}
	lflowMs = parseStopwatch(string(outLflow))

	return wallMs, loopMs, lflowMs
}

func (e *OVNEnvironment) Stop() {
	activeEnvsMu.Lock()
	delete(activeEnvs, e)
	activeEnvsMu.Unlock()

	if e.ovsdbClient != nil {
		e.ovsdbClient.Close()
	}
	killPidFile(filepath.Join(e.testDir, "controller.pid"))
	killPidFile(filepath.Join(e.testDir, "northd.pid"))
	killPidFile(filepath.Join(e.testDir, "vswitchd.pid"))
	killPidFile(filepath.Join(e.testDir, "ovs.pid"))
	killPidFile(filepath.Join(e.testDir, "sb.pid"))
	killPidFile(filepath.Join(e.testDir, "nb.pid"))
	_ = os.RemoveAll(e.testDir)
}

func (e *OVNEnvironment) bindChassisVIFs(portNames []string) {
	if len(portNames) == 0 {
		return
	}
	chunkSize := 1000
	for i := 0; i < len(portNames); i += chunkSize {
		end := i + chunkSize
		if end > len(portNames) {
			end = len(portNames)
		}
		var args []string
		args = append(args, "--db="+e.ovsSock, "--no-wait")
		for j := i; j < end; j++ {
			pName := portNames[j]
			vifName := fmt.Sprintf("vif-%d", j)
			args = append(args, "--", "--if-exists", "del-port", "br-int", vifName,
				"--", "add-port", "br-int", vifName,
				"--", "set", "Interface", vifName, "type=dummy", "external_ids:iface-id="+pName)
		}
		_ = exec.Command(e.ovsVsctl, args...).Run()
	}
}

func (e *OVNEnvironment) unbindChassisVIFs(count int) {
	if count <= 0 {
		return
	}
	chunkSize := 1000
	for i := 0; i < count; i += chunkSize {
		end := i + chunkSize
		if end > count {
			end = count
		}
		var args []string
		args = append(args, "--db="+e.ovsSock, "--no-wait")
		for j := i; j < end; j++ {
			vifName := fmt.Sprintf("vif-%d", j)
			args = append(args, "--", "--if-exists", "del-port", "br-int", vifName)
		}
		_ = exec.Command(e.ovsVsctl, args...).Run()
	}
}

func setupBaselineSwitch(env *OVNEnvironment, swName string, nPorts int) (*LogicalSwitch, error) {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: swName}
	opsSw, err := env.ovsdbClient.Create(sw)
	if err != nil {
		return nil, err
	}
	resSw, err := env.Transact(ctx, opsSw...)
	if err != nil || len(resSw) == 0 {
		return nil, fmt.Errorf("failed to create logical switch %s: %v", swName, err)
	}
	sw.UUID = resSw[0].UUID.GoUUID

	if nPorts > 0 {
		ports := make([]model.Model, nPorts)
		namedUUIDs := make([]string, nPorts)
		var pNames []string
		for j := 0; j < nPorts; j++ {
			pName := fmt.Sprintf("%s-p-%d", swName, j)
			pNames = append(pNames, pName)
			namedUUIDs[j] = fmt.Sprintf("base_p_%d", j)
			pIdx := j + 2
			hi := (pIdx >> 8) & 255
			lo := pIdx & 255
			mac := fmt.Sprintf("00:00:01:00:%02x:%02x", hi, lo)
			ip := fmt.Sprintf("10.1.%d.%d", hi, lo)
			ports[j] = &LogicalSwitchPort{
				UUID:         namedUUIDs[j],
				Name:         pName,
				Addresses:    []string{mac + " " + ip},
				PortSecurity: []string{mac + " " + ip},
			}
		}
		opsPorts, _ := env.ovsdbClient.Create(ports...)
		mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
			Field:   &sw.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   namedUUIDs,
		})
		_, _ = env.Transact(ctx, append(opsPorts, mutateOps...)...)
		env.bindChassisVIFs(pNames)
	}

	env.Sync()
	env.Recompute()
	return sw, nil
}

// ---------------------------------------------------------------------------
// Native libovsdb Benchmark Implementations
// ---------------------------------------------------------------------------

func benchPort(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := env.sampleMemory()

	// Phase 2: Add N ports and attach to switch via libovsdb
	t0 := time.Now()
	chunkSize := 300
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		chunkLen := end - i
		ports := make([]model.Model, chunkLen)
		namedUUIDs := make([]string, chunkLen)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			namedUUID := fmt.Sprintf("port_%d", idx)
			namedUUIDs[j] = namedUUID
			pIdx := idx + 2
			hi := (pIdx >> 8) & 255
			lo := pIdx & 255
			mac := fmt.Sprintf("00:00:0a:01:%02x:%02x", hi, lo)
			ip := fmt.Sprintf("10.1.%d.%d", hi, lo)
			ports[j] = &LogicalSwitchPort{
				UUID:         namedUUID,
				Name:         fmt.Sprintf("p-%d", idx),
				Addresses:    []string{mac + " " + ip},
				PortSecurity: []string{mac + " " + ip},
			}
		}
		opsPorts, _ := env.ovsdbClient.Create(ports...)
		mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
			Field:   &sw.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   namedUUIDs,
		})
		_, _ = env.Transact(ctx, append(opsPorts, mutateOps...)...)
	}

	// Bind representative VIFs in OVS so ovn-controller and ovs-vswitchd actively process the claimed ports
	var vifNames []string
	for j := 0; j < n; j++ {
		vifNames = append(vifNames, fmt.Sprintf("p-%d", j))
	}
	env.bindChassisVIFs(vifNames)
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove N ports from switch and delete rows
	t1 := time.Now()
	env.unbindChassisVIFs(n)

	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "bench-ls")},
		Row: map[string]interface{}{
			"ports": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delPortOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	_, _ = env.Transact(ctx, clearSwOp, delPortOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

func benchACL(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw, err := setupBaselineSwitch(env, "bench-ls", 10)
	if err != nil {
		fmt.Printf("setupBaselineSwitch error: %v\n", err)
	}
	baseline := env.sampleMemory()

	// Phase 2: Add N ACLs and attach to switch via libovsdb
	t0 := time.Now()
	chunkSize := 300
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		chunkLen := end - i
		acls := make([]model.Model, chunkLen)
		namedUUIDs := make([]string, chunkLen)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			namedUUID := fmt.Sprintf("acl_%d", idx)
			namedUUIDs[j] = namedUUID
			match := fmt.Sprintf("ip4.src == 10.%d.%d.1 && tcp.dst == %d", (idx>>8)&255, idx&255, 1000+(idx%1000))
			acls[j] = &ACL{
				UUID:      namedUUID,
				Action:    "allow-related",
				Direction: "to-lport",
				Priority:  1000 + (idx % 500),
				Match:     match,
			}
		}
		opsAcl, _ := env.ovsdbClient.Create(acls...)
		mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
			Field:   &sw.ACLs,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   namedUUIDs,
		})
		_, _ = env.Transact(ctx, append(opsAcl, mutateOps...)...)
	}
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove N ACLs from switch and delete rows
	t1 := time.Now()
	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "bench-ls")},
		Row: map[string]interface{}{
			"acls": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delAclOp := ovsdb.Operation{
		Op:    "delete",
		Table: "ACL",
		Where: []ovsdb.Condition{ovsdb.NewCondition("priority", ovsdb.ConditionGreaterThanOrEqual, 0)},
	}
	_, _ = env.Transact(ctx, clearSwOp, delAclOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

func benchAddressSet(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw, err := setupBaselineSwitch(env, "bench-ls", 10)
	if err != nil {
		fmt.Printf("setupBaselineSwitch error: %v\n", err)
	}
	baseline := env.sampleMemory()

	// Phase 2: Create Address Set with N unique IPs via libovsdb
	t0 := time.Now()
	ips := make([]string, n)
	for i := 0; i < n; i++ {
		ips[i] = fmt.Sprintf("10.%d.%d.%d", (i/62500)&255, ((i/250)%250)+1, (i%250)+1)
	}
	as := &AddressSet{
		Name:      "bench_as",
		Addresses: ips,
	}
	acl := &ACL{
		UUID:      "acl_as",
		Action:    "allow-related",
		Direction: "to-lport",
		Priority:  1000,
		Match:     "ip4.src == $bench_as",
	}

	opsAs, _ := env.ovsdbClient.Create(as)
	opsAcl, _ := env.ovsdbClient.Create(acl)
	mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
		Field:   &sw.ACLs,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{"acl_as"},
	})
	allOps := append(opsAs, opsAcl...)
	allOps = append(allOps, mutateOps...)
	_, _ = env.Transact(ctx, allOps...)
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove Address Set & ACL
	t1 := time.Now()
	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "bench-ls")},
		Row: map[string]interface{}{
			"acls": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delAsOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Address_Set",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	delAclOp := ovsdb.Operation{
		Op:    "delete",
		Table: "ACL",
		Where: []ovsdb.Condition{ovsdb.NewCondition("priority", ovsdb.ConditionGreaterThanOrEqual, 0)},
	}
	_, _ = env.Transact(ctx, clearSwOp, delAsOp, delAclOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

func benchPortGroup(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := env.sampleMemory()

	// Phase 2: Create N ports on switch, create Port_Group, and attach ACL via libovsdb
	t0 := time.Now()
	var allPortUUIDs []string
	chunkSize := 300
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		chunkLen := end - i
		ports := make([]model.Model, chunkLen)
		namedUUIDs := make([]string, chunkLen)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			namedUUID := fmt.Sprintf("p_%d", idx)
			namedUUIDs[j] = namedUUID
			pIdx := idx + 2
			hi := (pIdx >> 8) & 255
			lo := pIdx & 255
			mac := fmt.Sprintf("00:00:0a:01:%02x:%02x", hi, lo)
			ip := fmt.Sprintf("10.1.%d.%d", hi, lo)
			ports[j] = &LogicalSwitchPort{
				UUID:         namedUUID,
				Name:         fmt.Sprintf("p-%d", idx),
				Addresses:    []string{mac + " " + ip},
				PortSecurity: []string{mac + " " + ip},
			}
		}
		opsPorts, _ := env.ovsdbClient.Create(ports...)
		mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
			Field:   &sw.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   namedUUIDs,
		})
		res, _ := env.Transact(ctx, append(opsPorts, mutateOps...)...)
		for j := 0; j < chunkLen; j++ {
			if j < len(res) && res[j].UUID.GoUUID != "" {
				allPortUUIDs = append(allPortUUIDs, res[j].UUID.GoUUID)
			}
		}
	}

	// Bind representative VIFs in OVS
	var vifNames []string
	for j := 0; j < n; j++ {
		vifNames = append(vifNames, fmt.Sprintf("p-%d", j))
	}
	env.bindChassisVIFs(vifNames)

	acl := &ACL{
		UUID:      "acl_pg",
		Action:    "allow-related",
		Direction: "to-lport",
		Priority:  1000,
		Match:     "outport == @bench_pg",
	}
	pg := &PortGroup{
		Name:  "bench_pg",
		Ports: allPortUUIDs,
		ACLs:  []string{"acl_pg"},
	}
	opsPg, _ := env.ovsdbClient.Create(pg)
	opsAcl, _ := env.ovsdbClient.Create(acl)
	_, _ = env.Transact(ctx, append(opsPg, opsAcl...)...)
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove Port_Group & ACL & Ports
	t1 := time.Now()
	env.unbindChassisVIFs(n)

	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "bench-ls")},
		Row: map[string]interface{}{
			"ports": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delPgOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Port_Group",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	delAclOp := ovsdb.Operation{
		Op:    "delete",
		Table: "ACL",
		Where: []ovsdb.Condition{ovsdb.NewCondition("priority", ovsdb.ConditionGreaterThanOrEqual, 0)},
	}
	delPortOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	_, _ = env.Transact(ctx, clearSwOp, delPgOp, delAclOp, delPortOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

func benchLoadBalancer(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw, err := setupBaselineSwitch(env, "bench-ls", 10)
	if err != nil {
		fmt.Printf("setupBaselineSwitch error: %v\n", err)
	}

	proto := "tcp"
	lb := &LoadBalancer{
		UUID:     "lb_cluster",
		Name:     "k8s-cluster-lb-tcp",
		Protocol: &proto,
		Vips:     make(map[string]string),
	}
	opsLb, _ := env.ovsdbClient.Create(lb)
	mutateOpsSw, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
		Field:   &sw.LoadBalancer,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{"lb_cluster"},
	})
	resLb, err := env.Transact(ctx, append(opsLb, mutateOpsSw...)...)
	if err != nil {
		fmt.Printf("benchLoadBalancer create error: %v\n", err)
	}
	if len(resLb) > 0 && resLb[0].UUID.GoUUID != "" {
		lb.UUID = resLb[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := env.sampleMemory()

	// Phase 2: Add N VIPs into the shared cluster Load Balancer (matching OVN-Kube architecture)
	t0 := time.Now()
	chunkSize := 300
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		chunkVips := make(map[string]string)
		for j := i; j < end; j++ {
			vip := fmt.Sprintf("10.96.%d.%d:80", (j/250)&255, (j%250)+1)
			backends := fmt.Sprintf("10.244.0.1:%d,10.244.0.2:%d", 8080+(j%1000), 8080+(j%1000))
			chunkVips[vip] = backends
		}
		mutateOps, err := env.ovsdbClient.Where(lb).Mutate(lb, model.Mutation{
			Field:   &lb.Vips,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   chunkVips,
		})
		if err != nil {
			fmt.Printf("Mutate error: %v\n", err)
		}
		_, _ = env.Transact(ctx, mutateOps...)
	}
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove all VIPs from the Load Balancer
	t1 := time.Now()
	clearVipsOp := ovsdb.Operation{
		Op:    "update",
		Table: "Load_Balancer",
		Where: []ovsdb.Condition{ovsdb.NewCondition("_uuid", ovsdb.ConditionEqual, ovsdb.UUID{GoUUID: lb.UUID})},
		Row: map[string]interface{}{
			"vips": ovsdb.OvsMap{GoMap: map[interface{}]interface{}{}},
		},
	}
	_, _ = env.Transact(ctx, clearVipsOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

func benchFlow(env *OVNEnvironment, n int) BenchResult {
	baseline := env.sampleMemory()

	envVars := os.Environ()
	envVars = append(envVars, "OVS_RUNDIR="+env.testDir, "OVS_LOGDIR="+env.testDir)

	// Phase 2: Add N OpenFlow rules to br-int
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "table=%d,priority=%d,ip,nw_src=10.%d.%d.%d,actions=output:%d\n",
			i%8, (i%100)+10, (i>>16)&255, (i>>8)&255, i&255, (i%4)+1)
	}

	t0 := time.Now()
	cmdOf := exec.Command(env.ovsOfctl, "add-flows", "br-int", "-")
	cmdOf.Stdin = &buf
	cmdOf.Env = envVars
	out, err := cmdOf.CombinedOutput()
	if err != nil {
		fmt.Printf("  [ovs-ofctl error] add-flows failed: %s (%v)\n", string(out), err)
	}
	addSec := float64(time.Since(t0).Microseconds()) / 1e6
	peak := env.sampleMemory()

	// Phase 3: Remove N flows
	t1 := time.Now()
	cmdDel := exec.Command(env.ovsOfctl, "del-flows", "br-int")
	cmdDel.Env = envVars
	outDel, errDel := cmdDel.CombinedOutput()
	if errDel != nil {
		fmt.Printf("  [ovs-ofctl error] del-flows failed: %s (%v)\n", string(outDel), errDel)
	}
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, 0, 0, delSec, 0)
}

func benchDatapath(env *OVNEnvironment, n int) BenchResult {
	sw, err := setupBaselineSwitch(env, "bench-dp-ls", 2)
	if err != nil {
		fmt.Printf("setupBaselineSwitch error: %v\n", err)
	}
	_ = sw
	env.Sync()

	envVars := os.Environ()
	envVars = append(envVars, "OVS_RUNDIR="+env.testDir, "OVS_LOGDIR="+env.testDir)

	// Ensure datapath flow limit and idle time accommodate high-scale tests without premature eviction
	_ = exec.Command(env.ovsVsctl, "--db="+env.ovsSock, "--no-wait", "set", "Open_vSwitch", ".",
		"other_config:max-idle=60000", "other_config:flow-limit=200000").Run()

	// Single high-priority forwarding rule that rewrites IP_SRC from IP_DST.
	// This forces ofproto-dpif-xlate to un-wildcard the destination IP in the datapath
	// megaflow cache without polluting ovs-vswitchd with N OpenFlow rules.
	cmdOf := exec.Command(env.ovsOfctl, "add-flow", "br-int",
		"cookie=0x4450,table=0,priority=65500,in_port=vif-0,udp,actions=move:NXM_OF_IP_DST[]->NXM_OF_IP_SRC[],output:vif-1")
	cmdOf.Env = envVars
	if out, err := cmdOf.CombinedOutput(); err != nil {
		fmt.Printf("  [ovs-ofctl error] add-flow failed: %s (%v)\n", string(out), err)
	}

	baseline := env.sampleMemory()

	// Phase 2: Inject N packets across distinct 5-tuples into vif-0 via netdev-dummy/receive.
	// Each unique destination IP (10.b1.b2.b3) triggers an upcall and compiles a distinct
	// megaflow entry in dpif-netdev, testing datapath flow cache memory scaling.
	t0 := time.Now()
	chunkSize := 500
	ctl := filepath.Join(env.testDir, "vswitchd.ctl")
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		args := []string{"-t", ctl, "netdev-dummy/receive", "vif-0"}
		for j := i; j < end; j++ {
			b1 := byte(((j + 2) >> 16) & 255)
			b2 := byte(((j + 2) >> 8) & 255)
			b3 := byte((j + 2) & 255)
			pkt := fmt.Sprintf("50540000000250540000000108004500002400010000401100000a0000010a%02x%02x%02x12340050001000000000000000000000", b1, b2, b3)
			args = append(args, pkt)
		}
		_ = exec.Command(env.ovnAppctl, args...).Run()
	}
	fwdSec := float64(time.Since(t0).Microseconds()) / 1e6
	peak := env.sampleMemory()

	// Phase 3: Synchronously flush datapath flows and purge revalidator ukeys
	t1 := time.Now()
	_ = exec.Command(env.ovnAppctl, "-t", ctl, "dpctl/del-flows").Run()
	_ = exec.Command(env.ovnAppctl, "-t", ctl, "revalidator/purge").Run()
	cmdDel := exec.Command(env.ovsOfctl, "del-flows", "br-int", "cookie=0x4450/-1")
	cmdDel.Env = envVars
	_ = cmdDel.Run()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	fwdRate := float64(n) / math.Max(fwdSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, fwdSec, fwdRate, 0, 0, delSec, 0)
}

func benchCombined(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	env.Recompute()
	baseline := env.sampleMemory()

	t0 := time.Now()
	nSwitches := int(math.Max(1, float64(n/100)))
	nPorts := n
	nAcls := n
	nLbs := int(math.Max(1, float64(n/20)))

	// Router and Switches
	router := &LogicalRouter{Name: "r-main"}
	opsR, _ := env.ovsdbClient.Create(router)
	resR, _ := env.Transact(ctx, opsR...)
	if len(resR) > 0 {
		router.UUID = resR[0].UUID.GoUUID
	}

	switches := make([]*LogicalSwitch, nSwitches)
	swModels := make([]model.Model, nSwitches)
	for s := 0; s < nSwitches; s++ {
		switches[s] = &LogicalSwitch{Name: fmt.Sprintf("sw-%d", s)}
		swModels[s] = switches[s]
	}
	opsSw, _ := env.ovsdbClient.Create(swModels...)
	resSw, _ := env.Transact(ctx, opsSw...)
	for s := 0; s < nSwitches; s++ {
		if s < len(resSw) {
			switches[s].UUID = resSw[s].UUID.GoUUID
		}
	}

	// Connect each switch to r-main via L3 LogicalRouterPort and router patch port
	var rPortUUIDs []string
	var rPortModels []model.Model
	var swPatchModels []model.Model
	swPatchUUIDs := make(map[int]string)
	for s := 0; s < nSwitches; s++ {
		lrpUUID := fmt.Sprintf("lrp_sw_%d", s)
		lrpName := fmt.Sprintf("lrp-sw-%d", s)
		rPortUUIDs = append(rPortUUIDs, lrpUUID)
		rPortModels = append(rPortModels, &LogicalRouterPort{
			UUID:     lrpUUID,
			Name:     lrpName,
			MAC:      fmt.Sprintf("00:ff:00:00:%02x:%02x", (s>>8)&255, s&255),
			Networks: []string{fmt.Sprintf("10.%d.0.1/16", s)},
		})

		patchUUID := fmt.Sprintf("patch_sw_%d", s)
		patchName := fmt.Sprintf("patch-sw-%d", s)
		swPatchUUIDs[s] = patchUUID
		swPatchModels = append(swPatchModels, &LogicalSwitchPort{
			UUID:      patchUUID,
			Name:      patchName,
			Type:      "router",
			Addresses: []string{"router"},
			Options:   map[string]string{"router-port": lrpName},
		})
	}
	opsLRP, _ := env.ovsdbClient.Create(rPortModels...)
	mutateR, _ := env.ovsdbClient.Where(router).Mutate(router, model.Mutation{
		Field:   &router.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   rPortUUIDs,
	})
	opsPatch, _ := env.ovsdbClient.Create(swPatchModels...)
	var mutateSwPatch []ovsdb.Operation
	for s := 0; s < nSwitches; s++ {
		m, _ := env.ovsdbClient.Where(switches[s]).Mutate(switches[s], model.Mutation{
			Field:   &switches[s].Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{swPatchUUIDs[s]},
		})
		mutateSwPatch = append(mutateSwPatch, m...)
	}
	allL3 := append(opsLRP, mutateR...)
	allL3 = append(allL3, opsPatch...)
	allL3 = append(allL3, mutateSwPatch...)
	_, _ = env.Transact(ctx, allL3...)

	// Ports
	chunkSize := 300
	for i := 0; i < nPorts; i += chunkSize {
		end := i + chunkSize
		if end > nPorts {
			end = nPorts
		}
		chunkLen := end - i
		ports := make([]model.Model, chunkLen)
		swPorts := make(map[int][]string)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			swIdx := idx % nSwitches
			namedUUID := fmt.Sprintf("p_%d", idx)
			swPorts[swIdx] = append(swPorts[swIdx], namedUUID)
			pOffset := (idx / nSwitches) + 2
			hi := (pOffset >> 8) & 255
			lo := pOffset & 255
			mac := fmt.Sprintf("00:00:%02x:%02x:%02x:%02x", swIdx&255, (swIdx>>8)&255, hi, lo)
			ip := fmt.Sprintf("10.%d.%d.%d", swIdx, hi, lo)
			ports[j] = &LogicalSwitchPort{
				UUID:         namedUUID,
				Name:         fmt.Sprintf("p-%d", idx),
				Addresses:    []string{mac + " " + ip},
				PortSecurity: []string{mac + " " + ip},
			}
		}
		opsPorts, _ := env.ovsdbClient.Create(ports...)
		var mutateOps []ovsdb.Operation
		for swIdx, pUUIDs := range swPorts {
			mOps, _ := env.ovsdbClient.Where(switches[swIdx]).Mutate(switches[swIdx], model.Mutation{
				Field:   &switches[swIdx].Ports,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   pUUIDs,
			})
			mutateOps = append(mutateOps, mOps...)
		}
		_, _ = env.Transact(ctx, append(opsPorts, mutateOps...)...)
	}

	var vifNames []string
	for j := 0; j < nPorts; j++ {
		vifNames = append(vifNames, fmt.Sprintf("p-%d", j))
	}
	env.bindChassisVIFs(vifNames)

	// ACLs
	for i := 0; i < nAcls; i += chunkSize {
		end := i + chunkSize
		if end > nAcls {
			end = nAcls
		}
		chunkLen := end - i
		acls := make([]model.Model, chunkLen)
		swAcls := make(map[int][]string)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			swIdx := idx % nSwitches
			namedUUID := fmt.Sprintf("acl_%d", idx)
			swAcls[swIdx] = append(swAcls[swIdx], namedUUID)
			pOffset := (idx / nSwitches) + 2
			hi := (pOffset >> 8) & 255
			lo := pOffset & 255
			match := fmt.Sprintf("ip4.src == 10.%d.%d.%d && tcp.dst == 80", swIdx, hi, lo)
			acls[j] = &ACL{
				UUID:      namedUUID,
				Action:    "allow-related",
				Direction: "to-lport",
				Priority:  1000 + (idx % 500),
				Match:     match,
			}
		}
		opsAcl, _ := env.ovsdbClient.Create(acls...)
		var mutateOps []ovsdb.Operation
		for swIdx, aUUIDs := range swAcls {
			mOps, _ := env.ovsdbClient.Where(switches[swIdx]).Mutate(switches[swIdx], model.Mutation{
				Field:   &switches[swIdx].ACLs,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   aUUIDs,
			})
			mutateOps = append(mutateOps, mOps...)
		}
		_, _ = env.Transact(ctx, append(opsAcl, mutateOps...)...)
	}

	// Load Balancers
	proto := "tcp"
	for i := 0; i < nLbs; i += chunkSize {
		end := i + chunkSize
		if end > nLbs {
			end = nLbs
		}
		chunkLen := end - i
		lbs := make([]model.Model, chunkLen)
		swLbs := make(map[int][]string)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			swIdx := idx % nSwitches
			namedUUID := fmt.Sprintf("lb_%d", idx)
			swLbs[swIdx] = append(swLbs[swIdx], namedUUID)
			vip := fmt.Sprintf("10.96.%d.%d:80", (idx/250)&255, (idx%250)+1)
			backends := "10.0.0.1:8080,10.0.0.2:8080"
			lbs[j] = &LoadBalancer{
				UUID:     namedUUID,
				Name:     fmt.Sprintf("lb-%d", idx),
				Protocol: &proto,
				Vips:     map[string]string{vip: backends},
			}
		}
		opsLb, _ := env.ovsdbClient.Create(lbs...)
		var mutateOps []ovsdb.Operation
		for swIdx, lUUIDs := range swLbs {
			mOps, _ := env.ovsdbClient.Where(switches[swIdx]).Mutate(switches[swIdx], model.Mutation{
				Field:   &switches[swIdx].LoadBalancer,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   lUUIDs,
			})
			mutateOps = append(mutateOps, mOps...)
		}
		_, _ = env.Transact(ctx, append(opsLb, mutateOps...)...)
	}

	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := env.sampleMemory()

	// Phase 3: Remove all via libovsdb
	t1 := time.Now()
	env.unbindChassisVIFs(nPorts)
	delPortOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	delAclOp := ovsdb.Operation{
		Op:    "delete",
		Table: "ACL",
		Where: []ovsdb.Condition{ovsdb.NewCondition("priority", ovsdb.ConditionGreaterThanOrEqual, 0)},
	}
	delLbOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Load_Balancer",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	delSwOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	delROp := ovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Router",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	_, _ = env.Transact(ctx, delSwOp, delROp, delPortOp, delAclOp, delLbOp)

	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	env.TrimMemory()
	time.Sleep(env.decayWaitTime())
	postDel := env.sampleMemory()

	addRate := float64(n) / math.Max(addSec, 0.001)
	return buildBenchResult(baseline, peak, postDel, addSec, addRate, addLoop, addLflow, delSec, delLoop)
}

// ---------------------------------------------------------------------------
// Helpers & Report Generation
// ---------------------------------------------------------------------------

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func calcPct(vGlibc, vJemalloc float64, unit string) string {
	if vGlibc <= 0 {
		return "N/A"
	}
	// Filter sub-millisecond timer resolution noise
	if unit == "ms" && vGlibc <= 1.0 && vJemalloc <= 1.0 {
		return "~Parity (<1ms)"
	}
	if unit == "s" && vGlibc <= 0.001 && vJemalloc <= 0.001 {
		return "~Parity (<1ms)"
	}
	diff := ((vJemalloc - vGlibc) / vGlibc) * 100.0
	if math.Abs(diff) < 1.0 {
		return "~Parity (±1%)"
	}
	return fmt.Sprintf("%+.1f%%", diff)
}

func generateMermaidChart(title, yLabel string, categories []string, gVals, jVals []float64, chartType string) string {
	maxV := 0.0
	for _, v := range append(gVals, jVals...) {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		maxV = 10
	}
	maxAxis := int(math.Ceil(maxV * 1.15))

	catStrs := make([]string, len(categories))
	for i, c := range categories {
		catStrs[i] = fmt.Sprintf("%q", c)
	}
	gStrs := make([]string, len(gVals))
	for i, v := range gVals {
		gStrs[i] = fmt.Sprintf("%.1f", v)
	}
	jStrs := make([]string, len(jVals))
	for i, v := range jVals {
		jStrs[i] = fmt.Sprintf("%.1f", v)
	}

	var b strings.Builder
	b.WriteString("```mermaid\n")
	b.WriteString("---\n")
	b.WriteString("config:\n")
	b.WriteString("    themeVariables:\n")
	b.WriteString("        xyChart:\n")
	b.WriteString("            plotColorPalette: \"#1f77b4, #ff7f0e\"\n")
	b.WriteString("---\n")
	b.WriteString("xychart-beta\n")
	b.WriteString(fmt.Sprintf("    title %q\n", title))
	b.WriteString(fmt.Sprintf("    x-axis [%s]\n", strings.Join(catStrs, ", ")))
	b.WriteString(fmt.Sprintf("    y-axis %q 0 --> %d\n", yLabel, maxAxis))
	b.WriteString(fmt.Sprintf("    %s \"glibc malloc\" [%s]\n", chartType, strings.Join(gStrs, ", ")))
	b.WriteString(fmt.Sprintf("    %s \"jemalloc\" [%s]\n", chartType, strings.Join(jStrs, ", ")))
	b.WriteString("```\n\n")
	return b.String()
}

func generateReport(results map[string]map[string]map[string]BenchResult, sysInfo map[string]string) string {
	var lines []string
	lines = append(lines, "# OVS & OVN Scaling Benchmark: glibc malloc vs jemalloc", "")
	lines = append(lines, fmt.Sprintf("**Date:** %s", time.Now().Format("2006-01-02 15:04:05")))
	lines = append(lines, fmt.Sprintf("**System:** %s", sysInfo["os"]))
	lines = append(lines, fmt.Sprintf("**CPU:** %s (%s vCPUs)", sysInfo["cpu_model"], sysInfo["cpu_cores"]))
	lines = append(lines, fmt.Sprintf("**Memory:** %s", sysInfo["total_ram"]))
	lines = append(lines, fmt.Sprintf("**Glibc Version:** %s", sysInfo["glibc_version"]))
	lines = append(lines, fmt.Sprintf("**Jemalloc Version:** %s", sysInfo["jemalloc_version"]), "")
	lines = append(lines, "---", "")
	lines = append(lines, "## Methodology", "")
	lines = append(lines, "Each benchmark evaluates individual OVN/OVS objects, OpenFlow rule scaling, or combined workloads across powers of 10 (`1, 10, 100, 1,000, 10,000, 100,000`) through 3 lifecycle phases measuring the full OVN-Kubernetes pod stack (`ovn-northd`, `ovn-controller`, `ovs-vswitchd`, and `ovsdb-servers`):")
	lines = append(lines, "1. **Baseline (Idle)**: Total Pod and per-daemon clean process RSS before creating objects.")
	lines = append(lines, "2. **After Add (N)**: Time taken to commit $N$ objects via native OVSDB JSON-RPC, full recompute latency (`ovn-northd-loop` stopwatch), and Total Pod peak RSS (`VmHWM`) with per-daemon breakdown.")
	lines = append(lines, "3. **After Remove (N)**: Time taken to delete all $N$ objects via native OVSDB JSON-RPC, post-deletion recompute latency, and retained heap memory (`Post-Deletion RSS - Baseline RSS`) measured after a wait period allowing page decay (`dirty_decay_ms:0,muzzy_decay_ms:0`).", "")
	lines = append(lines, "---", "")

	objTitles := map[string]string{
		"port":          "Logical Switch Port (LSP with IP, MAC, port_security)",
		"acl":           "ACL (Access Control List with stateful allow-related)",
		"address_set":   "Address Set (IP list matching ACL)",
		"port_group":    "Port Group (Ports member grouping matching ACL)",
		"load_balancer": "Load Balancer (VIPs mapped to endpoints)",
		"flow":          "OpenFlow Flows (OVS Control Plane)",
		"datapath":      "OVS Datapath Packet Forwarding & Megaflow Cache",
		"combined":      "Combined Workload (All objects scaled proportionally)",
	}

	for _, obj := range []string{"port", "acl", "address_set", "port_group", "load_balancer", "flow", "datapath", "combined"} {
		scalesData, exists := results[obj]
		if !exists {
			continue
		}
		var scalesList []int
		for sStr, allocMap := range scalesData {
			if len(allocMap) == 0 {
				continue
			}
			hasData := false
			for _, res := range allocMap {
				if res.AddRate > 0 || res.PeakRSS > 0 || res.BaselineRSS > 0 {
					hasData = true
					break
				}
			}
			if !hasData {
				continue
			}
			if s, err := strconv.Atoi(sStr); err == nil {
				scalesList = append(scalesList, s)
			}
		}
		if len(scalesList) == 0 {
			continue
		}

		title := objTitles[obj]
		lines = append(lines, fmt.Sprintf("## %s", title), "")
		lines = append(lines, "| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |")
		lines = append(lines, "|---|---|---|---|---|")
		for i := 0; i < len(scalesList); i++ {
			for j := i + 1; j < len(scalesList); j++ {
				if scalesList[i] > scalesList[j] {
					scalesList[i], scalesList[j] = scalesList[j], scalesList[i]
				}
			}
		}

		for _, nVal := range scalesList {
			scaleStr := strconv.Itoa(nVal)
			allocData := scalesData[scaleStr]
			g := allocData["glibc"]
			j := allocData["jemalloc"]

			addUnit := "ops/s"
			addLabel := "Add Duration"
			rateLabel := "Add Rate"
			delLabel := "Delete Duration"
			if obj == "address_set" {
				addUnit = "addrs/s"
				rateLabel = "Address Insertion Rate"
			} else if obj == "datapath" {
				addUnit = "pkts/s"
				addLabel = "Forwarding Duration"
				rateLabel = "Forwarding Rate"
				delLabel = "Datapath Flush Duration"
			}

			lines = append(lines, fmt.Sprintf("| **%d** | Total Pod Baseline Heap (RssAnon) | %.2f MiB | %.2f MiB | %s |",
				nVal, g.BaselineRSS, j.BaselineRSS, calcPct(g.BaselineRSS, j.BaselineRSS, "MB")))
			lines = append(lines, fmt.Sprintf("| | %s | %.3f s | %.3f s | %s |",
				addLabel, g.AddSec, j.AddSec, calcPct(g.AddSec, j.AddSec, "s")))
			lines = append(lines, fmt.Sprintf("| | %s | %s %s | %s %s | %s |",
				rateLabel, formatNum(g.AddRate), addUnit, formatNum(j.AddRate), addUnit, calcPct(g.AddRate, j.AddRate, "rate")))

			if obj != "flow" && obj != "datapath" {
				if len(g.ThreadLoops) > 0 {
					for tKey, gVal := range g.ThreadLoops {
						jVal := j.ThreadLoops[tKey]
						lines = append(lines, fmt.Sprintf("| | Recompute (`northd-loop`) (%s) | %.1f ms | %.1f ms | %s |",
							strings.ToUpper(tKey), gVal, jVal, calcPct(gVal, jVal, "ms")))
					}
				} else {
					lines = append(lines, fmt.Sprintf("| | Recompute (`ovn-northd-loop`) | %.1f ms | %.1f ms | %s |",
						g.AddLoopMs, j.AddLoopMs, calcPct(g.AddLoopMs, j.AddLoopMs, "ms")))
					lines = append(lines, fmt.Sprintf("| | Logical Flow (`lflow`) | %.1f ms | %.1f ms | %s |",
						g.AddLflowMs, j.AddLflowMs, calcPct(g.AddLflowMs, j.AddLflowMs, "ms")))
				}
			}

			lines = append(lines, fmt.Sprintf("| | Total Pod Peak Heap (RssAnon) | %.2f MiB | %.2f MiB | %s |",
				g.PeakRSS, j.PeakRSS, calcPct(g.PeakRSS, j.PeakRSS, "MB")))
			lines = append(lines, fmt.Sprintf("| | %s | %.3f s | %.3f s | %s |",
				delLabel, g.DelSec, j.DelSec, calcPct(g.DelSec, j.DelSec, "s")))

			if obj != "flow" && obj != "datapath" {
				lines = append(lines, fmt.Sprintf("| | Post-Delete Recompute | %.1f ms | %.1f ms | %s |",
					g.DelLoopMs, j.DelLoopMs, calcPct(g.DelLoopMs, j.DelLoopMs, "ms")))
			}

			lines = append(lines, fmt.Sprintf("| | Total Pod Post-Delete Heap (RssAnon) | %.2f MiB | %.2f MiB | %s |",
				g.PostDelRSS, j.PostDelRSS, calcPct(g.PostDelRSS, j.PostDelRSS, "MB")))
			lines = append(lines, fmt.Sprintf("| | **Total Pod Retained Heap** | **%+.2f MiB** | **%+.2f MiB** | **%+.2f MiB** |",
				g.RetainedRSS, j.RetainedRSS, j.RetainedRSS-g.RetainedRSS))

			if len(g.Components) > 0 {
				compOrder := []struct {
					key  string
					name string
				}{
					{"ovn-northd", "ovn-northd"},
					{"ovn-controller", "ovn-controller"},
					{"ovs-vswitchd", "ovs-vswitchd"},
					{"ovsdb-sb", "ovsdb-server (SB)"},
					{"ovsdb-nb", "ovsdb-server (NB)"},
					{"ovsdb-ovs", "ovsdb-server (OVS)"},
				}
				for _, comp := range compOrder {
					gC := g.Components[comp.key]
					jC := j.Components[comp.key]
					lines = append(lines, fmt.Sprintf("| | ├─ `%s` (Peak / Retained Heap) | %.2f / %+.2f MiB | %.2f / %+.2f MiB | %s / %+.2f MiB |",
						comp.name, gC.PeakRSS, gC.RetainedRSS, jC.PeakRSS, jC.RetainedRSS, calcPct(gC.PeakRSS, jC.PeakRSS, "MB"), jC.RetainedRSS-gC.RetainedRSS))
				}
			}
			lines = append(lines, "|---|---|---|---|---|")
		}
		lines = append(lines, "")

		if len(scalesList) >= 2 {
			cats := make([]string, len(scalesList))
			gPeak := make([]float64, len(scalesList))
			jPeak := make([]float64, len(scalesList))
			gLoop := make([]float64, len(scalesList))
			jLoop := make([]float64, len(scalesList))
			gRate := make([]float64, len(scalesList))
			jRate := make([]float64, len(scalesList))

			for i, nVal := range scalesList {
				scaleStr := strconv.Itoa(nVal)
				allocData := scalesData[scaleStr]
				g := allocData["glibc"]
				j := allocData["jemalloc"]

				if nVal >= 1000 {
					cats[i] = fmt.Sprintf("%dk", nVal/1000)
				} else {
					cats[i] = strconv.Itoa(nVal)
				}
				gPeak[i] = g.PeakRSS
				jPeak[i] = j.PeakRSS
				gLoop[i] = g.AddLoopMs
				jLoop[i] = j.AddLoopMs
				gRate[i] = g.AddRate
				jRate[i] = j.AddRate
			}

			lines = append(lines, "### Visual Charts", "")
			if obj != "flow" && obj != "datapath" {
				chartTitle := fmt.Sprintf("%s - Recompute Latency", title)
				lines = append(lines, generateMermaidChart(chartTitle, "northd-loop (ms)", cats, gLoop, jLoop, "line"))
			} else {
				chartUnit := "ops/s"
				if obj == "datapath" {
					chartUnit = "pkts/s"
				}
				chartTitle := fmt.Sprintf("%s - Throughput", title)
				lines = append(lines, generateMermaidChart(chartTitle, chartUnit, cats, gRate, jRate, "line"))
			}

			chartPeakTitle := fmt.Sprintf("%s - Peak Memory", title)
			lines = append(lines, generateMermaidChart(chartPeakTitle, "Peak RSS (MiB)", cats, gPeak, jPeak, "line"))
		}
	}

	return strings.Join(lines, "\n")
}

func formatNum(f float64) string {
	parts := strings.Split(fmt.Sprintf("%.1f", f), ".")
	intPart := parts[0]
	var res []string
	for i := len(intPart); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		res = append([]string{intPart[start:i]}, res...)
	}
	return strings.Join(res, ",")
}

// ---------------------------------------------------------------------------
// Configuration & System Discovery
// ---------------------------------------------------------------------------

type Config struct {
	scales        []int
	threads       []int
	objects       []string
	allocators    []string
	flowMax       int
	ovsDir        string
	ovnDir        string
	useSystem     bool
	jemallocConf  string
	reportOnly    bool
	merge         bool
	trimTimeoutMs int
	runs          int
}

func parseConfig() *Config {
	scalesFlag := flag.String("scales", "1,10,100,1000,10000", "Comma-separated scales to evaluate")
	threadsFlag := flag.String("threads", "4", "Comma-separated thread counts to evaluate (e.g. 1,4,8)")
	objectsFlag := flag.String("objects", "all", "Comma-separated objects: port,acl,address_set,port_group,load_balancer,flow,datapath,combined,all")
	allocatorsFlag := flag.String("allocators", "glibc,jemalloc", "Comma-separated allocators: glibc,jemalloc")
	flowMaxFlag := flag.Int("flow-max", 100000, "Max scale for OpenFlow rules")
	ovsDirFlag := flag.String("ovs-dir", "", "Directory containing OVS binaries (or build/ovs-*)")
	ovnDirFlag := flag.String("ovn-dir", "", "Directory containing OVN binaries (or build/ovn-*)")
	useSystemFlag := flag.Bool("use-system", false, "Force using system/container-installed packages and LD_PRELOAD")
	jemallocConfFlag := flag.String("jemalloc-conf", defaultJemallocConf, "MALLOC_CONF string for jemalloc")
	reportOnlyFlag := flag.Bool("report-only", false, "Regenerate markdown report from results/benchmark_data.json and exit")
	mergeFlag := flag.Bool("merge", false, "Merge results with existing results/benchmark_data.json instead of starting fresh")
	trimTimeoutFlag := flag.Int("trim-timeout-ms", 1000, "Inactivity timeout in ms for OVN/OVS memory trimming (northd_trim_timeout, ovn-trim-timeout-ms; default 1000ms)")
	runsFlag := flag.Int("runs", 1, "Number of benchmark iterations per scale (reports median across runs, e.g. -runs 3)")
	flag.Parse()

	cfg := &Config{
		flowMax:       *flowMaxFlag,
		ovsDir:        *ovsDirFlag,
		ovnDir:        *ovnDirFlag,
		useSystem:     *useSystemFlag,
		jemallocConf:  *jemallocConfFlag,
		reportOnly:    *reportOnlyFlag,
		merge:         *mergeFlag,
		trimTimeoutMs: *trimTimeoutFlag,
		runs:          *runsFlag,
	}
	if cfg.runs < 1 {
		cfg.runs = 1
	}

	for _, s := range strings.Split(*scalesFlag, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			cfg.scales = append(cfg.scales, v)
		}
	}
	for _, t := range strings.Split(*threadsFlag, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			cfg.threads = append(cfg.threads, v)
		}
	}
	if len(cfg.threads) == 0 {
		cfg.threads = []int{4}
	}

	if *objectsFlag == "all" {
		cfg.objects = []string{"port", "acl", "address_set", "port_group", "load_balancer", "flow", "datapath", "combined"}
	} else {
		for _, o := range strings.Split(*objectsFlag, ",") {
			cfg.objects = append(cfg.objects, strings.TrimSpace(o))
		}
	}

	for _, a := range strings.Split(*allocatorsFlag, ",") {
		cfg.allocators = append(cfg.allocators, strings.TrimSpace(a))
	}

	return cfg
}

func getSystemInfo() map[string]string {
	sysInfo := map[string]string{
		"os":               "Linux",
		"cpu_model":        "Unknown",
		"cpu_cores":        "Unknown",
		"total_ram":        "Unknown",
		"glibc_version":    "Unknown",
		"jemalloc_version": "5.3.0",
	}

	if out, err := exec.Command("lscpu").Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if strings.Contains(l, "Model name:") {
				parts := strings.Split(l, ":")
				if len(parts) >= 2 {
					sysInfo["cpu_model"] = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	if out, err := exec.Command("nproc").Output(); err == nil {
		sysInfo["cpu_cores"] = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("sh", "-c", "free -h | grep 'Mem:'").Output(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 {
			sysInfo["total_ram"] = fields[1]
		}
	}
	if out, err := exec.Command("ldd", "--version").Output(); err == nil {
		sysInfo["glibc_version"] = strings.Split(string(out), "\n")[0]
	}
	if out, err := exec.Command("dpkg-query", "-W", "-f=${Version}", "libjemalloc2").Output(); err == nil && len(out) > 0 {
		sysInfo["jemalloc_version"] = strings.TrimSpace(string(out))
	}
	return sysInfo
}

func resolveBuildDirs(cfg *Config, alloc string) (string, string) {
	ovsDir := cfg.ovsDir
	ovnDir := cfg.ovnDir
	inContainer := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		inContainer = true
	}
	if !cfg.useSystem && !inContainer {
		if ovsDir == "" {
			for _, cand := range []string{fmt.Sprintf("build/ovs-%s", alloc), "build/ovs-glibc", "build/ovs"} {
				if info, err := os.Stat(cand); err == nil && info.IsDir() {
					ovsDir = cand
					break
				}
			}
		}
		if ovnDir == "" {
			for _, cand := range []string{fmt.Sprintf("build/ovn-%s", alloc), "build/ovn-glibc", "build/ovn"} {
				if info, err := os.Stat(cand); err == nil && info.IsDir() {
					ovnDir = cand
					break
				}
			}
		}
	}
	return ovsDir, ovnDir
}

func dispatchOVNBench(env *OVNEnvironment, obj string, n int) BenchResult {
	switch obj {
	case "port":
		return benchPort(env, n)
	case "acl":
		return benchACL(env, n)
	case "address_set":
		return benchAddressSet(env, n)
	case "port_group":
		return benchPortGroup(env, n)
	case "load_balancer":
		return benchLoadBalancer(env, n)
	case "flow":
		return benchFlow(env, n)
	case "datapath":
		return benchDatapath(env, n)
	case "combined":
		return benchCombined(env, n)
	default:
		return BenchResult{}
	}
}

func runSingleBenchmark(obj, alloc string, n int, cfg *Config) (BenchResult, error) {
	ovsDir, ovnDir := resolveBuildDirs(cfg, alloc)

	primaryT := cfg.threads[0]
	testDir := filepath.Join(os.TempDir(), fmt.Sprintf("ovn_bench_%s_%s_%d_%d", alloc, obj, n, os.Getpid()))
	env, err := NewOVNEnvironment(alloc, cfg.jemallocConf, ovsDir, ovnDir, testDir, primaryT, cfg.trimTimeoutMs)
	if err != nil {
		return BenchResult{}, err
	}
	if err := env.Start(); err != nil {
		env.Stop()
		return BenchResult{}, err
	}

	r := dispatchOVNBench(env, obj, n)
	env.Stop()

	// Measure thread scaling if multiple thread counts requested
	if len(cfg.threads) > 1 {
		r.ThreadLoops = make(map[string]float64)
		r.ThreadLflows = make(map[string]float64)
		for _, t := range cfg.threads {
			tKey := fmt.Sprintf("%dt", t)
			if t == primaryT {
				r.ThreadLoops[tKey] = r.AddLoopMs
				r.ThreadLflows[tKey] = r.AddLflowMs
				continue
			}
			tDir := filepath.Join(os.TempDir(), fmt.Sprintf("ovn_bench_%s_%s_%d_t%d_%d", alloc, obj, n, t, os.Getpid()))
			envT, err := NewOVNEnvironment(alloc, cfg.jemallocConf, ovsDir, ovnDir, tDir, t, cfg.trimTimeoutMs)
			if err == nil && envT.Start() == nil {
				rt := dispatchOVNBench(envT, obj, n)
				r.ThreadLoops[tKey] = rt.AddLoopMs
				r.ThreadLflows[tKey] = rt.AddLflowMs
				envT.Stop()
			}
		}
	}
	return r, nil
}

func filterScales(obj string, cfg *Config) []int {
	scales := make([]int, len(cfg.scales))
	copy(scales, cfg.scales)
	if (obj == "flow" || obj == "datapath") && cfg.flowMax > 0 {
		hasMax := false
		for _, s := range scales {
			if s == cfg.flowMax {
				hasMax = true
			}
		}
		if !hasMax {
			scales = append(scales, cfg.flowMax)
		}
	}
	return scales
}

func loadStoredResults() map[string]map[string]map[string]BenchResult {
	results := make(map[string]map[string]map[string]BenchResult)
	if data, err := os.ReadFile("results/benchmark_data.json"); err == nil {
		var stored struct {
			Data map[string]map[string]map[string]BenchResult `json:"data"`
		}
		if err := json.Unmarshal(data, &stored); err == nil && stored.Data != nil {
			results = stored.Data
		}
	}
	return results
}

func saveBenchmarkResults(results map[string]map[string]map[string]BenchResult, sysInfo map[string]string) {
	_ = os.MkdirAll("results", 0755)
	outputJSON := map[string]interface{}{
		"system": sysInfo,
		"data":   results,
	}
	if data, err := json.MarshalIndent(outputJSON, "", "  "); err == nil {
		_ = os.WriteFile("results/benchmark_data.json", data, 0644)
		fmt.Println("\nSaved results/benchmark_data.json")
	}

	reportMD := generateReport(results, sysInfo)
	_ = os.WriteFile("results/BENCHMARK_REPORT.md", []byte(reportMD), 0644)
	fmt.Println("Saved results/BENCHMARK_REPORT.md")
}

func computeMedianResult(results []BenchResult) BenchResult {
	if len(results) == 0 {
		return BenchResult{}
	}
	if len(results) == 1 {
		return results[0]
	}

	n := len(results)
	mid := n / 2

	getMedianFloat := func(vals []float64) float64 {
		sort.Float64s(vals)
		if n%2 == 1 {
			return vals[mid]
		}
		return (vals[mid-1] + vals[mid]) / 2.0
	}

	baselineList := make([]float64, n)
	addSecList := make([]float64, n)
	addRateList := make([]float64, n)
	addLoopList := make([]float64, n)
	addLflowList := make([]float64, n)
	peakList := make([]float64, n)
	delSecList := make([]float64, n)
	delLoopList := make([]float64, n)
	postDelList := make([]float64, n)
	retainedList := make([]float64, n)

	for i, r := range results {
		baselineList[i] = r.BaselineRSS
		addSecList[i] = r.AddSec
		addRateList[i] = r.AddRate
		addLoopList[i] = r.AddLoopMs
		addLflowList[i] = r.AddLflowMs
		peakList[i] = r.PeakRSS
		delSecList[i] = r.DelSec
		delLoopList[i] = r.DelLoopMs
		postDelList[i] = r.PostDelRSS
		retainedList[i] = r.RetainedRSS
	}

	med := BenchResult{
		BaselineRSS: round2(getMedianFloat(baselineList)),
		AddSec:      round3(getMedianFloat(addSecList)),
		AddRate:     round1(getMedianFloat(addRateList)),
		AddLoopMs:   round1(getMedianFloat(addLoopList)),
		AddLflowMs:  round1(getMedianFloat(addLflowList)),
		PeakRSS:     round2(getMedianFloat(peakList)),
		DelSec:      round3(getMedianFloat(delSecList)),
		DelLoopMs:   round1(getMedianFloat(delLoopList)),
		PostDelRSS:  round2(getMedianFloat(postDelList)),
		Components:  make(map[string]ComponentMem),
	}
	med.PeakRSS = math.Max(med.PeakRSS, med.PostDelRSS)
	med.RetainedRSS = round2(med.PostDelRSS - med.BaselineRSS)

	if len(results[0].Components) > 0 {
		for cName := range results[0].Components {
			cBase := make([]float64, n)
			cPeak := make([]float64, n)
			cPost := make([]float64, n)
			for i, r := range results {
				cBase[i] = r.Components[cName].BaselineRSS
				cPeak[i] = r.Components[cName].PeakRSS
				cPost[i] = r.Components[cName].PostDelRSS
			}
			baseVal := round2(getMedianFloat(cBase))
			postVal := round2(getMedianFloat(cPost))
			peakVal := math.Max(round2(getMedianFloat(cPeak)), postVal)
			med.Components[cName] = ComponentMem{
				BaselineRSS: baseVal,
				PeakRSS:     peakVal,
				PostDelRSS:  postVal,
				RetainedRSS: round2(postVal - baseVal),
			}
		}
	}

	if len(results[0].ThreadLoops) > 0 {
		med.ThreadLoops = make(map[string]float64)
		med.ThreadLflows = make(map[string]float64)
		for tKey := range results[0].ThreadLoops {
			tLoopList := make([]float64, n)
			tLflowList := make([]float64, n)
			for i, r := range results {
				tLoopList[i] = r.ThreadLoops[tKey]
				tLflowList[i] = r.ThreadLflows[tKey]
			}
			med.ThreadLoops[tKey] = round1(getMedianFloat(tLoopList))
			med.ThreadLflows[tKey] = round1(getMedianFloat(tLflowList))
		}
	}
	return med
}

// ---------------------------------------------------------------------------
// Main Orchestration
// ---------------------------------------------------------------------------

func main() {
	cfg := parseConfig()
	sysInfo := getSystemInfo()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n[signal] Interrupted by user. Cleaning up all active daemons...")
		activeEnvsMu.Lock()
		for e := range activeEnvs {
			e.Stop()
		}
		activeEnvsMu.Unlock()
		os.Exit(1)
	}()

	fmt.Printf("System: %s | %s cores | %s RAM\n", sysInfo["cpu_model"], sysInfo["cpu_cores"], sysInfo["total_ram"])
	fmt.Printf("Testing Objects (via native libovsdb): %v\n", cfg.objects)
	fmt.Printf("Scales: %v | Threads: %v | Runs per scale: %d\n\n", cfg.scales, cfg.threads, cfg.runs)

	results := make(map[string]map[string]map[string]BenchResult)
	if cfg.reportOnly || cfg.merge {
		results = loadStoredResults()
	}

	if cfg.reportOnly {
		saveBenchmarkResults(results, sysInfo)
		return
	}

	for _, obj := range cfg.objects {
		fmt.Printf("=== Benchmarking Object: %s ===\n", strings.ToUpper(obj))
		if _, ok := results[obj]; !ok {
			results[obj] = make(map[string]map[string]BenchResult)
		}

		for _, n := range filterScales(obj, cfg) {
			nStr := strconv.Itoa(n)
			if _, ok := results[obj][nStr]; !ok {
				results[obj][nStr] = make(map[string]BenchResult)
			}
			fmt.Printf("  Scale N = %d...\n", n)

			for _, alloc := range cfg.allocators {
				var runs []BenchResult
				for runIdx := 0; runIdx < cfg.runs; runIdx++ {
					r, err := runSingleBenchmark(obj, alloc, n, cfg)
					if err != nil {
						fmt.Printf("    [%s] Benchmark error on run %d: %v\n", alloc, runIdx+1, err)
						break
					}
					runs = append(runs, r)
				}
				if len(runs) == 0 {
					continue
				}
				r := computeMedianResult(runs)

				results[obj][nStr][alloc] = r
				if cfg.runs > 1 {
					fmt.Printf("    [%s] (median of %d runs) Add: %.3fs (%.1f ops/s) | Loop: %.1fms | Peak RSS: %.2fMB | Retained: %+.2fMB\n",
						alloc, len(runs), r.AddSec, r.AddRate, r.AddLoopMs, r.PeakRSS, r.RetainedRSS)
				} else {
					fmt.Printf("    [%s] Add: %.3fs (%.1f ops/s) | Loop: %.1fms | Peak RSS: %.2fMB | Retained: %+.2fMB\n",
						alloc, r.AddSec, r.AddRate, r.AddLoopMs, r.PeakRSS, r.RetainedRSS)
				}
			}
		}
	}

	saveBenchmarkResults(results, sysInfo)
}
