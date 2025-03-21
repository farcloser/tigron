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

package com_test

import (
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"go.farcloser.world/tigron/expect"
	"go.farcloser.world/tigron/test/internal"
	"go.farcloser.world/tigron/test/internal/com"
)

const windows = "windows"

func TestSimple(t *testing.T) {
	t.Parallel()

	command := &com.Command{
		Binary: "echo",
		Args:   []string{"one"},
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error s	hould be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "one\n", res.Stdout, "stdout should be the string 'one'")
	assert.Equal(t, "", res.Stderr, "stderr should be empty")
}

func TestWorkingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	command := &com.Command{
		Binary:     "pwd",
		WorkingDir: dir,
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	// Note:
	// - darwin will link to /private/DIR
	// - windows+ming will go to C:\Users\RUNNER~1\AppData\Local\Temp\
	if runtime.GOOS == windows {
		t.Skip("skipping last check on windows, see note")
	}

	assert.Assert(t, strings.HasSuffix(res.Stdout, dir+"\n"))
}

func TestEnvBlacklist(t *testing.T) {
	t.Setenv("FOO", "BAR")
	t.Setenv("FOOBAR", "BARBAR")

	command := &com.Command{
		Binary: "env",
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Assert(t, strings.Contains(res.Stdout, "FOO=BAR"))
	assert.Assert(t, strings.Contains(res.Stdout, "FOOBAR=BARBAR"))

	command = &com.Command{
		Binary:       "env",
		EnvBlackList: []string{"FOO"},
	}

	err = command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err = command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Assert(t, !strings.Contains(res.Stdout, "FOO=BAR"))
	assert.Assert(t, strings.Contains(res.Stdout, "FOOBAR=BARBAR"))

	// On windows, with mingw, SYSTEMROOT,TERM and HOME will be forcefully added to the environment regardless
	if runtime.GOOS == windows {
		t.Skip("Windows/mingw will always repopulate the environment with extra variables we cannot bypass")
	}

	command = &com.Command{
		Binary:       "env",
		EnvBlackList: []string{"*"},
	}

	err = command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err = command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, res.Stdout, "")
}

func TestEnvAdd(t *testing.T) {
	t.Setenv("FOO", "BAR")
	t.Setenv("BLED", "BLED")
	t.Setenv("BAZ", "OLD")

	command := &com.Command{
		Binary: "env",
		Env: map[string]string{
			"FOO":  "REPLACE",
			"BAR":  "NEW",
			"BLED": "EXPLICIT",
		},
		EnvBlackList: []string{"BLED"},
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Assert(t, strings.Contains(res.Stdout, "FOO=REPLACE"))
	assert.Assert(t, strings.Contains(res.Stdout, "BAR=NEW"))
	assert.Assert(t, strings.Contains(res.Stdout, "BAZ=OLD"))
	assert.Assert(t, strings.Contains(res.Stdout, "BLED=EXPLICIT"))
}

func TestStdoutStderr(t *testing.T) {
	t.Parallel()

	command := &com.Command{
		Binary: "bash",
		Args:   []string{"-c", "--", "echo onstdout; >&2 echo onstderr;"},
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "onstdout\n", res.Stdout, "stdout should be 'onstdout'")
	assert.Equal(t, "onstderr\n", res.Stderr, "stderr should be 'onstderr'")
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	start := time.Now()
	command := &com.Command{
		Binary:  "bash",
		Args:    []string{"-c", "--", "echo one; sleep 1; sleep 1; sleep 1; sleep 1; echo two"},
		Timeout: 1 * time.Second,
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	end := time.Now()

	assert.NilError(t, err, "error should be nil")
	assert.Equal(t, internal.ExitCodeTimeout, res.ExitCode, "exit code should be timeout")
	assert.Equal(t, "one\n", res.Stdout, "stdout should be the string 'one'")
	assert.Equal(t, "", res.Stderr, "stderr should be empty")
	assert.Assert(t, end.Sub(start) < 2*time.Second, "elapsed time should be less than two seconds")
}

func TestTimeoutOther(t *testing.T) {
	t.Parallel()

	start := time.Now()
	command := &com.Command{
		Binary:  "sleep",
		Args:    []string{"5"},
		Timeout: 1 * time.Second,
	}

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	end := time.Now()

	assert.NilError(t, err, "error should be nil")
	assert.Equal(t, internal.ExitCodeTimeout, res.ExitCode, "exit code should be timeout")
	assert.Assert(t, end.Sub(start) < 2*time.Second, "elapsed time should be less than two seconds")
}

func TestPTYStdout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windows {
		t.Skip("PTY are not supported on Windows")
	}

	command := &com.Command{
		Binary:  "bash",
		Args:    []string{"-c", "--", "echo onstdout; >&2 echo onstderr;"},
		Timeout: 1 * time.Second,
	}

	command.WithPTY(true, true, false)

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "onstdout\n", res.Stdout, "stdout should be 'onstdout'")
	assert.Equal(t, "onstderr\n", res.Stderr, "stderr should be 'onstderr'")
}

