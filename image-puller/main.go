/*
 * image-puller is the helper the Educates image pre-puller runs in place of
 * anything from the images it pulls.
 *
 * The pre-puller is a DaemonSet that makes each node pull an image by running
 * it as an init container. Running a command that the pulled image provides,
 * such as /bin/true, fails on images that have no such command, distroless
 * and scratch images among them, and a failing init container stops every
 * image after it from being pulled. Instead, a first init container from the
 * image-puller image copies this program into a shared emptyDir volume, every
 * pulled image runs that copy's exit role from the volume, and the
 * DaemonSet's long-running container runs the wait role, so that the pod
 * stays in place and stops promptly when Kubernetes terminates it.
 *
 * Roles:
 *
 *	image-puller copy <directory>  copy this program into <directory> as an
 *	                               executable named image-puller
 *	image-puller exit              exit 0
 *	image-puller wait              wait for SIGTERM or SIGINT, then exit 0
 *
 * The program is statically linked so that it runs from any image.
 */

package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const usage = "usage: image-puller copy <directory> | exit | wait"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	switch {
	case len(args) == 1 && args[0] == "exit":
		return 0
	case len(args) == 2 && args[0] == "copy":
		if err := copySelf(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "image-puller: %v\n", err)
			return 1
		}
		return 0
	case len(args) == 1 && args[0] == "wait":
		waitForTermination()
		return 0
	}
	fmt.Fprintln(os.Stderr, usage)
	return 2
}

// waitForTermination blocks until the process receives SIGTERM or
// SIGINT.
func waitForTermination() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	fmt.Println("image-puller: waiting for a termination signal")
	<-signals
}

// copySelf writes this program into dir as an executable named
// image-puller.
func copySelf(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find own executable: %w", err)
	}
	src, err := os.Open(self)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	target := filepath.Join(dir, "image-puller")
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("copy to %s: %w", target, err)
	}
	if err := dst.Close(); err != nil {
		return err
	}
	// The process umask may have narrowed the mode OpenFile set.
	return os.Chmod(target, 0o755)
}
