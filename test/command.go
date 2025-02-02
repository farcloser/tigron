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

package test

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"

	"go.farcloser.world/tigron/test/internal"
	"go.farcloser.world/tigron/test/internal/pty"
)

// CustomizableCommand is an interface meant for people who want to heavily customize the base command
// of their test case.
type CustomizableCommand interface {
	TestableCommand

	PrependArgs(args ...string)
	// WithBlacklist allows to filter out unwanted variables from the embedding environment -
	// default it pass any that is defined by WithEnv
	WithBlacklist(env []string)

	// withEnv *copies* the passed map to the environment of the command to be executed
	// Note that this will override any variable defined in the embedding environment
	withEnv(env map[string]string)
	// withTempDir specifies a temporary directory to use
	withTempDir(path string)
	// WithConfig allows passing custom config properties from the test to the base command
	withConfig(config Config)
	withT(t *testing.T)
	// Clear does a clone, but will clear binary and arguments, but retain the env, or any other custom properties
	// Gotcha: if GenericCommand is embedded with a custom Run and an overridden clear to return the embedding type
	// the result will be the embedding command, no longer the GenericCommand
	clear() TestableCommand

	// Will manipulate specific configuration option on the command
	// Note that config is a copy of the test config
	// Any modification done here will not be passed along to subtests, although they are shared
	// amongst all commands of the test.
	write(key ConfigKey, value ConfigValue)
	read(key ConfigKey) ConfigValue
}

// GenericCommand is a concrete Command implementation.
type GenericCommand struct {
	Config  Config
	TempDir string
	Env     map[string]string

	t *testing.T

	helperBinary string
	helperArgs   []string
	prependArgs  []string
	mainBinary   string
	mainArgs     []string

	envBlackList []string
	stdin        io.Reader
	async        bool
	pty          bool
	timeout      time.Duration
	workingDir   string

	result    *icmd.Result
	rawStdErr string
}

func (gc *GenericCommand) WithBinary(binary string) {
	gc.mainBinary = binary
}

func (gc *GenericCommand) WithArgs(args ...string) {
	gc.mainArgs = append(gc.mainArgs, args...)
}

func (gc *GenericCommand) WithWrapper(binary string, args ...string) {
	gc.helperBinary = binary
	gc.helperArgs = args
}

func (gc *GenericCommand) WithPseudoTTY() {
	gc.pty = true
}

func (gc *GenericCommand) WithStdin(r io.Reader) {
	gc.stdin = r
}

func (gc *GenericCommand) WithCwd(path string) {
	gc.workingDir = path
}

// TODO: it should be possible to timeout execution
// Primitives (gc.timeout) is here, it is just a matter of exposing a WithTimeout method
// - UX to be decided
// - validate use case: would we ever need this?

func (gc *GenericCommand) Run(expect *Expected) {
	if gc.t != nil {
		gc.t.Helper()
	}

	var result *icmd.Result

	var env []string

	if gc.async {
		result = icmd.WaitOnCmd(gc.timeout, gc.result)
		env = gc.result.Cmd.Env
	} else {
		iCmdCmd := gc.boot()
		env = iCmdCmd.Env

		if gc.pty {
			psty, tty, _ := pty.Open()
			iCmdCmd.Stdin = tty
			iCmdCmd.Stdout = tty
			iCmdCmd.Stderr = tty

			defer psty.Close()
			defer tty.Close()
		}

		// Run it
		result = icmd.RunCmd(iCmdCmd)
	}

	gc.rawStdErr = result.Stderr()

	// Check our expectations, if any
	if expect != nil {
		// Build the debug string - additionally attach the env (which iCmd does not do)
		debug := result.String() + "Env:\n" + strings.Join(env, "\n")

		// ExitCode goes first
		switch expect.ExitCode {
		case internal.ExitCodeNoCheck:
			// -2 means we do not care at all about exit code
		case internal.ExitCodeGenericFail:
			// -1 means any error
			assert.Assert(gc.t, result.ExitCode != 0,
				"Expected exit code to be different than 0\n"+debug)
		default:
			assert.Assert(gc.t, expect.ExitCode == result.ExitCode,
				fmt.Sprintf("Expected exit code: %d\n", expect.ExitCode)+debug)
		}

		// Range through the expected errors and confirm they are seen on stderr
		for _, expectErr := range expect.Errors {
			assert.Assert(gc.t, strings.Contains(gc.rawStdErr, expectErr.Error()),
				fmt.Sprintf("Expected error: %q to be found in stderr\n", expectErr.Error())+debug)
		}

		// Finally, check the output if we are asked to
		if expect.Output != nil {
			expect.Output(result.Stdout(), debug, gc.t)
		}
	}
}

