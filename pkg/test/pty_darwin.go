//go:build darwin

package test

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Originally from https://github.com/creack/pty/tree/2cde18bfb702199728dd43bf10a6c15c7336da0a

//nolint:revive,stylecheck
const (
	_IOC_PARAM_SHIFT = 13
	_IOC_PARAM_MASK  = (1 << _IOC_PARAM_SHIFT) - 1
)

var (
	errNotNULTerminated = errors.New("TIOCPTYGNAME string not NUL-terminated")
)

//nolint:revive,stylecheck
func _IOC_PARM_LEN(ioctl uintptr) uintptr {
	//nolint:mnd
	return (ioctl >> 16) & _IOC_PARAM_MASK
}

func Open() (pty, tty *os.File, err error) {
	defer func() {
		if err != nil {
			if pty != nil {
				err = errors.Join(pty.Close(), err)
			}

			err = errors.Join(ErrPTYFailure, err)
		}
	}()

	pFD, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}

	pty = os.NewFile(uintptr(pFD), "/dev/ptmx")

	npoint := make([]byte, _IOC_PARM_LEN(syscall.TIOCPTYGNAME))

	err = ioctl(pty, syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&npoint[0])))
	if err != nil {
		return nil, nil, err
	}

	sname := ""

	for i, c := range npoint {
		if c == 0 {
			sname = string(npoint[:i])

			break
		}
	}

	if sname == "" {
		return nil, nil, errNotNULTerminated
	}

	if err = ioctl(pty, syscall.TIOCPTYGRANT, 0); err != nil {
		return nil, nil, err
	}

	if err = ioctl(pty, syscall.TIOCPTYUNLK, 0); err != nil {
		return nil, nil, err
	}

	tty, err = os.OpenFile(sname, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}

	return pty, tty, nil
}

func ioctl(f *os.File, cmd, ptr uintptr) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), cmd, ptr)
	if e != 0 {
		return e
	}

	return nil
}
