/*
Copyright 2026 Nscale.

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

package ids

import (
	"github.com/google/uuid"
)

// InstanceID is a UUID-backed identifier for compute instances. It is a distinct
// named type so the compiler prevents accidental interchange with any other ID type.
// UnmarshalText delegates to uuid.UUID, so the oapi-codegen runtime rejects
// non-UUID path parameter values before any handler is reached.
//
//nolint:recvcheck // UnmarshalText must be a pointer receiver; String/MarshalText are value receivers for fmt.Stringer compatibility.
type InstanceID uuid.UUID

func (v InstanceID) String() string                { return uuid.UUID(v).String() }
func (v InstanceID) MarshalText() ([]byte, error)  { return uuid.UUID(v).MarshalText() }
func (v *InstanceID) UnmarshalText(b []byte) error { return unmarshalUUID((*uuid.UUID)(v), b) }

// unmarshalUUID is the shared implementation for all UnmarshalText methods.
func unmarshalUUID(dst *uuid.UUID, text []byte) error {
	var id uuid.UUID

	if err := id.UnmarshalText(text); err != nil {
		return err
	}

	*dst = id

	return nil
}

// ParseInstanceID parses s as a UUID into an InstanceID, returning
// an error if s is not a valid UUID.
func ParseInstanceID(s string) (InstanceID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return InstanceID{}, err
	}

	return InstanceID(id), nil
}

// MustParseInstanceID parses s as a UUID into an InstanceID. It panics if s is
// not a valid UUID, so use it only with compile-time constants and in tests.
func MustParseInstanceID(s string) InstanceID {
	return InstanceID(uuid.MustParse(s))
}