func TestPTYStderr(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windows {
		t.Skip("PTY are not supported on Windows")
	}

	command := &com.Command{
		Binary:  "bash",
		Args:    []string{"-c", "--", "echo onstdout; >&2 echo onstderr;"},
		Timeout: 1 * time.Second,
	}

	command.WithPTY(true, false, true)

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "onstdout\n", res.Stdout, "stdout should be 'onstdout'")
	assert.Equal(t, "onstderr\n", res.Stderr, "stderr should be 'onstderr'")
}

func TestPTYBoth(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windows {
		t.Skip("PTY are not supported on Windows")
	}

	command := &com.Command{
		Binary:  "bash",
		Args:    []string{"-c", "--", "echo onstdout; >&2 echo onstderr;"},
		Timeout: 1 * time.Second,
	}

	command.WithPTY(true, true, true)

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "onstdout\nonstderr\n", res.Stdout, "stdout should be 'onstdout'")
	assert.Equal(t, "", res.Stderr, "stderr should be 'onstderr'")
}

func TestWriteStdin(t *testing.T) {
	t.Parallel()

	command := &com.Command{
		Binary: "bash",
		Args: []string{
			"-c", "--",
			"read line1; read line2; read line3; printf 'from stdin%s%s%s' \"$line1\" \"$line2\" \"$line3\";",
		},
		Timeout: 1 * time.Second,
	}

	command.FeedFunction(func() io.Reader {
		time.Sleep(100 * time.Millisecond)

		return strings.NewReader("hello first\n")
	})

	command.Feed(strings.NewReader("hello world\n"))
	command.Feed(strings.NewReader("hello again\n"))

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, 0, res.ExitCode, "exit code should be 0")
	assert.Equal(t, "from stdinhello firsthello worldhello again", res.Stdout)
}

func TestWritePTYStdin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windows {
		t.Skip("PTY are not supported on Windows")
	}

	command := &com.Command{
		Binary:  "bash",
		Args:    []string{"-c", "--", "[ -t 0 ] || { echo not a pty; exit 41; }; cat /dev/stdin"},
		Timeout: 1 * time.Second,
	}

	command.WithPTY(true, true, false)

	command.FeedFunction(func() io.Reader {
		time.Sleep(100 * time.Millisecond)

		return strings.NewReader("hello first")
	})

	command.Feed(strings.NewReader("hello world"))
	command.Feed(strings.NewReader("hello again"))

	err := command.Run()
	assert.NilError(t, err, "error should be nil, was", err)

	res, err := command.Wait()

	assert.NilError(t, err, "error should be nil, was", err)
	assert.Equal(t, expect.ExitCodeTimeout, res.ExitCode, "exit code should be timeout")
	assert.Equal(t, "hello firsthello worldhello again", res.Stdout)
}

// FIXME: this is currently not working - maybe because of bash behavior wrt signals?
// func TestSignal(t *testing.T) {
//	t.Parallel()
//
//	var usig os.Signal
//	usig = syscall.SIGUSR1
//	sig := strconv.Itoa(int(usig.(syscall.Signal)))
//
//	command := &com.Command{
//		Binary: "bash",
//		Args:   []string{fmt.Sprintf("-c", "--", "echo entry; sig_msg () { printf \"caught\\n\"; exit 42; };
//		trap sig_msg %s; sleep 3600; }", sig)},
//		//Binary:  "sleep",
//		//Args:    []string{"3600"},
//		Timeout: 3 * time.Second,
//	}
//
//	command.Run()
//	// A bit arbitrary - just want to wait for stdout to go through before sending the signal
//	time.Sleep(100 * time.Millisecond)
//	f := command.Signal(usig)
//	fmt.Println(f)
//
//	res, err := command.Wait()
//	fmt.Println(res.ExitCode)
//	fmt.Println(res.Stdout)
//	fmt.Println(res.Stderr)
//	fmt.Println(err)
//	assert.Equal(t, "hello firsthello worldhello again", res.Stdout)
//	//assert.NilError(t, err, "error should be nil, was", err)
//	//assert.Equal(t, -12, res.ExitCode, "exit code should be timeout")
//
//}
