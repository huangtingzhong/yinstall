package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mysqlCmd = &cobra.Command{
	Use:   "mysql",
	Short: "MySQL install and replication",
	Long: `MySQL subcommands:
  install  Install MySQL (standalone) with optional OS baseline
  standby  Add replica instance to an existing primary MySQL

Use global -l/--list-steps on install or standby to list steps.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := GetGlobalFlags()
		if flags.ListSteps {
			PrintMySQLStepCatalog(mysqlSkipOS)
			return nil
		}
		return fmt.Errorf("specify subcommand: install or standby (e.g. yinstall mysql install -t HOST)")
	},
	SilenceUsage: true,
}

func init() {
	mysqlCmd.AddCommand(mysqlInstallCmd)
	mysqlCmd.AddCommand(mysqlStandbyCmd)
}
