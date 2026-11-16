package sandboxexec

import "errors"

func execProgram([]string) error {
	return errors.New("the kernel sandbox is not available on windows")
}
