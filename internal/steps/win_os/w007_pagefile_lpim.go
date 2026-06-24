package win_os

import (
	"github.com/yinstall/internal/runner"
)

func StepW007PagefileLPIM() *runner.Step {
	return &runner.Step{
		ID:          "W-007",
		Name:        "Pagefile & LPIM",
		Description: "Optional pagefile and lock pages in memory for SQL",
		Tags:        []string{"win-os", "win-os-install", "memory"},
		Optional:    true,
		PreCheck: func(ctx *runner.StepContext) error {
			if !ctx.GetParamBool("os_pagefile_enable", false) && !ctx.GetParamBool("os_lock_pages_in_memory", false) {
				return runner.NewStepSkippedError("pagefile/LPIM not requested")
			}
			return nil
		},
		Action: func(ctx *runner.StepContext) error {
			winOSLogPhase(ctx, "plan", "W-007 pagefile/LPIM")
			ctx.Logger.Info("W-007: pagefile/LPIM configuration documented; apply via GPO or manual SeLockMemoryPrivilege for production")
			return nil
		},
	}
}
