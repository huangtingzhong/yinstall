package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB002CreateGroup Create product groups
func StepB002CreateGroup() *runner.Step {
	return &runner.Step{
		ID:          "B-002",
		Name:        "Create Groups",
		Description: "Create yashan and YASDBA groups",
		Tags:        []string{"os", "user"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("which groupadd", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("groupadd command not found")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			group := ctx.GetParamString("os_group", "yashan")
			gid := ctx.GetParamInt("os_group_gid", 701)
			dbaGroup := ctx.GetParamString("os_dba_group", "YASDBA")
			dbaGid := ctx.GetParamInt("os_dba_group_gid", 702)
			forceDelete := ctx.IsForceDeleteUser()

			// 处理主组
			if err := createOrForceGroup(ctx, group, gid, forceDelete); err != nil {
				return err
			}

			// 处理 DBA 组
			if err := createOrForceGroup(ctx, dbaGroup, dbaGid, forceDelete); err != nil {
				return err
			}

			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			group := ctx.GetParamString("os_group", "yashan")
			dbaGroup := ctx.GetParamString("os_dba_group", "YASDBA")

			result, _ := ctx.Execute(fmt.Sprintf("getent group %s", group), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("group %s not found after creation", group)
			}
			result, _ = ctx.Execute(fmt.Sprintf("getent group %s", dbaGroup), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("group %s not found after creation", dbaGroup)
			}
			return nil
		},
	}
}

// createOrForceGroup 创建或强制重建组
func createOrForceGroup(ctx *runner.StepContext, group string, gid int, force bool) error {
	// 检查组是否已存在
	result, _ := ctx.Execute(fmt.Sprintf("getent group %s 2>/dev/null | cut -d: -f3", group), false)
	if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
		existingGID := strings.TrimSpace(result.GetStdout())
		expectedGID := fmt.Sprintf("%d", gid)

		if existingGID == expectedGID && !force {
			ctx.Logger.Info("Group '%s' already exists with correct GID %s", group, existingGID)
			return nil
		}

		// 需要删除重建（--force-delete-user 模式或 GID 不匹配）
		if force {
			ctx.Logger.Warn("Force mode (--force-delete-user): deleting group '%s' (GID %s) to recreate with GID %d", group, existingGID, gid)

			// 先检查并删除以该组为主组的用户
			result, _ := ctx.Execute(fmt.Sprintf("getent passwd | awk -F: '$4==%s {print $1}'", existingGID), false)
			if result != nil && strings.TrimSpace(result.GetStdout()) != "" {
				users := strings.Split(strings.TrimSpace(result.GetStdout()), "\n")
				for _, user := range users {
					user = strings.TrimSpace(user)
					if user == "" {
						continue
					}
					ctx.Logger.Warn("Force mode: deleting user '%s' (primary group is '%s')", user, group)
					if err := commonos.ForceDeleteUser(ctx, user); err != nil {
						return err
					}
				}
			}

			// 删除组（可能已被 userdel 一起删除）
			result, _ = ctx.Execute(fmt.Sprintf("getent group %s 2>/dev/null", group), false)
			if result != nil && result.GetExitCode() == 0 {
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("/usr/sbin/groupdel %s", group), true); err != nil {
					return fmt.Errorf("failed to delete group %s: %w", group, err)
				}
			}
		} else {
			return fmt.Errorf("group '%s' already exists with GID %s, but expected GID %s (use -f --force-delete-user to recreate)", group, existingGID, expectedGID)
		}
	}

	// 检查 GID 是否被其他组占用
	result, _ = ctx.Execute(fmt.Sprintf("getent group %d 2>/dev/null | cut -d: -f1", gid), false)
	if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
		existingGroup := strings.TrimSpace(result.GetStdout())
		if existingGroup != group {
			if force {
				ctx.Logger.Warn("Force mode (--force-delete-user): deleting group '%s' to free GID %d", existingGroup, gid)
				if _, err := ctx.ExecuteWithCheck(fmt.Sprintf("/usr/sbin/groupdel %s", existingGroup), true); err != nil {
					return fmt.Errorf("failed to delete group %s: %w", existingGroup, err)
				}
			} else {
				return fmt.Errorf("GID %d is already used by group '%s', cannot create group '%s' (use -f --force-delete-user to recreate)", gid, existingGroup, group)
			}
		}
	}

	// 创建组
	cmd := fmt.Sprintf("/usr/sbin/groupadd -g %d %s", gid, group)
	if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
		return fmt.Errorf("failed to create group %s: %w", group, err)
	}
	ctx.Logger.Info("Created group '%s' with GID %d", group, gid)

	return nil
}
