package main

import _ "embed"

const (
	remoteRuntimeReadyMarker   = "/opt/yolobox/remote/ready"
	remoteRuntimeSessionScript = "/usr/local/bin/yolobox-remote-session"
)

//go:embed assets/remote-vm-install.sh
var remoteVMInstallScript string

func buildRemoteBootstrapScript() string {
	return remoteVMInstallScript
}
