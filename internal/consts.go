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

// Package internal provides an assert library, pty, a command wrapper, and a leak detection library
// for internal use in Tigron.
// The objective for these is not to become generic use-cases libraries, but instead to deliver what
// Tigron needs
// in the simplest possible form.
package internal

// This is duplicated from `expect` to avoid circular imports.
const (
	ExitCodeSuccess     = 0
	ExitCodeGenericFail = -10
	ExitCodeNoCheck     = -11
	ExitCodeTimeout     = -12
	ExitCodeSignaled    = -13
	// ExitCodeCancelled = -14.
)
