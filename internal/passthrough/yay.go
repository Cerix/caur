// Package passthrough runs the underlying AUR engine (yay) with the given
// arguments, forwarding stdin/stdout/stderr transparently.
package passthrough

import (
	"errors"
	"os"
	"os/exec"
)

// Exec runs yay with the given arguments. It returns the process exit code
// (0 on success), or -1 on a startup error.
func Exec(yayPath string, args []string) (int, error) {
	if yayPath == "" {
		yayPath = "yay"
	}
	cmd := exec.Command(yayPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}
