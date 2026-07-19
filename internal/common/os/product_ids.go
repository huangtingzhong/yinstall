package os

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yinstall/internal/runner"
)

const productUIDMin = 701
const productUIDMax = 799

// ResolveProductUserIDs adopts or allocates UID/GID for product user/group.
func ResolveProductUserIDs(ctx *runner.StepContext, user, group string, defaultUID, defaultGID int) (uid, gid int, err error) {
	if ctx == nil {
		return 0, 0, fmt.Errorf("step context is nil")
	}
	if user == "" {
		user = "mysql"
	}
	if group == "" {
		group = user
	}

	res, _ := ctx.Execute(fmt.Sprintf("getent passwd %s 2>/dev/null", ShellSingleQuote(user)), false)
	if res != nil && res.GetExitCode() == 0 {
		line := strings.TrimSpace(res.GetStdout())
		if line != "" {
			fields := strings.Split(line, ":")
			if len(fields) >= 4 {
				u, e1 := strconv.Atoi(strings.TrimSpace(fields[2]))
				g, e2 := strconv.Atoi(strings.TrimSpace(fields[3]))
				if e1 == nil && e2 == nil {
					ctx.Logger.Info("Adopted existing user %s UID=%d GID=%d", user, u, g)
					return u, g, nil
				}
			}
		}
	}

	uid = defaultUID
	gid = defaultGID
	if uid == 0 {
		uid = productUIDMin
	}
	if gid == 0 {
		gid = uid
	}

	uidOK, err := idAvailable(ctx, uid, user)
	if err != nil {
		return 0, 0, err
	}
	gidOK, err := gidAvailable(ctx, gid, group)
	if err != nil {
		return 0, 0, err
	}
	if !uidOK || !gidOK {
		pair, e := scanFreeUIDPair(ctx, user, group)
		if e != nil {
			return 0, 0, e
		}
		return pair[0], pair[1], nil
	}
	return uid, gid, nil
}

func idAvailable(ctx *runner.StepContext, uid int, expectUser string) (bool, error) {
	res, _ := ctx.Execute(fmt.Sprintf("getent passwd %d 2>/dev/null", uid), false)
	if res == nil {
		return false, fmt.Errorf("getent passwd %d: no result", uid)
	}
	switch res.GetExitCode() {
	case 0:
		name := passwdUsernameFromLine(res.GetStdout())
		return name == "" || name == expectUser, nil
	case 1:
		return true, nil
	default:
		stderr := strings.TrimSpace(res.GetStderr())
		if res.GetExitCode() == 127 || strings.Contains(stderr, "command not found") {
			return false, fmt.Errorf("getent is not available: %s", stderr)
		}
		return false, fmt.Errorf("getent passwd %d failed (exit=%d): %s", uid, res.GetExitCode(), stderr)
	}
}

func gidAvailable(ctx *runner.StepContext, gid int, expectGroup string) (bool, error) {
	res, _ := ctx.Execute(fmt.Sprintf("getent group %d 2>/dev/null", gid), false)
	if res == nil {
		return false, fmt.Errorf("getent group %d: no result", gid)
	}
	switch res.GetExitCode() {
	case 0:
		name := groupNameFromLine(res.GetStdout())
		return name == "" || name == expectGroup, nil
	case 1:
		return true, nil
	default:
		stderr := strings.TrimSpace(res.GetStderr())
		if res.GetExitCode() == 127 || strings.Contains(stderr, "command not found") {
			return false, fmt.Errorf("getent is not available: %s", stderr)
		}
		return false, fmt.Errorf("getent group %d failed (exit=%d): %s", gid, res.GetExitCode(), stderr)
	}
}

func scanFreeUIDPair(ctx *runner.StepContext, user, group string) ([2]int, error) {
	for id := productUIDMin; id <= productUIDMax; id++ {
		uidOK, err := idAvailable(ctx, id, user)
		if err != nil {
			return [2]int{}, err
		}
		gidOK, err := gidAvailable(ctx, id, group)
		if err != nil {
			return [2]int{}, err
		}
		if uidOK && gidOK {
			return [2]int{id, id}, nil
		}
	}
	return [2]int{}, fmt.Errorf("no free UID/GID in range %d-%d", productUIDMin, productUIDMax)
}

func passwdUsernameFromLine(line string) string {
	fields := strings.Split(strings.TrimSpace(line), ":")
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

func groupNameFromLine(line string) string {
	fields := strings.Split(strings.TrimSpace(line), ":")
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}
