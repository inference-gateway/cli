package kubernetes

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
)

// TerminationLog is the file Kubernetes reads a container's termination
// message from.
const TerminationLog = "/dev/termination-log"

// ReportIdle leaves the operator the termination message of a pod daemon that
// idles out: whether every agent it ran is done, which starts the
// Orchestrator's retention. Outside a pod there is nothing to report.
func ReportIdle(done bool) error {
	return writeTerminationMessage(TerminationLog, done)
}

// ponytail: a pod that ran no agent reports not done, which only keeps its
// Orchestrator longer. Carry the last pod's answer if retention ever stalls.
func writeTerminationMessage(path string, done bool) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = file.WriteString("agents-done=" + strconv.FormatBool(done))
	return errors.Join(err, file.Close())
}
