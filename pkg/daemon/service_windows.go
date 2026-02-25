//go:build windows

package daemon

import "fmt"

func newPlatformService() (Service, error) {
	return nil, fmt.Errorf("daemon service is not yet supported on Windows; use WSL2 instead")
}
