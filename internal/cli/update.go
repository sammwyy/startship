package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sammwyy/startship/internal/update"
	"github.com/sammwyy/startship/internal/version"
)

const updateTimeout = 10 * time.Minute

func (c *cli) releases() *update.Client {
	if c.updates == nil {
		c.updates = update.FromEnv()
	}
	return c.updates
}

func (c *cli) installDir() (string, error) {
	if c.binDir != "" {
		return c.binDir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// restartDaemon restarts startshipd when it runs as the installer's systemd service.
func (c *cli) restartDaemon() (bool, error) {
	if c.restart != nil {
		return c.restart()
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false, nil
	}
	if exec.Command("systemctl", "is-active", "--quiet", "startshipd.service").Run() != nil {
		return false, nil
	}
	if out, err := exec.Command("systemctl", "restart", "startshipd.service").CombinedOutput(); err != nil {
		return false, fmt.Errorf("restart startshipd: %v: %s", err, out)
	}
	return true, nil
}

func (c *cli) runUpdate(a []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	sub := ""
	if len(a) > 0 && !update.ValidVersion(a[0]) {
		sub, a = a[0], a[1:]
	}
	if len(a) > 1 || (len(a) == 1 && !update.ValidVersion(a[0])) {
		return errors.New("usage: startship update [check | daemon] [vX.Y.Z]")
	}
	target := ""
	if len(a) == 1 {
		target = a[0]
	}
	switch sub {
	case "check":
		if target != "" {
			return errors.New("usage: startship update check")
		}
		return c.updateCheck(ctx)
	case "daemon":
		body := map[string]string{}
		if target != "" {
			body["version"] = target
		}
		v, err := c.request("POST", "system/update", body)
		if err != nil {
			return err
		}
		if c.raw {
			return c.print(v)
		}
		m, _ := v.(map[string]any)
		fmt.Fprintf(c.out, "Daemon update to %s queued; startshipd restarts when it is installed.\nFollow it with: startship update check\n", field(m, "version"))
		return nil
	case "apply-request":
		return c.applyRequest(ctx)
	case "":
		return c.updateLocal(ctx, target)
	}
	return errors.New("usage: startship update [check | daemon] [vX.Y.Z]")
}

func (c *cli) updateCheck(ctx context.Context) error {
	latest, checkErr := c.releases().Latest(ctx)
	daemon, daemonErr := c.request("GET", "system/update", nil)
	if c.raw {
		out := map[string]any{"cli": version.Version, "latest": latest.Version, "release_url": latest.URL}
		if checkErr != nil {
			out["check_error"] = checkErr.Error()
		}
		if daemonErr == nil {
			out["daemon"] = daemon
		} else {
			out["daemon_error"] = daemonErr.Error()
		}
		return c.print(out)
	}
	if checkErr != nil {
		fmt.Fprintf(c.out, "Latest release: unknown (%v)\n", checkErr)
	} else {
		fmt.Fprintf(c.out, "Latest release: %s  %s\n", latest.Version, latest.URL)
	}
	fmt.Fprintf(c.out, "CLI:            %s%s\n", version.Version, newer(version.Version, latest.Version))
	if daemonErr != nil {
		fmt.Fprintf(c.out, "Daemon:         unreachable (%v)\n", daemonErr)
		return nil
	}
	m, _ := daemon.(map[string]any)
	fmt.Fprintf(c.out, "Daemon:         %s%s\n", field(m, "current"), newer(field(m, "current"), latest.Version))
	if p, ok := m["pending"].(map[string]any); ok {
		fmt.Fprintf(c.out, "Pending:        update to %s requested by %s\n", field(p, "version"), field(p, "by"))
	}
	if s, ok := m["update"].(map[string]any); ok {
		line := field(s, "state") + " " + field(s, "version")
		if e := field(s, "error"); e != "" {
			line += ": " + e
		}
		fmt.Fprintf(c.out, "Last update:    %s\n", line)
	}
	return nil
}

func newer(current, latest string) string {
	switch {
	case latest == "":
		return ""
	case !update.ValidVersion(current):
		return " (development build)"
	case update.Compare(latest, current) > 0:
		return " (update available: startship update)"
	}
	return " (up to date)"
}

// updateLocal replaces the CLI and the daemon installed beside it, then restarts the service.
func (c *cli) updateLocal(ctx context.Context, target string) error {
	if target == "" {
		latest, err := c.releases().Latest(ctx)
		if err != nil {
			return fmt.Errorf("check for updates: %w", err)
		}
		if update.ValidVersion(version.Version) && update.Compare(latest.Version, version.Version) <= 0 {
			fmt.Fprintf(c.out, "Already up to date (%s).\n", version.Version)
			return nil
		}
		target = latest.Version
	}
	dir, err := c.installDir()
	if err != nil {
		return err
	}
	installed, err := c.releases().Install(ctx, target, dir, update.Binaries)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w; run sudo startship update", err)
		}
		return err
	}
	for _, path := range installed {
		fmt.Fprintf(c.out, "Installed %s %s\n", target, path)
	}
	restarted, err := c.restartDaemon()
	if err != nil {
		return err
	}
	if restarted {
		fmt.Fprintln(c.out, "Restarted startshipd.service")
	}
	return nil
}

// applyRequest is run as root by startship-update.service when the daemon queues an update.
func (c *cli) applyRequest(ctx context.Context) error {
	reqPath := os.Getenv("STARTSHIP_UPDATE_REQUEST")
	if reqPath == "" {
		return errors.New("STARTSHIP_UPDATE_REQUEST is not set")
	}
	statusPath := os.Getenv("STARTSHIP_UPDATE_STATUS")
	if statusPath == "" {
		statusPath = filepath.Join(filepath.Dir(reqPath), "status.json")
	}
	var req update.Request
	err := update.ReadJSON(reqPath, &req)
	// Remove first so the path unit does not trigger again.
	if rmErr := os.Remove(reqPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return rmErr
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	status := update.Status{State: "running", Version: req.Version, Started: time.Now().UTC()}
	finish := func(err error) error {
		status.Finished = time.Now().UTC()
		status.State = "succeeded"
		if err != nil {
			status.State, status.Error = "failed", err.Error()
		}
		if werr := update.WriteJSON(statusPath, status, 0o644); werr != nil && err == nil {
			err = werr
		}
		return err
	}
	if err != nil {
		return finish(fmt.Errorf("invalid update request: %w", err))
	}
	if !update.ValidVersion(req.Version) {
		return finish(fmt.Errorf("invalid version %q", req.Version))
	}
	if err := update.WriteJSON(statusPath, status, 0o644); err != nil {
		return err
	}
	dir, err := c.installDir()
	if err != nil {
		return finish(err)
	}
	if _, err := c.releases().Install(ctx, req.Version, dir, update.Binaries); err != nil {
		return finish(err)
	}
	if err := finish(nil); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Installed %s requested by %s\n", req.Version, req.By)
	if _, err := c.restartDaemon(); err != nil {
		return finish(err)
	}
	return nil
}
