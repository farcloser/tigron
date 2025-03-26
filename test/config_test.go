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

//nolint:testpackage
package test

import (
	"testing"

	"go.farcloser.world/tigron/internal/assertive"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	// Create
	cfg := WithConfig("test", "something")

	assertive.IsEqual(t, string(cfg.Read("test")), "something")

	// Write
	cfg.Write("test-write", "else")

	// Overwrite
	cfg.Write("test", "one")

	assertive.IsEqual(t, string(cfg.Read("test")), "one")
	assertive.IsEqual(t, string(cfg.Read("test-write")), "else")

	assertive.IsEqual(t, string(cfg.Read("doesnotexist")), "")

	// Test adoption
	cfg2 := WithConfig("test", "two")
	cfg2.Write("adopt", "two")

	//nolint:forcetypeassert
	cfg.(*config).adopt(cfg2)

	assertive.IsEqual(t, string(cfg.Read("test")), "one")
	assertive.IsEqual(t, string(cfg.Read("adopt")), "two")
}
