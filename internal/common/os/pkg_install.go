package os

import (
	"fmt"
	"strings"

	"github.com/yinstall/internal/runner"
)

// StillMissingPackages 返回 packages 中仍未安装的包名列表。
func StillMissingPackages(ctx *runner.StepContext, packages, pkgManager string) []string {
	var still []string
	for _, pkg := range strings.Fields(packages) {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if !IsDepPackageSatisfied(ctx, pkg, pkgManager) {
			still = append(still, pkg)
		}
	}
	return still
}

type installAttempt struct {
	exitCode int
	stderr   string
	stdout   string
	err      error
}

func runInstallCmd(ctx *runner.StepContext, cmd string) installAttempt {
	var a installAttempt
	result, err := ctx.Execute(cmd, true)
	a.err = err
	if result != nil {
		a.exitCode = result.GetExitCode()
		a.stderr = result.GetStderr()
		a.stdout = result.GetStdout()
	}
	return a
}

func installOutput(a installAttempt) string {
	if strings.TrimSpace(a.stderr) != "" {
		return a.stderr
	}
	return a.stdout
}

func logInstallAttemptPhase(ctx *runner.StepContext, phase string, cmd string, a installAttempt, still []string) {
	if ctx == nil {
		return
	}
	msg := fmt.Sprintf("cmd=%s exit=%d", cmd, a.exitCode)
	if len(still) > 0 {
		msg += fmt.Sprintf(" still_missing=%s", strings.Join(still, ","))
	}
	if a.err != nil {
		msg += fmt.Sprintf(" err=%s", runner.TruncateForLog(a.err.Error(), 120))
	} else if a.exitCode != 0 {
		msg += fmt.Sprintf(" output=%s", runner.TruncateForLog(installOutput(a), 160))
	}
	ctx.LogPhase(phase, msg)
}

func shouldFallbackToLocalMedia(ctx *runner.StepContext, attempt installAttempt, stillMissing []string) bool {
	// local 模式本身已是介质源，不再二次 fallback
	if IsLocalYumMode(GetYumMode(ctx)) {
		return false
	}
	if len(stillMissing) > 0 {
		return true
	}
	return IsRepoClassInstallError(installOutput(attempt), attempt.exitCode)
}

func fallbackTriggerReason(attempt installAttempt, stillMissing []string) string {
	if len(stillMissing) > 0 {
		return fmt.Sprintf("still_missing=%s", strings.Join(stillMissing, ","))
	}
	if IsRepoClassInstallError(installOutput(attempt), attempt.exitCode) {
		return "repo_class_error"
	}
	return fmt.Sprintf("exit=%d", attempt.exitCode)
}

func installViaSystem(ctx *runner.StepContext, pkgManager, packages string, isRHEL8 bool) (string, installAttempt) {
	cmd := BuildInstallCmd(pkgManager, "", packages, isRHEL8)
	return cmd, runInstallCmd(ctx, cmd)
}

func installViaLocalMedia(ctx *runner.StepContext, pkgManager, packages string, isRHEL8 bool) (string, installAttempt) {
	if err := PrepareLocalMediaRepo(ctx); err != nil {
		return "", installAttempt{err: err}
	}
	cmd := BuildInstallCmd(pkgManager, YumModeLocal, packages, isRHEL8)
	return cmd, runInstallCmd(ctx, cmd)
}

func installViaHTTP(ctx *runner.StepContext, pkgManager, packages string, isRHEL8 bool) (string, installAttempt) {
	if err := PrepareHTTPRepo(ctx); err != nil {
		return "", installAttempt{err: err}
	}
	cmd := BuildInstallCmd(pkgManager, YumModeHTTP, packages, isRHEL8)
	return cmd, runInstallCmd(ctx, cmd)
}

