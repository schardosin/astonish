package e2eboot

import "syscall"

// syscallSignal0 is the no-op probe signal used by IsInspectorRunning.
// On Unix this is signal 0; on Windows os.Process.Signal accepts only
// os.Kill / os.Interrupt — we don't support inspector mode on Windows.
var syscallSignal0 = syscall.Signal(0)
