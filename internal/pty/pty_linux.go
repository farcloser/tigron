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
	"strconv"
	"syscall"
	"unsafe"
)

// Originally from https://github.com/creack/pty/tree/2cde18bfb702199728dd43bf10a6c15c7336da0a

func open() (pty, tty *os.File, err error) {
	// Wrap errors
	defer func() {
		if err != nil {
			if pty != nil {
				err = errors.Join(pty.Close(), err)
			}

			err = errors.Join(ErrFailure, err)
		}
	}()

	// Open the pty
	pty, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck
	}

	// Get the slave unit number
	var unitNumber uint32

	//nolint:gosec
	_, _, sysErr := syscall.Syscall(
		syscall.SYS_IOCTL,
		pty.Fd(),
		syscall.TIOCGPTN,
		uintptr(unsafe.Pointer(&unitNumber)),
	)
	if sysErr != 0 {
		return nil, nil, sysErr
	}

	sname := "/dev/pts/" + strconv.Itoa(int(unitNumber))

	// Unlock
	var upoint int32

	//nolint:gosec
	_, _, sysErr = syscall.Syscall(
		syscall.SYS_IOCTL,
		pty.Fd(),
		syscall.TIOCSPTLCK,
		uintptr(unsafe.Pointer(&upoint)),
	)
	if sysErr != 0 {
		return nil, nil, sysErr
	}

	// Open the slave, preventing it from becoming the controlling terminal
	//nolint:gosec
	tty, err = os.OpenFile(sname, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck
	}

	return pty, tty, nil
}
