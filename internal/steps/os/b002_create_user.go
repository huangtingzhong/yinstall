package os

import (
	"fmt"
	"strings"

	commonos "github.com/yinstall/internal/common/os"
	"github.com/yinstall/internal/runner"
)

// StepB002CreateUser Create product user
func StepB002CreateUser() *runner.Step {
	return &runner.Step{
		ID:          "B-002",
		Name:        "Create User",
		Description: "Create yashan user",
		Tags:        []string{"os", "user"},
		Optional:    false,

		PreCheck: func(ctx *runner.StepContext) error {
			result, _ := ctx.Execute("which useradd", false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("useradd command not found")
			}
			return nil
		},

		Action: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			uid := ctx.GetParamInt("os_user_uid", 701)
			group := ctx.GetParamString("os_group", "yashan")
			dbaGroup := ctx.GetParamString("os_dba_group", "YASDBA")
			shell := ctx.GetParamString("os_user_shell", "/bin/bash")
			forceDelete := ctx.IsForceDeleteUser()

			// 检查用户是否已存在
			result, _ := ctx.Execute(fmt.Sprintf("id -u %s 2>/dev/null", user), false)
			if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
				existingUID := strings.TrimSpace(result.GetStdout())
				expectedUID := fmt.Sprintf("%d", uid)

				// 检查主组
				result, _ = ctx.Execute(fmt.Sprintf("id -gn %s 2>/dev/null", user), false)
				existingGroup := ""
				if result != nil {
					existingGroup = strings.TrimSpace(result.GetStdout())
				}

				if existingUID == expectedUID && existingGroup == group {
					ctx.Logger.Info("User '%s' already exists with correct UID %s and group '%s'", user, existingUID, existingGroup)
					ctx.SetResult("user_existed", true)
					return nil
				}

				if forceDelete {
					ctx.Logger.Warn("Force mode (--force-delete-user): deleting user '%s' to recreate", user)
					if err := commonos.ForceDeleteUser(ctx, user); err != nil {
						return err
					}
				} else {
					if existingUID != expectedUID {
						return fmt.Errorf("user '%s' already exists with UID %s, but expected UID %s (use -f --force-delete-user to recreate)", user, existingUID, expectedUID)
					}
					if existingGroup != group {
						return fmt.Errorf("user '%s' already exists with primary group '%s', but expected group '%s' (use -f --force-delete-user to recreate)", user, existingGroup, group)
					}
				}
			}

			// 检查 UID 是否被其他用户占用
			result, _ = ctx.Execute(fmt.Sprintf("getent passwd %d 2>/dev/null | cut -d: -f1", uid), false)
			if result != nil && result.GetExitCode() == 0 && strings.TrimSpace(result.GetStdout()) != "" {
				existingUser := strings.TrimSpace(result.GetStdout())
				if existingUser != user {
					if forceDelete {
						ctx.Logger.Warn("Force mode (--force-delete-user): deleting user '%s' to free UID %d", existingUser, uid)
						if err := commonos.ForceDeleteUser(ctx, existingUser); err != nil {
							return err
						}
					} else {
						return fmt.Errorf("UID %d is already used by user '%s', cannot create user '%s' (use -f --force-delete-user to recreate)", uid, existingUser, user)
					}
				}
			}

			// 创建用户
			cmd := fmt.Sprintf("/usr/sbin/useradd -u %d -g %s -G %s -m %s -s %s",
				uid, group, dbaGroup, user, shell)
			if _, err := ctx.ExecuteWithCheck(cmd, true); err != nil {
				return fmt.Errorf("failed to create user %s: %w", user, err)
			}

			ctx.Logger.Info("Created user '%s' with UID %d, group '%s'", user, uid, group)
			ctx.SetResult("user_existed", false)
			return nil
		},

		PostCheck: func(ctx *runner.StepContext) error {
			user := ctx.GetParamString("os_user", "yashan")
			result, _ := ctx.Execute(fmt.Sprintf("id %s", user), false)
			if result == nil || result.GetExitCode() != 0 {
				return fmt.Errorf("user %s not found after creation", user)
			}
			return nil
		},
	}
}
