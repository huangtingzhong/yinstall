// s008_net_bench.go - 网络压测（S-08）
// 单节点/本地：ping 写入 <host>/net/ping.txt。
// YAC（>=2 个 -t）：各节点 ping 其余节点（<host>/net/ping_<peer>.txt），
// Phase 2 结束后由 CLI 调用 RunIperf3YAC（首节点服务端，其余各节点作客户端）。
package stressos

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yinstall/internal/runner"
)

const defaultIperf3Seconds = 60

// stepNetBench 返回 S-08 步骤：per-host ping；YAC 为节点间互 ping。
func stepNetBench() *runner.Step {
	return &runner.Step{
		Name:     "Network benchmark (ping latency)",
		Optional: true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !getBool(ctx, "stress_net", false) {
				return fmt.Errorf("network benchmark disabled (--net=false)")
			}
			mode := getStr(ctx, "stress_net_mode", "ping")
			if mode == stressNetModeYAC {
				if len(s08YACPeerTargets(ctx)) == 0 {
					return fmt.Errorf("YAC ping: no peer targets for host %s", ctx.Executor.Host())
				}
				return nil
			}
			if getStr(ctx, "ping_target", "") == "" {
				return fmt.Errorf("no ping target available; skipping network benchmark")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			if getStr(ctx, "stress_net_mode", "ping") == stressNetModeYAC {
				return s08RunYACPingMesh(ctx)
			}
			return s08RunPingBench(ctx, getStr(ctx, "ping_target", ""))
		},
	}
}

const stressNetModeYAC = "yac"

func s08YACPeerTargets(ctx *runner.StepContext) []string {
	self := strings.TrimSpace(ctx.Executor.Host())
	seen := make(map[string]bool)
	var peers []string
	for _, p := range ctx.GetParamStringSlice("yac_targets") {
		p = strings.TrimSpace(p)
		if p == "" || p == self || seen[p] {
			continue
		}
		seen[p] = true
		peers = append(peers, p)
	}
	if len(peers) > 0 {
		return peers
	}
	for _, th := range ctx.TargetHosts {
		h := strings.TrimSpace(th.Host)
		if h == "" || h == self || seen[h] {
			continue
		}
		seen[h] = true
		peers = append(peers, h)
	}
	return peers
}

func s08RunYACPingMesh(ctx *runner.StepContext) error {
	peers := s08YACPeerTargets(ctx)
	self := ctx.Executor.Host()

	stressLogPhase(ctx, "plan",
		fmt.Sprintf("mode=yac-ping host=%s peers=%d targets=%v", self, len(peers), peers))

	var firstErr error
	for i, peer := range peers {
		stressLogPhase(ctx, "op-start", fmt.Sprintf("yac-ping %d/%d peer=%s", i+1, len(peers), peer))
		if err := s08RunPingBench(ctx, peer); err != nil && firstErr == nil {
			firstErr = err
		}
		stressLogPhase(ctx, "op-done", fmt.Sprintf("yac-ping peer=%s", peer))
	}
	ctx.Logger.Info("[S-08] YAC ping mesh completed for %s (%d peers)", self, len(peers))
	return firstErr
}

