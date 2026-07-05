package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScanCamoufoxOutputReturnsFailureOutput(t *testing.T) {
	captured := &camoufoxOutput{}

	err := scanCamoufoxOutput(strings.NewReader("missing libnss3.so\nbrowser closed\n"), captured, func(string) {})

	if err == nil {
		t.Fatal("scanCamoufoxOutput() err=nil, want error")
	}
	for _, want := range []string{
		"camoufox server exited without printing a ws:// endpoint",
		"missing libnss3.so",
		"browser closed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("scanCamoufoxOutput() error %q does not contain %q", err, want)
		}
	}
}

func TestScanCamoufoxOutputFindsEndpoint(t *testing.T) {
	captured := &camoufoxOutput{}
	var got string

	err := scanCamoufoxOutput(strings.NewReader("starting\nListening on ws://127.0.0.1:1234/devtools/browser/abc\u001b[0m\n"), captured, func(ws string) {
		got = ws
	})

	if err != nil {
		t.Fatalf("scanCamoufoxOutput() err=%v, want nil", err)
	}
	const want = "ws://127.0.0.1:1234/devtools/browser/abc"
	if got != want {
		t.Fatalf("endpoint=%q, want %q", got, want)
	}
}

func TestCamoufoxServerCommandUsesXvfbRunWithoutDisplayOnLinux(t *testing.T) {
	lookup := func(name string) (string, error) {
		if name != "xvfb-run" {
			t.Fatalf("lookPath(%q), want xvfb-run", name)
		}
		return "/usr/bin/xvfb-run", nil
	}

	name, args := camoufoxServerCommand("/opt/camoufox/bin/camoufox", "", "linux", lookup)

	if name != "/usr/bin/xvfb-run" {
		t.Fatalf("name=%q, want xvfb-run", name)
	}
	want := []string{"-a", "/opt/camoufox/bin/camoufox", "server"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%q, want %q", args, want)
	}
}

// TestReapCmdLockedKillsProcessGroup verifies the reaping fix: killing the
// process group takes down the whole tree (mirroring camoufox forking Firefox)
// and clears m.cmd, so a failed/torn-down launch can't leak a browser process.
func TestReapCmdLockedKillsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group signals are POSIX-only")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// The shell (our direct child) forks a grandchild sleep in the same process
	// group and records its PID, standing in for camoufox → Firefox.
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $! > "+pidFile+"; wait") //nolint:gosec,noctx // pidFile is a test-controlled t.TempDir() path; short-lived helper needs no ctx
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	shellPID := cmd.Process.Pid
	childPID := readPIDFile(t, pidFile)

	m := &Manager{cmd: cmd}
	m.reapCmdLocked()

	if m.cmd != nil {
		t.Error("reapCmdLocked did not clear m.cmd")
	}
	waitProcessGone(t, shellPID) // direct child killed and reaped
	waitProcessGone(t, childPID) // grandchild killed via the process-group signal
}

// TestReapCmdLockedNilCmd verifies reaping is a safe no-op when no process is
// running (e.g. after an idle teardown), so ensureBrowser/Close can call it
// unconditionally.
func TestReapCmdLockedNilCmd(t *testing.T) {
	m := &Manager{}
	m.reapCmdLocked() // must not panic
	if m.cmd != nil {
		t.Error("m.cmd should remain nil")
	}
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	for range 200 { // up to ~2s for the shell to write it
		if b, err := os.ReadFile(path); err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child PID file %s never written", path)
	return 0
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	for range 200 { // up to ~2s
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d still alive after reap", pid)
}