func (gc *GenericCommand) Stderr() string {
	return gc.rawStdErr
}

func (gc *GenericCommand) Background(timeout time.Duration) {
	// Run it
	gc.async = true

	i := gc.boot()

	gc.timeout = timeout
	gc.result = icmd.StartCmd(i)
}

func (gc *GenericCommand) withEnv(env map[string]string) {
	if gc.Env == nil {
		gc.Env = map[string]string{}
	}

	for k, v := range env {
		gc.Env[k] = v
	}
}

func (gc *GenericCommand) withTempDir(path string) {
	gc.TempDir = path
}

func (gc *GenericCommand) WithBlacklist(env []string) {
	gc.envBlackList = env
}

func (gc *GenericCommand) withConfig(config Config) {
	gc.Config = config
}

func (gc *GenericCommand) PrependArgs(args ...string) {
	gc.prependArgs = append(gc.prependArgs, args...)
}

//nolint:ireturn
func (gc *GenericCommand) Clone() TestableCommand {
	// Copy the command and return a new one - with almost everything from the parent command
	com := *gc
	com.result = nil
	com.stdin = nil
	com.timeout = 0
	com.rawStdErr = ""
	// Clone Env
	com.Env = make(map[string]string, len(gc.Env))
	for k, v := range gc.Env {
		com.Env[k] = v
	}

	return &com
}

func (gc *GenericCommand) T() *testing.T {
	return gc.t
}

//nolint:ireturn
func (gc *GenericCommand) clear() TestableCommand {
	com := *gc
	com.mainBinary = ""
	com.helperBinary = ""
	com.mainArgs = []string{}
	com.prependArgs = []string{}
	com.helperArgs = []string{}
	// Clone Env
	com.Env = make(map[string]string, len(gc.Env))
	// Reset configuration
	com.Config = &config{}
	for k, v := range gc.Env {
		com.Env[k] = v
	}

	return &com
}

func (gc *GenericCommand) withT(t *testing.T) {
	t.Helper()
	gc.t = t
}

func (gc *GenericCommand) read(key ConfigKey) ConfigValue {
	return gc.Config.Read(key)
}

func (gc *GenericCommand) write(key ConfigKey, value ConfigValue) {
	gc.Config.Write(key, value)
}

func (gc *GenericCommand) boot() icmd.Cmd {
	// This is a helper function, not to appear in the debugging output
	if gc.t != nil {
		gc.t.Helper()
	}

	binary := gc.mainBinary
	//nolint:gocritic
	args := append(gc.prependArgs, gc.mainArgs...)

	if gc.helperBinary != "" {
		args = append([]string{binary}, args...)
		args = append(gc.helperArgs, args...)
		binary = gc.helperBinary
	}

	// Create the command and set the env
	// TODO: do we really need iCmd?
	gc.t.Log(binary, strings.Join(args, " "))

	iCmdCmd := icmd.Command(binary, args...)
	iCmdCmd.Env = []string{}

	for _, envValue := range os.Environ() {
		add := true

		for _, b := range gc.envBlackList {
			if strings.HasPrefix(envValue, b+"=") {
				add = false

				break
			}
		}

		if add {
			iCmdCmd.Env = append(iCmdCmd.Env, envValue)
		}
	}

	// Ensure the subprocess gets executed in a temporary directory unless explicitly instructed otherwise
	iCmdCmd.Dir = gc.workingDir

	if gc.stdin != nil {
		iCmdCmd.Stdin = gc.stdin
	}

	// Attach any extra env we have
	for k, v := range gc.Env {
		iCmdCmd.Env = append(iCmdCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	return iCmdCmd
}
