package ycm

import (
	"fmt"
	"strings"
)

// InstallLayout 描述一次 YCM 安装/清理的有效路径与端口。
type InstallLayout struct {
	InstallDir         string
	YCMHome            string
	WebPort            int
	PrometheusPort     int
	LokiHTTPPort       int
	LokiGRPCPort       int
	YasdbExporterPort  int
	InstallDirInferred bool
}

// InstallLayoutInput 解析 layout 的输入；Explicit 标志对应 cobra Flags().Changed(...)。
type InstallLayoutInput struct {
	WebPort               int
	InstallDir            string
	InstallDirExplicit    bool
	YCMHome               string
	YCMHomeExplicit       bool
	PrometheusPort        int
	PrometheusExplicit    bool
	LokiHTTPPort          int
	LokiHTTPExplicit      bool
	LokiGRPCPort          int
	LokiGRPCExplicit      bool
	YasdbExporterPort     int
	YasdbExporterExplicit bool
}

// ResolveInstallLayout 解析 install/clean 共用的 YCM 路径与副端口。
//
// 非默认 Web 端口且未显式 --ycm-install-dir 时：install_dir=/opt/ycm_<port>，ycm_home={install_dir}/ycm。
// clean 侧若显式 --ycm-home，则以 YCMHome 为准并反推 install_dir（用于 systemd unit 名推导）。
func ResolveInstallLayout(in InstallLayoutInput) InstallLayout {
	webPort := in.WebPort
	if webPort <= 0 {
		webPort = defaultYCMPort
	}

	var installDir string
	var ycmHome string
	inferred := false

	if in.YCMHomeExplicit {
		ycmHome = strings.TrimRight(strings.TrimSpace(in.YCMHome), "/")
		if ycmHome == "" {
			ycmHome = defaultInstallDir + "/ycm"
		}
		installDir = InstallDirFromYCMHome(ycmHome)
	} else {
		installDir = strings.TrimRight(strings.TrimSpace(in.InstallDir), "/")
		if installDir == "" {
			installDir = defaultInstallDir
		}
		if webPort != defaultYCMPort && !in.InstallDirExplicit {
			installDir = fmt.Sprintf("/opt/ycm_%d", webPort)
			inferred = true
		}
		ycmHome = installDir + "/ycm"
	}

	prom := in.PrometheusPort
	lokiHTTP := in.LokiHTTPPort
	lokiGRPC := in.LokiGRPCPort
	exporter := in.YasdbExporterPort
	if !in.PrometheusExplicit {
		prom = webPort + 1
	}
	if !in.LokiHTTPExplicit {
		lokiHTTP = webPort + 2
	}
	if !in.LokiGRPCExplicit {
		lokiGRPC = webPort + 3
	}
	if !in.YasdbExporterExplicit {
		exporter = webPort + 4
	}

	return InstallLayout{
		InstallDir:         installDir,
		YCMHome:            ycmHome,
		WebPort:            webPort,
		PrometheusPort:     prom,
		LokiHTTPPort:       lokiHTTP,
		LokiGRPCPort:       lokiGRPC,
		YasdbExporterPort:  exporter,
		InstallDirInferred: inferred,
	}
}

// FormatCleanRemediation 返回可复制执行的 YCM clean 命令（含 port/home）。
func FormatCleanRemediation(host string, layout InstallLayout) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "<host>"
	}
	return fmt.Sprintf("yinstall clean --type ycm -F -t %s --ycm-port %d --ycm-home %s",
		host, layout.WebPort, layout.YCMHome)
}

// FormatYCMWipeCommand 返回仅 wipe 安装目录的 yinstall ycm -f G-003 命令（非默认 layout 时带 port/install-dir）。
func FormatYCMWipeCommand(host string, layout InstallLayout) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "<host>"
	}
	cmd := fmt.Sprintf("yinstall ycm -t %s -f G-003", host)
	if layout.WebPort != defaultYCMPort {
		cmd += fmt.Sprintf(" --ycm-port %d", layout.WebPort)
	}
	if installDirExplicit(layout) {
		cmd += fmt.Sprintf(" --ycm-install-dir %s", layout.InstallDir)
	}
	return cmd
}

func installDirExplicit(layout InstallLayout) bool {
	if layout.InstallDirInferred {
		return false
	}
	installDir := strings.TrimRight(strings.TrimSpace(layout.InstallDir), "/")
	return installDir != "" && installDir != defaultInstallDir
}

// YCMCleanDeletePath 返回 clean 应 rm -rf 的根路径。
// 当 ycm_home 为 {install_dir}/ycm 且 install_dir 非 /opt 时，默认删除整个 install_dir（含 ycm-init 等父级产物）；
// 若仅显式 --ycm-home 而未显式 --ycm-install-dir，则只删 ycm_home，避免误删父目录其它内容。
func YCMCleanDeletePath(installDir, ycmHome string, homeExplicit, installDirExplicit bool) string {
	installDir = strings.TrimRight(strings.TrimSpace(installDir), "/")
	ycmHome = strings.TrimRight(strings.TrimSpace(ycmHome), "/")
	if installDir == "" {
		installDir = InstallDirFromYCMHome(ycmHome)
	}
	if ycmHome == "" {
		ycmHome = installDir + "/ycm"
	}
	canonicalHome := installDir + "/ycm"
	if ycmHome != canonicalHome || installDir == defaultInstallDir {
		return ycmHome
	}
	if homeExplicit && !installDirExplicit {
		return ycmHome
	}
	return installDir
}
