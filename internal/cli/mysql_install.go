package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	commonmysql "github.com/yinstall/internal/common/mysql"
	"github.com/yinstall/internal/logging"
)

var mysqlInstallCmd = &cobra.Command{
	Use:          "install",
	Short:        "Install MySQL database",
	Long:         `Install MySQL (standalone) with optional OS baseline preparation.`,
	RunE:         runMysqlInstall,
	SilenceUsage: true,
}

func init() {
	mysqlInstallCmd.Flags().BoolVar(&mysqlSkipOS, "skip-os", false, "Skip OS baseline preparation")
	registerMysqlInstallFlags(mysqlInstallCmd)
}

func runMysqlInstall(cmd *cobra.Command, args []string) error {
	if err := validatePorts(map[string]int{"--mysql-port": mysqlPort}); err != nil {
		return err
	}

	applyInstallArchiveDefault(cmd)
	flags := GetGlobalFlags()
	if len(flags.Targets) == 0 {
		flags.Local = true
		flags.Targets = []string{"localhost"}
	} else {
		flags.Local = false
	}
	applyMysqlPlatformDefaults(cmd, &flags, &mysqlBase)
	if flags.ListSteps {
		PrintMySQLInstallStepCatalog(mysqlSkipOS)
		return nil
	}

	if err := validateMysqlInstallStage(mysqlStage, mysqlRootPassword, mysqlPackage, mysqlVersion, flags.DryRun, flags.Precheck); err != nil {
		return err
	}
	stage, err := commonmysql.ParseStage(mysqlStage)
	if err != nil {
		return err
	}

	rid := flags.RunID
	if rid == "" {
		rid = fmt.Sprintf("mysql-%s", time.Now().Format("20060102-150405"))
	}

	logger, err := logging.NewLogger(rid, flags.LogDir, AppVersion, AppAuthor, AppContact)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	logger.Info("Starting MySQL installation (RunID: %s)", rid)
	logger.Info("Targets: %v", flags.Targets)

	params := buildMysqlParams(len(flags.Targets), flags, stage)
	params["sudo"] = flags.UseSudo
	params["ssh_port"] = flags.SSHPort
	params["local_mode"] = flags.Local

	if err := RunMysqlInstallOnHosts(cmd, flags, logger, stage, params); err != nil {
		return err
	}
	logger.Info("MySQL installation completed successfully")
	return nil
}
