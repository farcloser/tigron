//go:build darwin

/*
   Copyright Farcloser.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pty

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Originally from https://github.com/creack/pty/tree/2cde18bfb702199728dd43bf10a6c15c7336da0a

const (
	ioctlParamShift = 13
	ioctlParamMask  = (1 << ioctlParamShift) - 1
	ioctlShift      = 16
)

var errNotNULTerminated = errors.New("TIOCPTYGNAME string not NUL-terminated")

func ioctlParmLen(ioctl uintptr) uintptr {
	return (ioctl >> ioctlShift) & ioctlParamMask
}

func popen() (pty, tty *os.File, err error) {
	defer func() {
		if err != nil {
			if pty != nil {
				err = errors.Join(pty.Close(), err)
			}

			err = errors.Join(ErrFailure, err)
		}
	}()

	pFD, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck
	}

	pty = os.NewFile(uintptr(pFD), "/dev/ptmx")

	npoint := make([]byte, ioctlParmLen(syscall.TIOCPTYGNAME))

	//nolint:gosec
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
		return nil, nil, err //nolint:wrapcheck
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