func tryLocalMediaFallback(ctx *runner.StepContext, pkgManager, pkgs string, isRHEL8 bool, trigger string) error {
	available, mediaReason := localMediaAvailability(ctx, true)
	if !available {
		ctx.LogPhase("fallback-unavailable", fmt.Sprintf("trigger=%s media_reason=%s", trigger, mediaReason))
		return fmt.Errorf("no matching local media (optical drive or ISO) is available for auto fallback")
	}
	ctx.LogPhase("fallback-start", fmt.Sprintf(
		"trigger=%s media=%s action=retry_local_media",
		trigger, mediaReason,
	))
	ctx.LogPhase("local-install-start", fmt.Sprintf("packages=%s via=fallback", pkgs))
	localCmd, localAttempt := installViaLocalMedia(ctx, pkgManager, pkgs, isRHEL8)
	stillAfter := StillMissingPackages(ctx, pkgs, pkgManager)
	if localAttempt.err == nil && localAttempt.exitCode == 0 && len(stillAfter) == 0 {
		ctx.LogPhase("local-install-done", fmt.Sprintf("packages=%s via=fallback", pkgs))
		ctx.LogPhase("fallback-done", fmt.Sprintf("packages=%s result=installed_via_local_media", pkgs))
		return nil
	}
	logInstallAttemptPhase(ctx, "local-install-fail", localCmd, localAttempt, stillAfter)
	ctx.LogPhase("fallback-fail", fmt.Sprintf("packages=%s still_missing=%s", pkgs, strings.Join(stillAfter, ",")))
	return finalizeInstallError(ctx, "local media fallback install", localAttempt, stillAfter)
}

