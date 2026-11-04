package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests build the program and run it through its command line, the
// way the image pre-puller's containers run it.

var program string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "image-puller-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	program = filepath.Join(dir, "image-puller")
	build := exec.Command("go", "build", "-o", program, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build image-puller: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

func TestExitRoleExitsZero(t *testing.T) {
	out, err := exec.Command(program, "exit").CombinedOutput()
	if err != nil {
		t.Fatalf("image-puller exit: %v\n%s", err, out)
	}
}

func TestCopyRoleLeavesARunnableCopyInTheDirectory(t *testing.T) {
	dir := t.TempDir()

	if out, err := exec.Command(program, "copy", dir).CombinedOutput(); err != nil {
		t.Fatalf("image-puller copy %s: %v\n%s", dir, err, out)
	}

	copied := filepath.Join(dir, "image-puller")
	info, err := os.Stat(copied)
	if err != nil {
		t.Fatalf("no copy in %s: %v", dir, err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("copy has mode %v, want -rwxr-xr-x so any user can run it", info.Mode().Perm())
	}
	if out, err := exec.Command(copied, "exit").CombinedOutput(); err != nil {
		t.Errorf("running the copy's exit role: %v\n%s", err, out)
	}
}

func TestUnknownRoleFailsWithUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"pull"}, {"copy"}, {"exit", "now"}} {
		var stderr strings.Builder
		cmd := exec.Command(program, args...)
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err == nil {
			t.Errorf("image-puller %v exited 0, want a failure", args)
		}
		if !strings.Contains(stderr.String(), "usage: image-puller copy <directory> | exit | wait") {
			t.Errorf("image-puller %v stderr = %q, want the usage line", args, stderr.String())
		}
	}
}

func TestWaitRoleRunsUntilATerminationSignalThenExitsZero(t *testing.T) {
	cmd := exec.Command(program, "wait")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start image-puller wait: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// The program says it is waiting once it handles termination
	// signals, so the signal below can't arrive before that.
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("image-puller wait printed no line before waiting: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		t.Fatalf("image-puller wait exited before any signal: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("image-puller wait after SIGTERM: %v, want exit status 0", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("image-puller wait did not exit within 5s of SIGTERM")
	}
}
