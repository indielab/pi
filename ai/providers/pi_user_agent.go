package providers

import (
	"runtime"
	"sync"
)

// piUserAgent returns the pi runtime user agent, ported from pi
// utils/pi-user-agent.ts getPiUserAgent(): "pi (<platform> <release>; <arch>)"
// from Node os.platform()/os.release()/os.arch(), or "pi (browser)" when the
// runtime has no node:os. Platform and arch use Node's names, not Go's
// (win32/x64/ia32), so the wire string matches pi byte-for-byte; the browser
// fallback maps to GOOSes where the release syscall is unavailable. The kernel
// release cannot change mid-process, so the string is computed once.
var piUserAgent = sync.OnceValue(func() string {
	release, ok := osRelease()
	if !ok {
		return "pi (browser)"
	}
	return "pi (" + nodePlatform() + " " + release + "; " + nodeArch() + ")"
})

// nodePlatform maps runtime.GOOS to Node's process.platform name. They agree
// everywhere the port builds except Windows.
func nodePlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

// nodeArch maps runtime.GOARCH to Node's os.arch() name (documented values:
// arm, arm64, ia32, loong64, mips, mipsel, ppc64, riscv64, s390x, x64).
func nodeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	case "mipsle":
		return "mipsel"
	case "ppc64le":
		return "ppc64"
	default:
		return runtime.GOARCH
	}
}