func finalizeInstallError(ctx *runner.StepContext, prefix string, attempt installAttempt, still []string) error {
	ignore := ctx.GetParamBool("os_ignore_install_errors", false)
	if len(still) > 0 {
		msg := fmt.Sprintf("%s: packages still not installed: %s", prefix, strings.Join(still, ", "))
		if ignore {
			ctx.Logger.Warn("%s (--os-ignore-install-errors)", msg)
			return nil
		}
		return fmt.Errorf("%s", msg)
	}
	if attempt.err != nil {
		if ignore {
			ctx.Logger.Warn("%s: %v (--os-ignore-install-errors)", prefix, attempt.err)
			return nil
		}
		return fmt.Errorf("%s: %w", prefix, attempt.err)
	}
	if attempt.exitCode != 0 {
		msg := fmt.Sprintf("%s: command failed with exit code %d: %s", prefix, attempt.exitCode, installOutput(attempt))
		if ignore {
			ctx.Logger.Warn("%s (--os-ignore-install-errors)", msg)
			return nil
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// InstallPackages 安装 packages（空格分隔）。
// 空 os_yum_mode：先系统源；失败且有 ISO 时 fallback。
// local：直接本地介质。http（IP/URL）：写自定义 repo 后安装，失败可再 fallback ISO。
func InstallPackages(ctx *runner.StepContext, packages string) error {
	packages = strings.TrimSpace(packages)
	if packages == "" {
		return nil
	}

	ensureOSInfo(ctx)
	pkgManager := GetPkgManager(ctx.OSInfo)
	rawMode := GetYumModeRaw(ctx)
	if err := ValidateYumMode(rawMode); err != nil {
		return err
	}
	mode := GetYumMode(ctx)
	ignore := ctx.GetParamBool("os_ignore_install_errors", false)
	isRHEL8 := IsRHEL8(ctx.OSInfo)

	ctx.LogPhase("pkg-install-plan", fmt.Sprintf(
		"yum_mode_raw=%q yum_mode=%q pkg_mgr=%s packages=%s rhel8=%v ignore_errors=%v",
		rawMode, mode, pkgManager, packages, isRHEL8, ignore,
	))

	if pkgManager == "apt" {
		if IsHTTPYumMode(mode) {
			return fmt.Errorf("os-yum-mode HTTP endpoint is not supported with apt; use system apt sources or switch to yum/dnf hosts")
		}
		ctx.LogPhase("system-install-start", fmt.Sprintf("pkg_mgr=apt packages=%s", packages))
		cmd, attempt := installViaSystem(ctx, pkgManager, packages, isRHEL8)
		still := StillMissingPackages(ctx, packages, pkgManager)
		if len(still) == 0 && attempt.err == nil && attempt.exitCode == 0 {
			ctx.LogPhase("system-install-done", fmt.Sprintf("packages=%s", packages))
		} else {
			logInstallAttemptPhase(ctx, "system-install-fail", cmd, attempt, still)
		}
		return finalizeInstallError(ctx, "apt install", attempt, still)
	}

	installOne := func(pkgs string) error {
		if IsHTTPYumMode(mode) {
			ctx.LogPhase("http-install-start", fmt.Sprintf("packages=%s", pkgs))
			cmd, attempt := installViaHTTP(ctx, pkgManager, pkgs, isRHEL8)
			still := StillMissingPackages(ctx, pkgs, pkgManager)
			if attempt.err == nil && attempt.exitCode == 0 && len(still) == 0 {
				ctx.LogPhase("http-install-done", fmt.Sprintf("packages=%s", pkgs))
				return nil
			}
			logInstallAttemptPhase(ctx, "http-install-fail", cmd, attempt, still)
			if !shouldFallbackToLocalMedia(ctx, attempt, still) {
				return finalizeInstallError(ctx, "HTTP yum install", attempt, still)
			}
			trigger := fallbackTriggerReason(attempt, still)
			if err := tryLocalMediaFallback(ctx, pkgManager, pkgs, isRHEL8, trigger); err != nil {
				if len(still) > 0 {
					return fmt.Errorf("HTTP yum install failed and %w; still missing: %s", err, strings.Join(still, ", "))
				}
				return fmt.Errorf("HTTP yum install failed and %w", err)
			}
			return nil
		}

		if IsLocalYumMode(mode) {
			ctx.LogPhase("local-install-start", fmt.Sprintf("packages=%s", pkgs))
			cmd, attempt := installViaLocalMedia(ctx, pkgManager, pkgs, isRHEL8)
			still := StillMissingPackages(ctx, pkgs, pkgManager)
			if attempt.err == nil && attempt.exitCode == 0 && len(still) == 0 {
				ctx.LogPhase("local-install-done", fmt.Sprintf("packages=%s", pkgs))
				return nil
			}
			logInstallAttemptPhase(ctx, "local-install-fail", cmd, attempt, still)
			return finalizeInstallError(ctx, "local media install", attempt, still)
		}

		ctx.LogPhase("system-install-start", fmt.Sprintf("packages=%s", pkgs))
		cmd, attempt := installViaSystem(ctx, pkgManager, pkgs, isRHEL8)
		still := StillMissingPackages(ctx, pkgs, pkgManager)
		if attempt.err == nil && attempt.exitCode == 0 && len(still) == 0 {
			ctx.LogPhase("system-install-done", fmt.Sprintf("packages=%s", pkgs))
			return nil
		}
		logInstallAttemptPhase(ctx, "system-install-fail", cmd, attempt, still)

		if !shouldFallbackToLocalMedia(ctx, attempt, still) {
			ctx.LogPhase("fallback-skip", fmt.Sprintf("reason=not_eligible trigger=%s", fallbackTriggerReason(attempt, still)))
			return finalizeInstallError(ctx, "system repo install", attempt, still)
		}

		trigger := fallbackTriggerReason(attempt, still)
		if err := tryLocalMediaFallback(ctx, pkgManager, pkgs, isRHEL8, trigger); err != nil {
			msg := "system repo install failed and " + err.Error()
			if len(still) > 0 {
				msg = fmt.Sprintf("%s; still missing: %s", msg, strings.Join(still, ", "))
			} else if attempt.exitCode != 0 || attempt.err != nil {
				msg = fmt.Sprintf("%s: %s", msg, installOutput(attempt))
			}
			ignore := ctx.GetParamBool("os_ignore_install_errors", false)
			if ignore {
				ctx.Logger.Warn("%s (--os-ignore-install-errors)", msg)
				return nil
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	}

	if ignore {
		var failed []string
		for _, pkg := range strings.Fields(packages) {
			pkg = strings.TrimSpace(pkg)
			if pkg == "" {
				continue
			}
			ctx.LogPhase("pkg-install-one-start", fmt.Sprintf("package=%s ignore_errors=true", pkg))
			if err := installOne(pkg); err != nil {
				failed = append(failed, pkg)
				ctx.Logger.Warn("Failed to install %s: %v", pkg, err)
				ctx.LogPhase("pkg-install-one-fail", fmt.Sprintf("package=%s err=%s", pkg, runner.TruncateForLog(err.Error(), 120)))
			} else {
				ctx.LogPhase("pkg-install-one-done", fmt.Sprintf("package=%s", pkg))
			}
		}
		still := StillMissingPackages(ctx, packages, pkgManager)
		for _, p := range still {
			if !containsString(failed, p) {
				ctx.Logger.Warn("Package still missing after install attempts: %s (--os-ignore-install-errors)", p)
				ctx.LogPhase("pkg-install-one-fail", fmt.Sprintf("package=%s reason=still_missing_after_attempts", p))
			}
		}
		ctx.LogPhase("pkg-install-done", fmt.Sprintf("packages=%s ignore_errors=true still_missing=%d", packages, len(still)))
		return nil
	}

	err := installOne(packages)
	if err != nil {
		ctx.LogPhase("pkg-install-fail", fmt.Sprintf("packages=%s err=%s", packages, runner.TruncateForLog(err.Error(), 160)))
		return err
	}
	ctx.LogPhase("pkg-install-done", fmt.Sprintf("packages=%s", packages))
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
