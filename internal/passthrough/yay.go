// Package passthrough esegue il motore AUR sottostante (yay) con gli argomenti
// dati, inoltrando stdin/stdout/stderr in modo trasparente.
package passthrough

import (
	"errors"
	"os"
	"os/exec"
)

// Exec lancia yay con gli argomenti forniti. Restituisce il codice di uscita
// del processo (0 se ok), oppure -1 in caso di errore di avvio.
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
