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
	"path/filepath"
	"strconv"
	"strings"
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
	VmRSS float64 `json:"vm_rss_mib"`
	VmHWM float64 `json:"vm_hwm_mib"`
}

type BenchResult struct {
	BaselineRSS  float64            `json:"baseline_rss_mib"`
	AddSec       float64            `json:"add_sec"`
	AddRate      float64            `json:"add_rate"`
	AddLoopMs    float64            `json:"add_loop_ms"`
	AddLflowMs   float64            `json:"add_lflow_ms"`
	PeakRSS      float64            `json:"peak_rss_mib"`
	DelSec       float64            `json:"del_sec"`
	DelLoopMs    float64            `json:"del_loop_ms"`
	PostDelRSS   float64            `json:"post_del_rss_mib"`
	RetainedRSS  float64            `json:"retained_rss_mib"`
	ThreadLoops  map[string]float64 `json:"thread_loops,omitempty"`
	ThreadLflows map[string]float64 `json:"thread_lflows,omitempty"`
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
		"/home/ljouin/workspaces/ovn-org/ovn/" + name,
		"/home/ljouin/workspaces/openvswitch/ovs/vswitchd/" + name,
	}
	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
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
				if k == "VmRSS" {
					res.VmRSS = math.Round((v/1024.0)*100) / 100
				} else if k == "VmHWM" {
					res.VmHWM = math.Round((v/1024.0)*100) / 100
				}
			}
		}
	}
	return res
}

