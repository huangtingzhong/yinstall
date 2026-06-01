package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const dbDefaultBeginPort = 1688

func dbProductUser(osUser string) string {
	u := strings.TrimSpace(osUser)
	if u == "" {
		return "yashan"
	}
	return u
}

// defaultDBStageDir 与 standby.DefaultPrimaryStageDir 规则一致。
func defaultDBStageDir(osUser string, port int) string {
	u := dbProductUser(osUser)
	if port == dbDefaultBeginPort {
		return fmt.Sprintf("/home/%s/install", u)
	}
	return fmt.Sprintf("/home/%s/install_%d", u, port)
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
	user := dbProductUser(osUser)

	if !cmd.Flags().Changed("os-group") && cmd.Flags().Changed("os-user") {
		osGroup = user
	}
	if !cmd.Flags().Changed("db-stage-dir") {
		dbStageDir = defaultDBStageDir(user, dbPort)
	}
	if !cmd.Flags().Changed("db-home-path") {
		dbInstallPath = defaultDBInstallPath(user, dbPort)
	}
	if !cmd.Flags().Changed("db-data-path") {
		dbDataPath = defaultDBDataPath(user, dbPort)
	}
	if !cmd.Flags().Changed("db-log-path") {
		dbLogPath = defaultDBLogPath(user, dbPort)
	}
	if dbPort != dbDefaultBeginPort && !cmd.Flags().Changed("db-cluster-name") {
		dbClusterName = fmt.Sprintf("yashandb_%d", dbPort)
	}
}
