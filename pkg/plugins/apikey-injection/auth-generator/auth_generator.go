/*
Copyright 2026 The opendatahub.io Authors.

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

package authgenerator

import (
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/plugin"
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/requesthandling"
)

// AuthHeadersGenerator generates auth headers from credential fields.
// Each implementation defines which fields it requires from the credentials map.
type AuthHeadersGenerator interface {
	// ExtractRequestData pulls provider-specific data from the request/CycleState
	// that is needed for auth header generation (e.g., body, endpoint for SigV4).
	// The returned map is merged into credentialsData before GenerateAuthHeaders is called.
	// Implementations that don't need request data should return nil, nil.
	ExtractRequestData(cycleState *plugin.CycleState, request *requesthandling.InferenceRequest) (map[string]string, error)

	GenerateAuthHeaders(credentialsData map[string]string) (map[string]string, error)
}
