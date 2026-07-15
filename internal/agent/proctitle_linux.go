//go:build linux

package agent

import (
	"os"
	"syscall"
	"unsafe"
)

// prSetName is PR_SET_NAME from <linux/prctl.h>.
const prSetName = 15

// setProcTitle rewrites the process's visible name so tools like htop, ps, and
// top display `title` instead of the install path.
//
// Two mechanisms are used together:
//   - argv[0] is overwritten in place. The kernel exposes /proc/self/cmdline
//     from the original argv memory, which os.Args[0] points at directly on
//     Linux; overwriting those bytes changes what htop shows in its Command
//     column. This is the standard setproctitle technique. It only works when
//     argv[0] is at least as long as the title — the installer places the
//     binary at /usr/local/bin/management-agent (well over 16 bytes), so the
//     16-byte "MANAGEMENT AGENT" always fits.
//   - prctl(PR_SET_NAME) sets the kernel thread comm (capped at 15 chars),
//     which htop shows for threads and some views.
func setProcTitle(title string) {
	if len(os.Args) == 0 {
		return
	}
	// Overwrite argv[0] in place. Capture the backing bytes before mutating.
	n := len(os.Args[0])
	if n > 0 {
		buf := unsafe.Slice(unsafe.StringData(os.Args[0]), n)
		for i := range buf {
			buf[i] = 0
		}
		t := title
		if len(t) > n {
			t = t[:n]
		}
		copy(buf, t)
	}

	// Set the kernel comm (thread name), NUL-terminated, max 16 bytes incl NUL.
	comm := title
	if len(comm) > 15 {
		comm = comm[:15]
	}
	cb := append([]byte(comm), 0)
	_, _, _ = syscall.Syscall(syscall.SYS_PRCTL, prSetName, uintptr(unsafe.Pointer(&cb[0])), 0)
}
