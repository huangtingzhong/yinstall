package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mssqlCmd = &cobra.Command{
	Use:   "mssql",
	Short: "SQL Server install and HA (Windows)",
	Long: `MSSQL subcommands:
  install   Install SQL Server with Windows OS baseline (W-*) and MS-* steps
  mirror    Configure database mirroring (M-*); --primary-host X -t Y (add),
            subcommand 'remove' (SET PARTNER OFF), or -t PRIMARY,REPLICA (rebuild)
  ag        Configure Always On availability group (A-*); requires pre-existing
            WSFC cluster; same add/remove/rebuild modes as mirror

Use global -l/--list-steps on install, mirror, or ag to list steps.
Install stages (--stage): all/a, software/s
Mirror/AG stages (--stage): all/a (install replica + HA), software/s (install only), ha/h (HA only)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flags := GetGlobalFlags()
		if flags.ListSteps {
			PrintMssqlInstallStepCatalog(mssqlSkipOS)
			return nil
		}
		return fmt.Errorf("specify subcommand: install, mirror, or ag (e.g. yinstall mssql install -t HOST)")
	},
	SilenceUsage: true,
}

func init() {
	mssqlCmd.AddCommand(mssqlInstallCmd)
	mssqlCmd.AddCommand(mssqlMirrorCmd)
	mssqlCmd.AddCommand(mssqlAGCmd)
}
