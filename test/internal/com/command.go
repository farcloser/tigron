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

package com

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/term"

	"go.farcloser.world/tigron/test/internal"
	"go.farcloser.world/tigron/test/internal/pty"
)

const defaultTimeout = 10 * time.Minute

var (
	ErrFailedStarting      = errors.New("failed starting")
	ErrFailedSendingSignal = errors.New("failed sending signal")
	ErrAlreadyStarted      = errors.New("already started")
	ErrNotStarted          = errors.New("not started")
	ErrFailedAcquiringPipe = errors.New("failed acquiring pipe")
	ErrFailedRead          = errors.New("failed reading")
	ErrFailedWrite         = errors.New("failed writing")
)

type Execution struct {
	//nolint:containedctx
	context context.Context
	cancel  context.CancelFunc
	command *exec.Cmd
	ioGroup *errgroup.Group

	Environ  []string `json:"environ"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	ExitCode int      `json:"exitCode"`
}

type Command struct {
	Binary   string        `json:"binary"`
	Args     []string      `json:"args"`
	Prepend  []string      `json:"prepend"`
	Wrap     string        `json:"wrap"`
	WrapArgs []string      `json:"wrapArgs"`
	Timeout  time.Duration `json:"timeout"`

	WorkingDir string            `json:"workingDir"`
	Env        map[string]string `json:"env"`
	// XXX EnvBlackList might change (replace by regexp and/or whitelist + blacklist)
	EnvBlackList []string `json:"envBlackList"`

	writers []func() io.Reader

	ptyStdout bool
	ptyStderr bool
	ptyStdin  bool

	mutex sync.Mutex
	exec  *Execution
}

func (gc *Command) Clone() *Command {
	com := &Command{
		Binary:       gc.Binary,
		Args:         append([]string(nil), gc.Args...),
		Prepend:      append([]string(nil), gc.Prepend...),
		Wrap:         gc.Wrap,
		WrapArgs:     append([]string(nil), gc.WrapArgs...),
		Timeout:      gc.Timeout,
		WorkingDir:   gc.WorkingDir,
		Env:          map[string]string{},
		EnvBlackList: append([]string(nil), gc.EnvBlackList...),
		writers:      append([]func() io.Reader(nil), gc.writers...),
		ptyStdout:    gc.ptyStdout,
		ptyStderr:    gc.ptyStderr,
		ptyStdin:     gc.ptyStdin,
	}

	for k, v := range gc.Env {
		com.Env[k] = v
	}

	return com
}

func (gc *Command) WithPTY(stdin bool, stdout bool, stderr bool) {
	gc.ptyStdout = stdout
	gc.ptyStderr = stderr
	gc.ptyStdin = stdin
}

func (gc *Command) Feed(reader io.Reader) {
	gc.writers = append(gc.writers, func() io.Reader {
		return reader
	})
}

func (gc *Command) FeedFunction(writers ...func() io.Reader) {
	gc.writers = append(gc.writers, writers...)
}

//nolint:gocognit
func (gc *Command) Run() error {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	if gc.exec != nil {
		return ErrAlreadyStarted
	}

	var ctxCancel context.CancelFunc

	// Get a timing-out context
	timeout := gc.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	// Create a contextual command with that
	gc.exec = &Execution{}
	gc.exec.context, ctxCancel = context.WithTimeout(context.Background(), timeout)
	gc.exec.cancel = ctxCancel
	gc.exec.command = gc.commandWithContext(gc.exec.context)
	gc.exec.ioGroup, _ = errgroup.WithContext(gc.exec.context)

	var (
		stdoutReader, stderrReader io.Reader
		stdinWriter                io.WriteCloser
		mty, tty                   *os.File
		err                        error
	)

	// If we want a pty, configure it now
	if gc.ptyStdout || gc.ptyStderr || gc.ptyStdin {
		mty, tty, _ = pty.Open()
		_, _ = term.MakeRaw(int(tty.Fd()))
		gc.exec.cancel = func() {
			// Closing the master is technically enough, but would leak a revoked fd for the tty
			_ = mty.Close()
			_ = tty.Close()
			// Also can the context
			ctxCancel()
		}

		if gc.ptyStdin {
			gc.exec.command.Stdin = tty
			stdinWriter = mty
		}

		if gc.ptyStdout {
			gc.exec.command.Stdout = tty
			stdoutReader = mty
		}

		if gc.ptyStderr {
			gc.exec.command.Stderr = tty
			stderrReader = mty
		}
	}

	// If we do not have a pty, just use the pipes
	if gc.exec.command.Stdout == nil {
		stdoutReader, err = gc.exec.command.StdoutPipe()
		if err != nil {
			return errors.Join(ErrFailedAcquiringPipe, err)
		}
	}

	if gc.exec.command.Stderr == nil {
		stderrReader, err = gc.exec.command.StderrPipe()
		if err != nil {
			return errors.Join(ErrFailedAcquiringPipe, err)
		}
	}

	// Only create a pipe for stdin if we intend on writing to stdin. Otherwise, some process may hang awaiting content
	if gc.exec.command.Stdin == nil && len(gc.writers) > 0 {
		stdinWriter, err = gc.exec.command.StdinPipe()
		if err != nil {
			return errors.Join(ErrFailedAcquiringPipe, err)
		}
	}

	// Start the command
	if err = gc.exec.command.Start(); err != nil {
		return errors.Join(ErrFailedStarting, err)
	}

	// Start writing to stdin, whether pty or pipe
	if stdinWriter != nil {
		gc.exec.ioGroup.Go(func() error {
			for _, writer := range gc.writers {
				if _, copyErr := io.Copy(stdinWriter, writer()); err != nil {
					return errors.Join(ErrFailedWrite, copyErr)
				}
			}

			return nil
		})
	}

	// Start reading stdout...
	gc.exec.ioGroup.Go(func() error {
		buf := &bytes.Buffer{}
		_, copyErr := io.Copy(buf, stdoutReader)
		gc.exec.Stdout = buf.String()

		if copyErr != nil {
			copyErr = errors.Join(ErrFailedRead, copyErr)
		}

		return copyErr
	})

	// ... and stderr
	gc.exec.ioGroup.Go(func() error {
		// Avoid reading twice if we have a pty on both
		if gc.ptyStdout && gc.ptyStderr {
			return nil
		}

		buf := &bytes.Buffer{}
		_, copyErr := io.Copy(buf, stderrReader)
		gc.exec.Stderr = buf.String()

		if copyErr != nil {
			copyErr = errors.Join(ErrFailedRead, copyErr)
		}

		return copyErr
	})

	return nil
}

func (gc *Command) Wait() (*Execution, error) {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	if gc.exec == nil {
		return nil, ErrNotStarted
	}

	defer gc.exec.cancel()

	// Wait for io to be done BEFORE waiting for the command
	// Also let the error bubble-up for now
	_ = gc.exec.ioGroup.Wait()

	// Wait for the command now
	err := gc.exec.command.Wait()

	var exitCode int

	switch {
	case errors.Is(gc.exec.context.Err(), context.DeadlineExceeded):
		exitCode = internal.ExitCodeTimeout
		err = nil
	case errors.Is(gc.exec.context.Err(), context.Canceled):
		exitCode = internal.ExitCodeCancelled
		err = nil
	default:
		exitCode = gc.exec.command.ProcessState.ExitCode()
	}

	gc.exec.ExitCode = exitCode
	gc.exec.Environ = gc.exec.command.Environ()

	return gc.exec, err
}

func (gc *Command) Signal(sig os.Signal) error {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	if gc.exec == nil {
		return ErrNotStarted
	}

	// FIXME: is this always safe? Could process be nil?
	err := gc.exec.command.Process.Signal(sig)
	if err != nil {
		err = errors.Join(ErrFailedSendingSignal, err)
	}

	return err
}

func (gc *Command) commandWithContext(ctx context.Context) *exec.Cmd {
	args := gc.Args
	binary := gc.Binary

	if gc.Prepend != nil {
		args = append(gc.Prepend, args...)
	}

	if gc.Wrap != "" {
		args = append([]string{gc.Binary}, args...)
		args = append(gc.WrapArgs, args...)
		binary = gc.Wrap
	}

	cmd := exec.CommandContext(ctx, binary, args...)

	// Add dir
	cmd.Dir = gc.WorkingDir

	// Attach platform ProcAttr and get back possible custom cancellation routine
	if cancellation := addAttr(cmd); cancellation != nil {
		cmd.Cancel = cancellation
	}

	// Deal with env
	cmd.Env = []string{}

	// TODO: replace with regexps?
	// TODO: and/or whitelist?
	for _, envValue := range os.Environ() {
		add := true

		for _, b := range gc.EnvBlackList {
			if b == "*" || strings.HasPrefix(envValue, b+"=") {
				add = false

				break
			}
		}

		if add {
			cmd.Env = append(cmd.Env, envValue)
		}
	}

	// Attach any extra env we have
	for k, v := range gc.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	return cmd
}
