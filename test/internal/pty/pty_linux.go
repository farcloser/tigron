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

func Open() (pty, tty *os.File, err error) {
	defer func() {
		if err != nil {
			if pty != nil {
				err = errors.Join(pty.Close(), err)
			}

			err = errors.Join(ErrFailure, err)
		}
	}()

	pty, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}

	var npoint uint32

	err = ioctl(pty, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&npoint)))
	if err != nil {
		return nil, nil, err
	}

	sname := "/dev/pts/" + strconv.Itoa(int(npoint))

	var u int32
	if err = ioctl(pty, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&u))); err != nil {
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