func parseStopwatch(out string) float64 {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Maximum:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				f := strings.Fields(parts[1])
				if len(f) > 0 {
					if v, err := strconv.ParseFloat(f[0], 64); err == nil {
						return v
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

// ---------------------------------------------------------------------------
// OVN Environment & libovsdb Client Management
// ---------------------------------------------------------------------------

const (
	defaultJemallocConf = "background_thread:true,dirty_decay_ms:0,muzzy_decay_ms:0"
	decayWaitTime       = 5 * time.Second
)

type OVNEnvironment struct {
	alloc        string
	jemallocConf string
	ovsDir       string
	ovnDir       string
	testDir      string
	nThreads     int
	nbSock       string
	sbSock       string
	northdPid    int
	ovsdbTool    string
	ovsdbServer  string
	ovnNbctl     string
	ovnSbctl     string
	ovnNorthd    string
	ovnAppctl    string
	ovsdbClient  client.Client
}

func NewOVNEnvironment(alloc, jemallocConf, ovsDir, ovnDir, testDir string, nThreads int) (*OVNEnvironment, error) {
	tool, err := findBinary("ovsdb-tool", ovsDir)
	if err != nil {
		return nil, err
	}
	server, err := findBinary("ovsdb-server", ovsDir)
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
	appctl, err := findBinary("ovn-appctl", ovnDir)
	if err != nil {
		return nil, err
	}

	return &OVNEnvironment{
		alloc:        alloc,
		jemallocConf: jemallocConf,
		ovsDir:       ovsDir,
		ovnDir:       ovnDir,
		testDir:      testDir,
		nThreads:     nThreads,
		nbSock:       "unix:" + filepath.Join(testDir, "nb.sock"),
		sbSock:       "unix:" + filepath.Join(testDir, "sb.sock"),
		ovsdbTool:    tool,
		ovsdbServer:  server,
		ovnNbctl:     nbctl,
		ovnSbctl:     sbctl,
		ovnNorthd:    northd,
		ovnAppctl:    appctl,
	}, nil
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

	_ = exec.Command(e.ovsdbTool, "create", filepath.Join(e.testDir, "nb.db"), nbSchema).Run()
	_ = exec.Command(e.ovsdbTool, "create", filepath.Join(e.testDir, "sb.db"), sbSchema).Run()

	envVars := os.Environ()
	if e.alloc == "jemalloc" {
		if jlib := findJemallocLib(); jlib != "" {
			conf := e.jemallocConf
			if conf == "" {
				conf = defaultJemallocConf
			}
			envVars = append(envVars, "LD_PRELOAD="+jlib, "MALLOC_CONF="+conf)
		}
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

	time.Sleep(100 * time.Millisecond)

	_ = exec.Command(e.ovnNbctl, "--db="+e.nbSock, "init").Run()
	_ = exec.Command(e.ovnSbctl, "--db="+e.sbSock, "init").Run()

	cmdNorthd := exec.Command(e.ovnNorthd, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(e.testDir, "northd.pid"),
		"--unixctl="+filepath.Join(e.testDir, "northd.ctl"),
		"--ovnnb-db="+e.nbSock, "--ovnsb-db="+e.sbSock,
		fmt.Sprintf("--n-threads=%d", e.nThreads))
	cmdNorthd.Env = envVars
	if err := cmdNorthd.Run(); err != nil {
		return err
	}

	pidData, err := os.ReadFile(filepath.Join(e.testDir, "northd.pid"))
	if err == nil {
		e.northdPid, _ = strconv.Atoi(strings.TrimSpace(string(pidData)))
	}

	_ = exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "inc-engine/enable-stopwatch").Run()

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

	return nil
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

func (e *OVNEnvironment) Sync() {
	_ = exec.Command(e.ovnNbctl, "--db="+e.nbSock, "--wait=sb", "sync").Run()
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
	if err != nil {
		outLflow, _ = exec.Command(e.ovnAppctl, "-t", filepath.Join(e.testDir, "northd.ctl"), "stopwatch/show", "build_lflows").CombinedOutput()
	}
	lflowMs = parseStopwatch(string(outLflow))

	if loopMs == 0.0 {
		loopMs = wallMs
	}
	return
}

func (e *OVNEnvironment) Stop() {
	if e.ovsdbClient != nil {
		e.ovsdbClient.Close()
	}
	killPidFile(filepath.Join(e.testDir, "northd.pid"))
	killPidFile(filepath.Join(e.testDir, "nb.pid"))
	killPidFile(filepath.Join(e.testDir, "sb.pid"))
	_ = os.RemoveAll(e.testDir)
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
	baseline := getProcMem(env.northdPid)

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
			mac := fmt.Sprintf("00:00:%02x:%02x:00:02", (idx>>8)&255, idx&255)
			ip := fmt.Sprintf("10.%d.%d.2", (idx>>8)&255, idx&255)
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
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := getProcMem(env.northdPid)

	// Phase 3: Remove N ports from switch and delete rows
	t1 := time.Now()
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
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchACL(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := getProcMem(env.northdPid)

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
	peak := getProcMem(env.northdPid)

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
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchAddressSet(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := getProcMem(env.northdPid)

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
	peak := getProcMem(env.northdPid)

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
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchPortGroup(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}

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
			ports[j] = &LogicalSwitchPort{
				UUID: namedUUID,
				Name: fmt.Sprintf("p-%d", idx),
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
	env.Sync()
	env.Recompute()
	baseline := getProcMem(env.northdPid)

	// Phase 2: Create Port_Group with N ports and attach ACL via libovsdb
	t0 := time.Now()
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
	peak := getProcMem(env.northdPid)

	// Phase 3: Remove Port_Group & ACL & Ports
	t1 := time.Now()
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
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchLoadBalancer(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	sw := &LogicalSwitch{Name: "bench-ls"}
	opsSw, _ := env.ovsdbClient.Create(sw)
	resSw, _ := env.Transact(ctx, opsSw...)
	if len(resSw) > 0 {
		sw.UUID = resSw[0].UUID.GoUUID
	}
	env.Sync()
	env.Recompute()
	baseline := getProcMem(env.northdPid)

	// Phase 2: Add N Load Balancers and attach to switch via libovsdb
	t0 := time.Now()
	proto := "tcp"
	chunkSize := 300
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		chunkLen := end - i
		lbs := make([]model.Model, chunkLen)
		namedUUIDs := make([]string, chunkLen)
		for j := 0; j < chunkLen; j++ {
			idx := i + j
			namedUUID := fmt.Sprintf("lb_%d", idx)
			namedUUIDs[j] = namedUUID
			vip := fmt.Sprintf("10.96.%d.%d:80", (idx/250)&255, (idx%250)+1)
			backends := "10.244.0.1:8080,10.244.0.2:8080"
			lbs[j] = &LoadBalancer{
				UUID:     namedUUID,
				Name:     fmt.Sprintf("lb-%d", idx),
				Protocol: &proto,
				Vips:     map[string]string{vip: backends},
			}
		}
		opsLb, _ := env.ovsdbClient.Create(lbs...)
		mutateOps, _ := env.ovsdbClient.Where(sw).Mutate(sw, model.Mutation{
			Field:   &sw.LoadBalancer,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   namedUUIDs,
		})
		_, _ = env.Transact(ctx, append(opsLb, mutateOps...)...)
	}
	env.Sync()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6

	_, addLoop, addLflow := env.Recompute()
	peak := getProcMem(env.northdPid)

	// Phase 3: Remove N Load Balancers from switch and delete rows
	t1 := time.Now()
	clearSwOp := ovsdb.Operation{
		Op:    "update",
		Table: "Logical_Switch",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionEqual, "bench-ls")},
		Row: map[string]interface{}{
			"load_balancer": ovsdb.OvsSet{GoSet: []interface{}{}},
		},
	}
	delLbOp := ovsdb.Operation{
		Op:    "delete",
		Table: "Load_Balancer",
		Where: []ovsdb.Condition{ovsdb.NewCondition("name", ovsdb.ConditionNotEqual, "")},
	}
	_, _ = env.Transact(ctx, clearSwOp, delLbOp)
	env.Sync()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	_, delLoop, _ := env.Recompute()
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchFlow(alloc, ovsDir, jemallocConf string, n int) BenchResult {
	testDir := fmt.Sprintf("/tmp/opencode/ovs_flow_bench_%d_%d", os.Getpid(), n)
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0755)

	envVars := os.Environ()
	envVars = append(envVars, "OVS_RUNDIR="+testDir, "OVS_LOGDIR="+testDir)
	if alloc == "jemalloc" {
		if jlib := findJemallocLib(); jlib != "" {
			conf := jemallocConf
			if conf == "" {
				conf = defaultJemallocConf
			}
			envVars = append(envVars, "LD_PRELOAD="+jlib, "MALLOC_CONF="+conf)
		}
	}

	tool, _ := findBinary("ovsdb-tool", ovsDir)
	server, _ := findBinary("ovsdb-server", ovsDir)
	vsctl, _ := findBinary("ovs-vsctl", ovsDir)
	vswitchd, _ := findBinary("ovs-vswitchd", ovsDir)
	ofctl, _ := findBinary("ovs-ofctl", ovsDir)
	schema, _ := findSchema("vswitch.ovsschema")
	confDB := filepath.Join(testDir, "conf.db")
	sock := "unix:" + filepath.Join(testDir, "db.sock")

	_ = exec.Command(tool, "create", confDB, schema).Run()
	cmdServer := exec.Command(server, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(testDir, "ovsdb.pid"),
		"--remote=punix:"+filepath.Join(testDir, "db.sock"), confDB)
	cmdServer.Env = envVars
	_ = cmdServer.Run()

	_ = exec.Command(vsctl, "--db="+sock, "--no-wait", "init").Run()

	cmdVswitchd := exec.Command(vswitchd, "--enable-dummy=override", "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(testDir, "vswitchd.pid"), sock)
	cmdVswitchd.Env = envVars
	_ = cmdVswitchd.Run()

	pidData, _ := os.ReadFile(filepath.Join(testDir, "vswitchd.pid"))
	vpid, _ := strconv.Atoi(strings.TrimSpace(string(pidData)))

	_ = exec.Command(vsctl, "--db="+sock, "add-br", "br0", "--", "set", "bridge", "br0", "datapath_type=dummy").Run()

	baseline := getProcMem(vpid)

	// Phase 2: Add N OpenFlow rules
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		fmt.Fprintf(&buf, "table=%d,priority=%d,ip,nw_src=10.%d.%d.%d,actions=output:%d\n",
			i%8, (i%100)+10, (i>>16)&255, (i>>8)&255, i&255, (i%4)+1)
	}

	t0 := time.Now()
	cmdOf := exec.Command(ofctl, "add-flows", "br0", "-")
	cmdOf.Stdin = &buf
	cmdOf.Env = envVars
	_ = cmdOf.Run()
	addSec := float64(time.Since(t0).Microseconds()) / 1e6
	peak := getProcMem(vpid)

	// Phase 3: Remove N flows
	t1 := time.Now()
	cmdDel := exec.Command(ofctl, "del-flows", "br0")
	cmdDel.Env = envVars
	_ = cmdDel.Run()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	time.Sleep(decayWaitTime)
	postDel := getProcMem(vpid)

	killPidFile(filepath.Join(testDir, "vswitchd.pid"))
	killPidFile(filepath.Join(testDir, "ovsdb.pid"))
	_ = os.RemoveAll(testDir)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchDatapath(alloc, ovsDir, jemallocConf string, n int) BenchResult {
	testDir := fmt.Sprintf("/tmp/opencode/ovs_dp_bench_%d_%d", os.Getpid(), n)
	_ = os.RemoveAll(testDir)
	_ = os.MkdirAll(testDir, 0755)

	envVars := os.Environ()
	envVars = append(envVars, "OVS_RUNDIR="+testDir, "OVS_LOGDIR="+testDir)
	if alloc == "jemalloc" {
		if jlib := findJemallocLib(); jlib != "" {
			conf := jemallocConf
			if conf == "" {
				conf = defaultJemallocConf
			}
			envVars = append(envVars, "LD_PRELOAD="+jlib, "MALLOC_CONF="+conf)
		}
	}

	tool, _ := findBinary("ovsdb-tool", ovsDir)
	server, _ := findBinary("ovsdb-server", ovsDir)
	vsctl, _ := findBinary("ovs-vsctl", ovsDir)
	vswitchd, _ := findBinary("ovs-vswitchd", ovsDir)
	appctl, _ := findBinary("ovs-appctl", ovsDir)
	ofctl, _ := findBinary("ovs-ofctl", ovsDir)
	schema, _ := findSchema("vswitch.ovsschema")
	confDB := filepath.Join(testDir, "conf.db")
	sock := "unix:" + filepath.Join(testDir, "db.sock")
	ctl := filepath.Join(testDir, "vswitchd.ctl")

	_ = exec.Command(tool, "create", confDB, schema).Run()
	cmdServer := exec.Command(server, "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(testDir, "ovsdb.pid"),
		"--unixctl="+filepath.Join(testDir, "ovsdb.ctl"),
		"--remote=punix:"+filepath.Join(testDir, "db.sock"), confDB)
	cmdServer.Env = envVars
	_ = cmdServer.Run()

	_ = exec.Command(vsctl, "--db="+sock, "--no-wait", "init").Run()
	cmdVswitchd := exec.Command(vswitchd, "--enable-dummy=override", "--detach", "--no-chdir",
		"--pidfile="+filepath.Join(testDir, "vswitchd.pid"),
		"--unixctl="+ctl, sock)
	cmdVswitchd.Env = envVars
	_ = cmdVswitchd.Run()

	time.Sleep(100 * time.Millisecond)

	_ = exec.Command(vsctl, "--db="+sock, "add-br", "br0", "--", "set", "bridge", "br0", "datapath_type=dummy").Run()
	_ = exec.Command(vsctl, "--db="+sock, "add-port", "br0", "p1", "--", "set", "Interface", "p1", "type=dummy").Run()
	_ = exec.Command(vsctl, "--db="+sock, "add-port", "br0", "p2", "--", "set", "Interface", "p2", "type=dummy").Run()

	cmdOf := exec.Command(ofctl, "add-flow", "br0", "priority=100,in_port=1,actions=output:2")
	cmdOf.Env = envVars
	_ = cmdOf.Run()

	pidData, _ := os.ReadFile(filepath.Join(testDir, "vswitchd.pid"))
	vpid, _ := strconv.Atoi(strings.TrimSpace(string(pidData)))

	baseline := getProcMem(vpid)

	// Phase 2: Inject N packets through datapath via netdev-dummy/receive
	t0 := time.Now()
	pkt := "50540000000250540000000108004500001c00010000400164c70a0000010a0000020800f7ff00000000"
	chunkSize := 500
	for i := 0; i < n; i += chunkSize {
		end := i + chunkSize
		if end > n {
			end = n
		}
		args := []string{"-t", ctl, "netdev-dummy/receive", "p1"}
		for j := 0; j < (end - i); j++ {
			args = append(args, pkt)
		}
		_ = exec.Command(appctl, args...).Run()
	}
	addSec := float64(time.Since(t0).Microseconds()) / 1e6
	peak := getProcMem(vpid)

	// Phase 3: Flush datapath flows and measure memory reclamation
	t1 := time.Now()
	_ = exec.Command(appctl, "-t", ctl, "dpctl/del-flows").Run()
	delSec := float64(time.Since(t1).Microseconds()) / 1e6

	time.Sleep(decayWaitTime)
	postDel := getProcMem(vpid)

	killPidFile(filepath.Join(testDir, "vswitchd.pid"))
	killPidFile(filepath.Join(testDir, "ovsdb.pid"))
	_ = os.RemoveAll(testDir)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

func benchCombined(env *OVNEnvironment, n int) BenchResult {
	ctx := context.Background()
	env.Recompute()
	baseline := getProcMem(env.northdPid)

	t0 := time.Now()
	nSwitches := int(math.Max(1, float64(n/100)))
	nPorts := n
	nAcls := n
	nLbs := int(math.Max(1, float64(n/20)))

	// Router and Switches
	router := &LogicalRouter{Name: "r-main"}
	opsR, _ := env.ovsdbClient.Create(router)
	_, _ = env.Transact(ctx, opsR...)

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
			mac := fmt.Sprintf("00:00:%02x:%02x:00:02", (idx>>8)&255, idx&255)
			ip := fmt.Sprintf("10.%d.%d.%d", swIdx, (idx>>8)&255, idx&255)
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
			match := fmt.Sprintf("ip4.src == 10.0.%d.%d && tcp.dst == 80", (idx>>8)&255, idx&255)
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
	peak := getProcMem(env.northdPid)

	// Phase 3: Remove all via libovsdb
	t1 := time.Now()
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
	time.Sleep(decayWaitTime)
	postDel := getProcMem(env.northdPid)

	return BenchResult{
		BaselineRSS: baseline.VmRSS,
		AddSec:      round3(addSec),
		AddRate:     round1(float64(n) / math.Max(addSec, 0.001)),
		AddLoopMs:   addLoop,
		AddLflowMs:  addLflow,
		PeakRSS:     peak.VmHWM,
		DelSec:      round3(delSec),
		DelLoopMs:   delLoop,
		PostDelRSS:  postDel.VmRSS,
		RetainedRSS: round2(postDel.VmRSS - baseline.VmRSS),
	}
}

// ---------------------------------------------------------------------------
// Helpers & Report Generation
// ---------------------------------------------------------------------------

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func calcPct(vGlibc, vJemalloc float64) string {
	if vGlibc == 0 {
		return "N/A"
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
	lines = append(lines, "Each benchmark evaluates individual OVN/OVS objects, dataplane forwarding, or combined workloads across powers of 10 (`1, 10, 100, 1,000, 10,000, 100,000`) through 3 lifecycle phases using **native libovsdb** for database transactions:")
	lines = append(lines, "1. **Baseline (Idle)**: Clean process RSS before creating objects.")
	lines = append(lines, "2. **After Add (N)**: Time taken to commit $N$ objects via native OVSDB JSON-RPC, full recompute latency (`ovn-northd-loop` stopwatch), and peak RSS (`VmHWM`).")
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
		title := objTitles[obj]
		lines = append(lines, fmt.Sprintf("## %s", title), "")
		lines = append(lines, "| Scale (N) | Phase / Metric | glibc malloc | jemalloc | Relative Difference |")
		lines = append(lines, "|---|---|---|---|---|")

		var scalesList []int
		for sStr := range scalesData {
			if s, err := strconv.Atoi(sStr); err == nil {
				scalesList = append(scalesList, s)
			}
		}
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
			if obj == "datapath" {
				addUnit = "pkts/s"
				addLabel = "Forwarding Duration"
				rateLabel = "Forwarding Rate"
				delLabel = "Flow Flush Duration (`dpctl/del-flows`)"
			}

			lines = append(lines, fmt.Sprintf("| **%d** | Baseline RSS | %.2f MiB | %.2f MiB | %s |",
				nVal, g.BaselineRSS, j.BaselineRSS, calcPct(g.BaselineRSS, j.BaselineRSS)))
			lines = append(lines, fmt.Sprintf("| | %s | %.3f s | %.3f s | %s |",
				addLabel, g.AddSec, j.AddSec, calcPct(g.AddSec, j.AddSec)))
			lines = append(lines, fmt.Sprintf("| | %s | %s %s | %s %s | %s |",
				rateLabel, formatNum(g.AddRate), addUnit, formatNum(j.AddRate), addUnit, calcPct(g.AddRate, j.AddRate)))

			if obj != "flow" && obj != "datapath" {
				if len(g.ThreadLoops) > 0 {
					for tKey, gVal := range g.ThreadLoops {
						jVal := j.ThreadLoops[tKey]
						lines = append(lines, fmt.Sprintf("| | Recompute (`northd-loop`) (%s) | %.1f ms | %.1f ms | %s |",
							strings.ToUpper(tKey), gVal, jVal, calcPct(gVal, jVal)))
					}
				} else {
					lines = append(lines, fmt.Sprintf("| | Recompute (`ovn-northd-loop`) | %.1f ms | %.1f ms | %s |",
						g.AddLoopMs, j.AddLoopMs, calcPct(g.AddLoopMs, j.AddLoopMs)))
					lines = append(lines, fmt.Sprintf("| | Logical Flow (`lflow`) | %.1f ms | %.1f ms | %s |",
						g.AddLflowMs, j.AddLflowMs, calcPct(g.AddLflowMs, j.AddLflowMs)))
				}
			}

			lines = append(lines, fmt.Sprintf("| | Peak RSS (`VmHWM`) | %.2f MiB | %.2f MiB | %s |",
				g.PeakRSS, j.PeakRSS, calcPct(g.PeakRSS, j.PeakRSS)))
			lines = append(lines, fmt.Sprintf("| | %s | %.3f s | %.3f s | %s |",
				delLabel, g.DelSec, j.DelSec, calcPct(g.DelSec, j.DelSec)))

			if obj != "flow" && obj != "datapath" {
				lines = append(lines, fmt.Sprintf("| | Post-Delete Recompute | %.1f ms | %.1f ms | %s |",
					g.DelLoopMs, j.DelLoopMs, calcPct(g.DelLoopMs, j.DelLoopMs)))
			}

			lines = append(lines, fmt.Sprintf("| | Post-Delete RSS | %.2f MiB | %.2f MiB | %s |",
				g.PostDelRSS, j.PostDelRSS, calcPct(g.PostDelRSS, j.PostDelRSS)))
			lines = append(lines, fmt.Sprintf("| | **Retained Heap vs Baseline** | **%+.2f MiB** | **%+.2f MiB** | **%+.2f MiB** |",
				g.RetainedRSS, j.RetainedRSS, j.RetainedRSS-g.RetainedRSS))
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
			} else if obj == "datapath" {
				chartTitle := fmt.Sprintf("%s - Forwarding Rate", title)
				lines = append(lines, generateMermaidChart(chartTitle, "pkts/s", cats, gRate, jRate, "line"))
			} else if obj == "flow" {
				chartTitle := fmt.Sprintf("%s - Insertion Rate", title)
				lines = append(lines, generateMermaidChart(chartTitle, "ops/s", cats, gRate, jRate, "line"))
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
	scales       []int
	threads      []int
	objects      []string
	allocators   []string
	flowMax      int
	ovsDir       string
	ovnDir       string
	useSystem    bool
	jemallocConf string
	reportOnly   bool
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
	flag.Parse()

	cfg := &Config{
		flowMax:      *flowMaxFlag,
		ovsDir:       *ovsDirFlag,
		ovnDir:       *ovnDirFlag,
		useSystem:    *useSystemFlag,
		jemallocConf: *jemallocConfFlag,
		reportOnly:   *reportOnlyFlag,
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
	return sysInfo
}

func resolveBuildDirs(alloc string, cfg *Config) (string, string) {
	ovsDir := cfg.ovsDir
	ovnDir := cfg.ovnDir
	inContainer := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		inContainer = true
	}
	if !cfg.useSystem && !inContainer {
		if ovsDir == "" {
			cand := fmt.Sprintf("build/ovs-%s", alloc)
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				ovsDir = cand
			}
		}
		if ovnDir == "" {
			cand := fmt.Sprintf("build/ovn-%s", alloc)
			if info, err := os.Stat(cand); err == nil && info.IsDir() {
				ovnDir = cand
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
	case "combined":
		return benchCombined(env, n)
	default:
		return BenchResult{}
	}
}

func runSingleBenchmark(obj, alloc string, n int, cfg *Config) (BenchResult, error) {
	ovsDir, ovnDir := resolveBuildDirs(alloc, cfg)

	if obj == "flow" {
		return benchFlow(alloc, ovsDir, cfg.jemallocConf, n), nil
	}
	if obj == "datapath" {
		return benchDatapath(alloc, ovsDir, cfg.jemallocConf, n), nil
	}

	primaryT := cfg.threads[0]
	testDir := fmt.Sprintf("/tmp/opencode/bench_%s_%s_%d", alloc, obj, n)
	env, err := NewOVNEnvironment(alloc, cfg.jemallocConf, ovsDir, ovnDir, testDir, primaryT)
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
			tDir := fmt.Sprintf("/tmp/opencode/bench_%s_%s_%d_t%d", alloc, obj, n, t)
			envT, err := NewOVNEnvironment(alloc, cfg.jemallocConf, ovsDir, ovnDir, tDir, t)
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

// ---------------------------------------------------------------------------
// Main Orchestration
// ---------------------------------------------------------------------------

func main() {
	cfg := parseConfig()
	sysInfo := getSystemInfo()

	fmt.Printf("System: %s | %s cores | %s RAM\n", sysInfo["cpu_model"], sysInfo["cpu_cores"], sysInfo["total_ram"])
	fmt.Printf("Testing Objects (via native libovsdb): %v\n", cfg.objects)
	fmt.Printf("Scales: %v | Threads: %v\n\n", cfg.scales, cfg.threads)

	results := loadStoredResults()

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
				r, err := runSingleBenchmark(obj, alloc, n, cfg)
				if err != nil {
					fmt.Printf("    [%s] Benchmark error: %v\n", alloc, err)
					continue
				}

				results[obj][nStr][alloc] = r
				fmt.Printf("    [%s] Add: %.3fs (%.1f ops/s) | Loop: %.1fms | Peak RSS: %.2fMB | Retained: %+.2fMB\n",
					alloc, r.AddSec, r.AddRate, r.AddLoopMs, r.PeakRSS, r.RetainedRSS)
			}
		}
	}

	saveBenchmarkResults(results, sysInfo)
}
