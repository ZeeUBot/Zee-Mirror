package storage

import "os/exec"

// execCommand is a seam replaced in tests to fake external tools.
var execCommand = exec.CommandContext