func s08RunPingBench(ctx *runner.StepContext, pingTarget string) error {
	hostDir := stressHostDir(ctx)
	netDir := filepath.Join(hostDir, "net")

	safePeer := strings.NewReplacer(":", "_", "/", "_").Replace(pingTarget)
	destName := "ping.txt"
	if getStr(ctx, "stress_net_mode", "ping") == stressNetModeYAC {
		destName = "ping_" + safePeer + ".txt"
	}

	ctx.Logger.Info("[S-08] network benchmark: ping target=%s", pingTarget)

	stressLogPhase(ctx, "plan",
		fmt.Sprintf("mode=ping target=%s count=100 interval=0.1s (~10s+ wall)", pingTarget))

	benchTimeout := 60 * time.Second
	pingCmd := fmt.Sprintf("ping -c 100 -i 0.1 -q %s 2>&1", pingTarget)

	stressLogPhase(ctx, "bench-start",
		fmt.Sprintf("target=%s timeout_cap=%ds cmd=%s",
			pingTarget, int(benchTimeout.Seconds()), truncateCmdForLog(pingCmd)))

	wallStart := time.Now()
	r, err := stressExecute(ctx, pingCmd, false, benchTimeout)
	wallDur := time.Since(wallStart)

	pingTxt := fmt.Sprintf("=== ping -c 100 -i 0.1 %s (from %s) ===\n", pingTarget, ctx.Executor.Host())
	body := ""
	exitCode := -1
	if r != nil {
		body = r.GetStdout()
		exitCode = r.GetExitCode()
		pingTxt += body
	}
	summary := stressPingSummary(body)
	if err != nil {
		appendWarning(ctx, fmt.Sprintf("ping %s failed: %v", pingTarget, err))
		pingTxt += "\nERROR: " + err.Error()
		stressLogPhase(ctx, "bench-fail",
			fmt.Sprintf("target=%s wall=%s exit=%d err=%v %s",
				pingTarget, wallDur.Round(time.Millisecond), exitCode, err, summary))
	} else {
		stressLogPhase(ctx, "bench-done",
			fmt.Sprintf("target=%s wall=%s exit=%d %s",
				pingTarget, wallDur.Round(time.Millisecond), exitCode, summary))
	}

	destPath := filepath.Join(netDir, destName)
	if err2 := writeTextFile(destPath, pingTxt+"\n"); err2 != nil {
		appendWarning(ctx, "write "+destName+": "+err2.Error())
	}

	return err
}

