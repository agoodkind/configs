package opnsensesvc

import "os"

// writeBytes is a thin os.WriteFile wrapper used by tests in this
// package so they don't each need to import os.
func writeBytes(path string, content []byte) error {
	return os.WriteFile(path, content, 0o644)
}
