package cli

import (
	"slices"
	"testing"
)

func TestExecArgs(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		path    string
		command []string
	}{
		{[]string{"container:detected"}, "containers/detected/exec", nil},
		{[]string{"app:workspace/my-app/container", "--", "echo", "--wait"}, "workspaces/workspace/apps/my-app/containers/container/exec", []string{"echo", "--wait"}},
		{[]string{"service:redis"}, "services/redis/exec", nil},
		{[]string{"volume:data"}, "", nil},
		{[]string{"app:workspace/../container"}, "", nil},
		{[]string{"container:detected", "--"}, "", nil},
	} {
		path, command, err := execArgs(tc.args)
		if (err == nil) != (tc.path != "") || path != tc.path || !slices.Equal(command, tc.command) {
			t.Fatalf("execArgs(%q) = %q, %v, %v", tc.args, path, command, err)
		}
	}
}
