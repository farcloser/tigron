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
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Behavior map:
// - a command that fails at start (not found in PATH) will return:
//    * Run: err = ErrFaulty (ErrExecFailedStarting)
//    * Wait: err = ErrFaulty (ErrExecFailedStarting)
//    * ExitCode = -1
// - a command that times-out on Wait will return:
//    * Run: err = nil
//    * Wait: err = ErrExecutionTimeout
//    * ExitCode = -1
// - a command that starts, but fails, will return:
//    * Run: err = nil
//    * Wait: err = ErrExecutionFailed
//    * ExitCode = X > 0 (X is controlled by the binary being called)
// - a command that gets interrupted by a signal (not being caught):
//    * Run: err = nil
//	  * Signal: err = nil
//    * Wait: err = ErrExecutionSignaled
//    * ExitCode = -1
//    * Signal = the os.Signal that was sent
// - a command that has already finished and that get sent a signal:
// 	  * Run: err = nil
//	  * Wait: err = nil
//	  * Signal: err = ErrExecAlreadyFinished
//    * ExitCode = X > 0 (X is controlled by the binary being called)
// - a command that has already finished without Wait and that get sent a signal:
// 	  * Run: err = nil
//	  * Signal: err = ErrFailedSendingSignal
//    * ExitCode = X > 0 (X is controlled by the binary being called)
// - a command that runs normally will return:
//    * Run: err = nil
//    * Wait: err = nil
//    * ExitCode = 0

const (
	defaultTimeout = 10 * time.Second
	delayAfterWait = 100 * time.Millisecond
)

var (
	ErrExecutionTimeout   = errors.New("command timed out")
	ErrExecutionCancelled = errors.New("command execution cancelled")
	ErrExecutionFailed    = errors.New("command returned a non-zero exit code")
	ErrExecutionSignaled  = errors.New("command execution signalled")

	ErrExecAlreadyStarted  = errors.New("command has already been started (double `Run`)")
	ErrExecNotStarted      = errors.New("command has not been started (call `Run` first)")
	ErrExecFailedStarting  = errors.New("command failed starting")
	ErrExecAlreadyFinished = errors.New("command is already finished")

	// ErrFailedSendingSignal may happen if sending a signal to an already terminated process.
	ErrFailedSendingSignal = errors.New("failed sending signal")
)

type Result struct {
	Environ  []string
	Stdout   string
	Stderr   string
	ExitCode int
	Signal   os.Signal
}

type Execution struct {
	//nolint:containedctx
	context context.Context
	cancel  context.CancelFunc
	command *exec.Cmd
	pipes   *stdPipes
	log     zerolog.Logger
	err     error
}

type Command struct {
	Binary      string
	PrependArgs []string
	Args        []string
	WrapBinary  string
	WrapArgs    []string
	Timeout     time.Duration

	WorkingDir string
	Env        map[string]string
	// FIXME: EnvBlackList might change for a better mechanism (regexp and/or whitelist + blacklist)
	EnvBlackList []string

	writers []func() io.Reader

	ptyStdout bool
	ptyStderr bool
	ptyStdin  bool

	exec   *Execution
	mutex  sync.Mutex
	result *Result
}

