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

//nolint:varnamelen
package assertive

import (
	"errors"
	"strings"
	"time"
)

type T interface {
	Helper()
	FailNow()
	Fail()
	Log(args ...interface{})
}

func failNow(t T, msg ...string) {
	t.Helper()

	if len(msg) > 0 {
		for _, m := range msg {
			t.Log(m)
		}
	}

	t.FailNow()
}

func ErrorIsNil(t T, err error, msg ...string) {
	t.Helper()

	if err != nil {
		t.Log("expecting nil error, but got:", err)
		failNow(t, msg...)
	}
}

func ErrorIs(t T, err error, compErr error, msg ...string) {
	t.Helper()

	if !errors.Is(err, compErr) {
		t.Log("expected error to be:", compErr, "- instead it is:", err)
		failNow(t, msg...)
	}
}

func IsEqual(t T, actual, expected interface{}, msg ...string) {
	t.Helper()

	if !isEqual(t, actual, expected) {
		t.Log("expected:", actual, " - to be equal to:", expected)
		failNow(t, msg...)
	}
}

func IsNotEqual(t T, actual, expected interface{}, msg ...string) {
	t.Helper()

	if isEqual(t, actual, expected) {
		t.Log("expected:", actual, " - to be equal to:", expected)
		failNow(t, msg...)
	}
}

func isEqual(t T, actual, expected interface{}) bool {
	t.Helper()

	truthy := false
	// FIXME: this is risky and limited. Right now this is fine internally, but should be better if this becomes public.
	if actual == expected {
		truthy = true
	}

	return truthy
}

func StringContains(t T, actual string, contains string, msg ...string) {
	t.Helper()

	if !strings.Contains(actual, contains) {
		t.Log("expected:", actual, " - to contain:", contains)
		failNow(t, msg...)
	}
}

func StringDoesNotContain(t T, actual string, contains string, msg ...string) {
	t.Helper()

	if strings.Contains(actual, contains) {
		t.Log("expected:", actual, " - to NOT contain:", contains)
		failNow(t, msg...)
	}
}

func StringHasSuffix(t T, actual string, suffix string, msg ...string) {
	t.Helper()

	if !strings.HasSuffix(actual, suffix) {
		t.Log("expected:", actual, " - to end with:", suffix)
		failNow(t, msg...)
	}
}

func StringHasPrefix(t T, actual string, prefix string, msg ...string) {
	t.Helper()

	if !strings.HasPrefix(actual, prefix) {
		t.Log("expected:", actual, " - to start with:", prefix)
		failNow(t, msg...)
	}
}

func DurationIsLessThan(t T, actual, expected time.Duration, msg ...string) {
	t.Helper()

	if actual >= expected {
		t.Log("expected:", actual, " - to be less than:", expected)
		failNow(t, msg...)
	}
}

func True(t T, comp bool, msg ...string) bool {
	t.Helper()

	if !comp {
		failNow(t, msg...)
	}

	return comp
}

func Check(t T, comp bool, msg ...string) bool {
	t.Helper()

	if !comp {
		for _, m := range msg {
			t.Log(m)
		}

		t.Fail()
	}

	return comp
}
