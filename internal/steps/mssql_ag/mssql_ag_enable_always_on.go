package mssql_ag

import (
	"fmt"
	"time"

	commonmssql "github.com/yinstall/internal/common/mssql"
	"github.com/yinstall/internal/runner"
)

func stepEnableAlwaysOn() *runner.Step {
	return &runner.Step{
		Name:        "Enable Always On",
		Description: "Enable HADR via WMI ChangeHadrServiceSetting (Enable-DbaAgHadr -Force)",
		Tags:        []string{"mssql-ha", "feature"},
		PreCheck: func(ctx *runner.StepContext) error {
			inst := commonmssql.ResolvedInstanceName(ctx)
			enabled, err := commonmssql.HadrEnabledFromWmi(ctx, inst)
			if err != nil {
				return fmt.Errorf("A-010: %w", err)
			}
			if enabled && !ctx.IsForceStep() && !ctx.ForceAll {
				return runner.NewStepSkippedError("A-010: Always On already enabled (WMI IsHadrEnabled)")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			inst := commonmssql.ResolvedInstanceName(ctx)
			enabled, err := commonmssql.HadrEnabledFromWmi(ctx, inst)
			if err != nil {
				return fmt.Errorf("A-010: %w", err)
			}
			if enabled && !ctx.IsForceStep() && !ctx.ForceAll {
				ctx.Logger.Info("A-010: Always On already enabled (WMI IsHadrEnabled)")
				return nil
			}
			mshLogPhase(ctx, "hadr-enable-start", commonmssql.SqlWmiInstanceName(inst))
			if err := commonmssql.EnableDbaAgHadr(ctx, inst); err != nil {
				return fmt.Errorf("A-010: %w", err)
			}
			if err := waitForHadrEnabledWmi(ctx, inst, 120*time.Second); err != nil {
				return err
			}
			ctx.Logger.Info("A-010: Always On enabled (WMI IsHadrEnabled=1)")
			return nil
		},
	}
}

func waitForHadrEnabledWmi(ctx *runner.StepContext, instance string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		enabled, err := commonmssql.HadrEnabledFromWmi(ctx, instance)
		if err != nil {
			return err
		}
		if enabled {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("A-010: WMI IsHadrEnabled still false after %s", timeout)
}

func requireHadrEnabledWmi(ctx *runner.StepContext, stepID string) error {
	inst := commonmssql.ResolvedInstanceName(ctx)
	enabled, err := commonmssql.HadrEnabledFromWmi(ctx, inst)
	if err != nil {
		return fmt.Errorf("%s: %w", stepID, err)
	}
	if !enabled {
		return fmt.Errorf("%s: Always On (HADR) not enabled on instance %s; run A-010 (Enable-DbaAgHadr)", stepID, commonmssql.SqlWmiInstanceName(inst))
	}
	return nil
}
