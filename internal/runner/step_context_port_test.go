package runner

import "testing"

func TestStepContextYasbootRemoteSSHPort(t *testing.T) {
	t.Parallel()
	ctx := &StepContext{Params: map[string]interface{}{"ssh_port": 2222}}
	if got := ctx.YasbootRemoteSSHPort(22); got != 2222 {
		t.Fatalf("fallback ssh: got %d", got)
	}
	ctx.Params["yasboot_ssh_port"] = 2200
	if got := ctx.YasbootRemoteSSHPort(22); got != 2200 {
		t.Fatalf("explicit yasboot: got %d", got)
	}
	ctx.Params["yasboot_ssh_port"] = 0
	if got := ctx.YasbootRemoteSSHPort(22); got != 2222 {
		t.Fatalf("zero yasboot uses ssh: got %d", got)
	}
}