func (gc *Command) Clone() *Command {
	com := &Command{
		Binary:      gc.Binary,
		PrependArgs: append([]string(nil), gc.PrependArgs...),
		Args:        append([]string(nil), gc.Args...),
		WrapBinary:  gc.WrapBinary,
		WrapArgs:    append([]string(nil), gc.WrapArgs...),
		Timeout:     gc.Timeout,

		WorkingDir:   gc.WorkingDir,
		Env:          map[string]string{},
		EnvBlackList: append([]string(nil), gc.EnvBlackList...),

		writers: append([]func() io.Reader(nil), gc.writers...),

		ptyStdout: gc.ptyStdout,
		ptyStderr: gc.ptyStderr,
		ptyStdin:  gc.ptyStdin,
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

func (gc *Command) WithFeeder(writers ...func() io.Reader) {
	gc.writers = append(gc.writers, writers...)
}

func (gc *Command) Feed(reader io.Reader) {
	gc.writers = append(gc.writers, func() io.Reader {
		return reader
	})
}

func (gc *Command) Run(parentCtx context.Context) error {
	// Lock
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	// Protect against dumb calls
	if gc.result != nil {
		return ErrExecAlreadyFinished
	} else if gc.exec != nil {
		return ErrExecAlreadyStarted
	}

	var (
		ctx       context.Context
		ctxCancel context.CancelFunc
		pipes     *stdPipes
		cmd       *exec.Cmd
		err       error
	)

	// Get a timing-out context
	timeout := gc.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	ctx, ctxCancel = context.WithTimeout(parentCtx, timeout)

	// Create a contextual command, set the logger
	cmd = gc.buildCommand(ctx)
	logg := log.Logger.With().Ctx(ctx).Str("module", "com").Str("command", cmd.String()).Logger()

	gc.exec = &Execution{
		context: ctx,
		cancel:  ctxCancel,
		command: cmd,
		log:     logg,
	}

	// Prepare pipes
	pipes, err = newStdPipes(ctx, logg, gc.ptyStdout, gc.ptyStderr, gc.ptyStdin, gc.writers)
	if err != nil {
		ctxCancel()

		gc.exec.err = errors.Join(ErrExecFailedStarting, err)

		// No wrapping here - we do not even have pipes, and the command has not been started.

		return gc.exec.err
	}

	// Attach pipes
	gc.exec.pipes = pipes
	cmd.Stdout = pipes.stdout.writer
	cmd.Stderr = pipes.stderr.writer
	cmd.Stdin = pipes.stdin.reader

	// Start it
	if err = cmd.Start(); err != nil {
		// On failure, can the context, wrap whatever we have and return
		gc.exec.log.Warn().Err(err).Msg("start failed")

		gc.exec.err = errors.Join(ErrExecFailedStarting, err)

		_ = gc.wrap()

		defer ctxCancel()

		return gc.exec.err
	}

	select {
	case <-ctx.Done():
		// There is no good reason for this to happen, so, log it
		err = gc.wrap()

		gc.exec.log.Error().
			Err(ctx.Err()).
			Err(err).
			Str("stdout", gc.result.Stdout).
			Str("stderr", gc.result.Stderr).
			Int("exit", gc.result.ExitCode).
			Send()

		return err
	default:
	}

	return nil
}

func (gc *Command) Wait() (*Result, error) {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	switch {
	case gc.exec == nil:
		return nil, ErrExecNotStarted
	case gc.exec.err != nil:
		return gc.result, gc.exec.err
	case gc.result != nil:
		return gc.result, ErrExecAlreadyFinished
	}

	// Cancel the context in any case now
	defer gc.exec.cancel()

	// Wait for the command
	_ = gc.exec.command.Wait()

	// Capture timeout and cancellation
	select {
	case <-gc.exec.context.Done():
	default:
	}

	// Wrap the results and return
	err := gc.wrap()

	return gc.result, err
}

func (gc *Command) Signal(sig os.Signal) error {
	gc.mutex.Lock()
	defer gc.mutex.Unlock()

	if gc.exec == nil {
		return ErrExecNotStarted
	}

	err := gc.exec.command.Process.Signal(sig)
	if err != nil {
		err = errors.Join(ErrFailedSendingSignal, err)
	}

	return err
}

func (gc *Command) wrap() error {
	pipes := gc.exec.pipes
	cmd := gc.exec.command
	ctx := gc.exec.context

	// Close and drain the pipes
	pipes.closeCallee()
	_ = pipes.ioGroup.Wait()
	pipes.closeCaller()

	// Get the status, exitCode, signal, error
	var (
		status   syscall.WaitStatus
		signal   os.Signal
		exitCode int
		err      error
	)

	// XXXgolang: this is troubling. cmd.ProcessState.ExitCode() is always fine, even if cmd.ProcessState is nil.
	exitCode = cmd.ProcessState.ExitCode()

	if cmd.ProcessState != nil {
		var ok bool
		if status, ok = cmd.ProcessState.Sys().(syscall.WaitStatus); !ok {
			log.Panic().Msg("failed casting process state sys")
		}

		if status.Signaled() {
			signal = status.Signal()
			err = ErrExecutionSignaled
		} else if exitCode != 0 {
			err = ErrExecutionFailed
		}
	}

	// Catch-up on the context
	switch ctx.Err() {
	case context.DeadlineExceeded:
		err = ErrExecutionTimeout
	case context.Canceled:
		err = ErrExecutionCancelled
	default:
	}

	// Stuff everything in Result and return err
	gc.result = &Result{
		ExitCode: exitCode,
		Stdout:   pipes.fromStdout,
		Stderr:   pipes.fromStderr,
		Environ:  cmd.Environ(),
		Signal:   signal,
	}

	if gc.exec.err == nil {
		gc.exec.err = err
	}

	return gc.exec.err
}

func (gc *Command) buildCommand(ctx context.Context) *exec.Cmd {
	// Build arguments and binary
	args := gc.Args
	if gc.PrependArgs != nil {
		args = append(gc.PrependArgs, args...)
	}

	binary := gc.Binary

	if gc.WrapBinary != "" {
		args = append([]string{gc.Binary}, args...)
		args = append(gc.WrapArgs, args...)
		binary = gc.WrapBinary
	}

	cmd := exec.CommandContext(ctx, binary, args...)

	// Add dir
	cmd.Dir = gc.WorkingDir

	// Set wait delay after waits returns
	cmd.WaitDelay = delayAfterWait

	// Build env
	cmd.Env = []string{}
	// TODO: replace with regexps? and/or whitelist?
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

	// Attach any explicit env we have
	for k, v := range gc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Attach platform ProcAttr and get optional custom cancellation routine
	if cancellation := addAttr(cmd); cancellation != nil {
		cmd.Cancel = func() error {
			gc.exec.log.Trace().Msg("command cancelled")

			// Call the platform dependent cancellation routine
			return cancellation()
		}
	}

	return cmd
}
