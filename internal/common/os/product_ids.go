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
	if user == "" {
		user = "mysql"
	}
	if group == "" {
		group = user
	}

	// Existing user → adopt IDs
	res, _ := ctx.Execute(fmt.Sprintf("getent passwd %s 2>/dev/null", user), false)
	if res != nil && res.GetExitCode() == 0 && strings.TrimSpace(res.GetStdout()) != "" {
		fields := strings.Split(strings.TrimSpace(res.GetStdout()), ":")
		if len(fields) >= 4 {
			u, e1 := strconv.Atoi(fields[2])
			g, e2 := strconv.Atoi(fields[3])
			if e1 == nil && e2 == nil {
				ctx.Logger.Info("Adopted existing user %s UID=%d GID=%d", user, u, g)
				return u, g, nil
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

	if !idAvailable(ctx, uid, user) || !gidAvailable(ctx, gid, group) {
		pair, e := scanFreeUIDPair(ctx, user, group)
		if e != nil {
			return 0, 0, e
		}
		return pair[0], pair[1], nil
	}
	return uid, gid, nil
}

func idAvailable(ctx *runner.StepContext, uid int, expectUser string) bool {
	res, _ := ctx.Execute(fmt.Sprintf("getent passwd %d 2>/dev/null | cut -d: -f1", uid), false)
	if res == nil || res.GetExitCode() != 0 || strings.TrimSpace(res.GetStdout()) == "" {
		return true
	}
	return strings.TrimSpace(res.GetStdout()) == expectUser
}

func gidAvailable(ctx *runner.StepContext, gid int, expectGroup string) bool {
	res, _ := ctx.Execute(fmt.Sprintf("getent group %d 2>/dev/null | cut -d: -f1", gid), false)
	if res == nil || res.GetExitCode() != 0 || strings.TrimSpace(res.GetStdout()) == "" {
		return true
	}
	return strings.TrimSpace(res.GetStdout()) == expectGroup
}

func scanFreeUIDPair(ctx *runner.StepContext, user, group string) ([2]int, error) {
	for id := productUIDMin; id <= productUIDMax; id++ {
		if idAvailable(ctx, id, user) && gidAvailable(ctx, id, group) {
			return [2]int{id, id}, nil
		}
	}
	return [2]int{}, fmt.Errorf("no free UID/GID in range %d-%d", productUIDMin, productUIDMax)
}
