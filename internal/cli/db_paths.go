package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	commonos "github.com/yinstall/internal/common/os"
)

const dbDefaultBeginPort = 1688

func dbProductUser(osUser string) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		return "yashan"
	}
	return u
}

// defaultDBStageDir CLI 占位；远端真实路径由 commonos.ResolveStageDirParam 在建连后修正。
func defaultDBStageDir(osUser string, port int) string {
	return commonos.ConventionStageDir(dbProductUser(osUser), port)
}

func defaultDBInstallPath(osUser string, port int) string {
	u := dbProductUser(osUser)
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/yasdb_home", u)
	}
	return fmt.Sprintf("/data/%s/yasdb_home_%d", u, port)
}

func defaultDBDataPath(osUser string, port int) string {
	u := dbProductUser(osUser)
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/yasdb_data", u)
	}
	return fmt.Sprintf("/data/%s/yasdb_data_%d", u, port)
}

func defaultDBLogPath(osUser string, port int) string {
	u := dbProductUser(osUser)
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/data/%s/log", u)
	}
	return fmt.Sprintf("/data/%s/log_%d", u, port)
}

// applyDBUserPathDefaults 在用户未显式指定路径 flag 时，按 --os-user 与 --db-port 推导默认路径。
func applyDBUserPathDefaults(cmd *cobra.Command) {
	applyDBUserPathDefaultsFor(cmd, osUser, dbPort, &dbStageDir, &dbInstallPath, &dbDataPath, &dbLogPath, &dbClusterName)
}

// applyCleanDBUserPathDefaults 与 yinstall db 路径推断一致，供 clean --type db 使用。
func applyCleanDBUserPathDefaults(cmd *cobra.Command, user string, port int, stageDir, yasdbHome, yasdbData, yasdbLog, clusterName *string) {
	applyDBUserPathDefaultsFor(cmd, user, port, stageDir, yasdbHome, yasdbData, yasdbLog, clusterName)
}

func applyDBUserPathDefaultsFor(cmd *cobra.Command, osUser string, port int, stageDir, installPath, dataPath, logPath, clusterName *string) {
	user := dbProductUser(osUser)

	if !cmd.Flags().Changed("os-group") && cmd.Flags().Changed("os-user") {
		osGroup = user
	}
	if stageDir != nil && !cmd.Flags().Changed("db-stage-dir") {
		*stageDir = defaultDBStageDir(user, port)
	}
	homeFlagChanged := cmd.Flags().Changed("db-home-path") || cmd.Flags().Changed("yasdb-home")
	dataFlagChanged := cmd.Flags().Changed("db-data-path") || cmd.Flags().Changed("yasdb-data")
	logFlagChanged := cmd.Flags().Changed("db-log-path") || cmd.Flags().Changed("yasdb-log")
	clusterFlagChanged := cmd.Flags().Changed("db-cluster-name") || cmd.Flags().Changed("cluster-name")
	if installPath != nil && !homeFlagChanged {
		*installPath = defaultDBInstallPath(user, port)
	}
	if dataPath != nil && !dataFlagChanged {
		*dataPath = defaultDBDataPath(user, port)
	}
	if logPath != nil && !logFlagChanged {
		*logPath = defaultDBLogPath(user, port)
	}
	if port != dbDefaultBeginPort && clusterName != nil && !clusterFlagChanged {
		*clusterName = commonos.DefaultDBClusterName(port)
	}
}
