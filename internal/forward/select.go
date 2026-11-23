package forward

import "runtime"

// hostGOOS is the platform NewForwarder selects for. Tests replace it to
// exercise the Windows choice on any machine.
var hostGOOS = runtime.GOOS

// ControlMasterSupported reports whether the host's OpenSSH can keep a unix-socket
// ControlMaster. Windows OpenSSH cannot, so there every forward runs its own ssh.
func ControlMasterSupported() bool { return hostGOOS != "windows" }

// NewForwarder returns the forwarding strategy for this platform: ControlMaster
// through controlPath where OpenSSH supports it, otherwise one ssh process per
// forward. run and starter may be nil; they are used only by the strategy chosen.
func NewForwarder(sshPath, controlPath, host string, run Runner, starter Starter) Forwarder {
	if ControlMasterSupported() && controlPath != "" {
		return NewControlMaster(sshPath, controlPath, host, run)
	}
	return NewPerProcess(sshPath, host, starter)
}
