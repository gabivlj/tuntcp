package main

import (
	"fmt"
	"os"
	"path/filepath"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// these methods are basically taken from gvisor repository
//  https://github.com/google/gvisor

// nsPath returns the path of the namespace for the current process and the
// given namespace.
func nsPath(nst specs.LinuxNamespaceType) string {
	base := "/proc/self/ns"
	switch nst {
	case specs.CgroupNamespace:
		return filepath.Join(base, "cgroup")
	case specs.IPCNamespace:
		return filepath.Join(base, "ipc")
	case specs.MountNamespace:
		return filepath.Join(base, "mnt")
	case specs.NetworkNamespace:
		return filepath.Join(base, "net")
	case specs.PIDNamespace:
		return filepath.Join(base, "pid")
	case specs.UserNamespace:
		return filepath.Join(base, "user")
	case specs.UTSNamespace:
		return filepath.Join(base, "uts")
	default:
		panic(fmt.Sprintf("unknown namespace %v", nst))
	}
}

func ApplyNS(ns specs.LinuxNamespace) (func() error, error) {
	newNS, err := os.Open(ns.Path)
	if err != nil {
		return nil, fmt.Errorf("error opening %q: %v", ns.Path, err)
	}
	defer newNS.Close()

	// Store current namespace to restore back.
	curPath := nsPath(ns.Type)
	oldNS, err := os.Open(curPath)
	if err != nil {
		return nil, fmt.Errorf("error opening %q: %v", curPath, err)
	}

	// Set namespace to the one requested and setup function to restore it back.
	flag := nsCloneFlag(ns.Type)
	if err := setNS(newNS.Fd(), flag); err != nil {
		oldNS.Close()
		return nil, fmt.Errorf("error setting namespace of type %v and path %q: %v", ns.Type, ns.Path, err)
	}
	return func() error {
		defer oldNS.Close()
		if err := setNS(oldNS.Fd(), flag); err != nil {
			return fmt.Errorf("error restoring namespace: of type %v: %v", ns.Type, err)
		}
		return nil
	}, nil
}

// setNS sets the namespace of the given type.  It must be called with
// OSThreadLocked.
func setNS(fd, nsType uintptr) error {
	if _, _, err := unix.RawSyscall(unix.SYS_SETNS, fd, nsType, 0); err != 0 {
		return err
	}
	return nil
}

// nsCloneFlag returns the clone flag that can be used to set a namespace of
// the given type.
func nsCloneFlag(nst specs.LinuxNamespaceType) uintptr {
	switch nst {
	case specs.IPCNamespace:
		return unix.CLONE_NEWIPC
	case specs.MountNamespace:
		return unix.CLONE_NEWNS
	case specs.NetworkNamespace:
		return unix.CLONE_NEWNET
	case specs.PIDNamespace:
		return unix.CLONE_NEWPID
	case specs.UTSNamespace:
		return unix.CLONE_NEWUTS
	case specs.UserNamespace:
		return unix.CLONE_NEWUSER
	case specs.CgroupNamespace:
		return unix.CLONE_NEWCGROUP
	default:
		panic(fmt.Sprintf("unknown namespace %v", nst))
	}
}