// RunIperf3YAC runs iperf3 server on serverCtx host; each clientCtx runs iperf3 -c sequentially.
// Results under <output_dir>/yac/net/ (iperf3_client_<host>.txt + iperf3_meta.json).
func RunIperf3YAC(serverCtx *runner.StepContext, clientCtxs []*runner.StepContext) error {
	if len(clientCtxs) == 0 {
		return fmt.Errorf("no iperf3 clients")
	}
	outDir := stressRootDir(serverCtx)
	if outDir == "" {
		return fmt.Errorf("output_dir not set")
	}
	serverHost := serverCtx.GetParamString("iperf3_server_host", serverCtx.Executor.Host())
	duration := serverCtx.GetParamInt("iperf3_time", defaultIperf3Seconds)
	if duration <= 0 {
		duration = defaultIperf3Seconds
	}
	port := serverCtx.GetParamInt("iperf3_port", 5201)
	if port <= 0 {
		port = 5201
	}

	clientHosts := make([]string, 0, len(clientCtxs))
	for _, c := range clientCtxs {
		clientHosts = append(clientHosts, c.Executor.Host())
	}

	yacDir := filepath.Join(outDir, "yac", "net")
	stopCmd := "pkill -x iperf3 2>/dev/null || true"
	stressLogPhase(serverCtx, "plan",
		fmt.Sprintf("mode=iperf3 server=%s clients=%v port=%d duration=%ds dir=%s",
			serverHost, clientHosts, port, duration, yacDir))

	startCmd := fmt.Sprintf("pkill -x iperf3 2>/dev/null || true; iperf3 -s -p %d -D 2>&1", port)
	stressLogPhase(serverCtx, "bench-start", fmt.Sprintf("iperf3-server port=%d", port))
	if _, err := stressExecute(serverCtx, startCmd, false, 30*time.Second); err != nil {
		appendWarning(serverCtx, fmt.Sprintf("iperf3 server start: %v", err))
		return err
	}

	serverIP, err := s08ResolveHostIP(clientCtxs[0], serverHost)
	if err != nil {
		_, _ = stressExecute(serverCtx, stopCmd, false, 15*time.Second)
		return fmt.Errorf("resolve iperf3 server IP for %s: %w", serverHost, err)
	}

	_, _ = stressExecute(clientCtxs[0], "sleep 2", false, 10*time.Second)
	clientTimeout := stressBenchTimeout(time.Duration(duration) * time.Second)
	clientCmdTpl := "iperf3 -c %s -p %d -t %d -J 2>&1 || iperf3 -c %s -p %d -t %d 2>&1"

	var clientResults []map[string]interface{}
	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("=== iperf3 YAC ===\nserver_host: %s\nserver_ip: %s\nport: %d\nduration: %ds\nclients: %v\n\n",
		serverHost, serverIP, port, duration, clientHosts))

	var firstErr error
	for i, clientCtx := range clientCtxs {
		clientHost := clientCtx.Executor.Host()
		stressLogPhase(clientCtx, "op-start", fmt.Sprintf("iperf3-client %d/%d -> %s", i+1, len(clientCtxs), serverHost))
		clientCmd := fmt.Sprintf(clientCmdTpl, serverIP, port, duration, serverIP, port, duration)
		stressLogPhase(clientCtx, "bench-start",
			fmt.Sprintf("iperf3-client server=%s (%s) duration=%ds", serverHost, serverIP, duration))
		clientResult, clientErr := stressExecute(clientCtx, clientCmd, false, clientTimeout)

		clientOut := ""
		clientExit := -1
		if clientResult != nil {
			clientOut = clientResult.GetStdout()
			clientExit = clientResult.GetExitCode()
		}
		entry := map[string]interface{}{
			"client_host": clientHost,
			"server_host": serverHost,
			"server_ip":   serverIP,
			"exit_code":   clientExit,
		}
		if clientErr != nil {
			entry["error"] = clientErr.Error()
			appendWarning(clientCtx, fmt.Sprintf("iperf3 client %s: %v", clientHost, clientErr))
			stressLogPhase(clientCtx, "bench-fail", fmt.Sprintf("exit=%d err=%v", clientExit, clientErr))
			if firstErr == nil {
				firstErr = clientErr
			}
		} else {
			stressLogPhase(clientCtx, "bench-done", fmt.Sprintf("exit=%d", clientExit))
		}
		clientResults = append(clientResults, entry)

		report := fmt.Sprintf("=== iperf3 client %s -> server %s (%s) ===\nport: %d\nduration: %ds\n\n%s\n",
			clientHost, serverHost, serverIP, port, duration, clientOut)
		if clientErr != nil {
			report += "ERROR: " + clientErr.Error() + "\n"
		}
		safeClient := strings.NewReplacer(":", "_", "/", "_").Replace(clientHost)
		perClientFile := filepath.Join(yacDir, "iperf3_client_"+safeClient+".txt")
		if err := writeTextFile(perClientFile, report); err != nil {
			appendWarning(clientCtx, "write "+filepath.Base(perClientFile)+": "+err.Error())
		}
		summary.WriteString(fmt.Sprintf("--- client %s (exit=%d) ---\n", clientHost, clientExit))
		if clientErr != nil {
			summary.WriteString("ERROR: " + clientErr.Error() + "\n")
		}
		stressLogPhase(clientCtx, "op-done", fmt.Sprintf("iperf3-client %s", clientHost))
	}

	_, _ = stressExecute(serverCtx, stopCmd, false, 15*time.Second)

	if err := writeTextFile(filepath.Join(yacDir, "iperf3_report.txt"), summary.String()); err != nil {
		appendWarning(serverCtx, "write iperf3_report.txt: "+err.Error())
	}
	if err := writeJSON(filepath.Join(yacDir, "iperf3_meta.json"), map[string]interface{}{
		"server_host": serverHost,
		"server_ip":   serverIP,
		"port":        port,
		"duration_s":  duration,
		"clients":     clientResults,
	}); err != nil {
		appendWarning(serverCtx, "write iperf3_meta.json: "+err.Error())
	}

	serverCtx.SetResult("iperf3_yac_done", true)
	return firstErr
}

func s08ResolveHostIP(ctx *runner.StepContext, host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("empty host")
	}
	parts := strings.Split(host, ".")
	if len(parts) == 4 {
		return host, nil
	}
	cmd := fmt.Sprintf("getent ahostsv4 %s 2>/dev/null | awk '{print $1; exit}'", host)
	r, err := stressExecute(ctx, cmd, false, 15*time.Second)
	if err == nil && r != nil {
		ip := strings.TrimSpace(r.GetStdout())
		if ip != "" {
			return ip, nil
		}
	}
	r2, _ := stressExecute(ctx, "hostname -I 2>/dev/null | awk '{print $1}'", false, 10*time.Second)
	if r2 != nil {
		ip := strings.TrimSpace(r2.GetStdout())
		if ip != "" {
			return ip, nil
		}
	}
	return host, nil
}
