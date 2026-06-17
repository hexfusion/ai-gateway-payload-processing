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

package vertex

import (
	"fmt"

	"github.com/opendatahub-io/ai-gateway-payload-processing/pkg/plugins/api-translation/translator"
	errcommon "github.com/llm-d/llm-d-inference-payload-processor/pkg/common/error"
)

const (
	// vertexOpenAIPathTemplate builds the full Vertex AI OpenAI-compatible endpoint path.
	// Reference: https://cloud.google.com/vertex-ai/generative-ai/docs/reference/rest/v1/projects.locations.endpoints.chat/completions
	vertexOpenAIPathTemplate = "/v1/projects/%s/locations/%s/endpoints/%s/chat/completions"
)

// compile-time interface check
var _ translator.Translator = &VertexOpenAITranslator{}

// NewVertexOpenAITranslator initializes a new VertexOpenAITranslator with the given
// GCP project, location, and endpoint, used to construct the full API path.
func NewVertexOpenAITranslator(project, location, endpoint string) *VertexOpenAITranslator {
	return &VertexOpenAITranslator{
		path: fmt.Sprintf(vertexOpenAIPathTemplate, project, location, endpoint),
		stripper: translator.NewResponseFieldStripper([]string{
			"usage.extra_properties",
		}),
	}
}

// VertexOpenAITranslator targets Vertex AI's native OpenAI-compatible chat/completions endpoint.
// Unlike VertexTranslator (which converts to Gemini generateContent format), this translator
// passes the request body through unchanged since the endpoint accepts OpenAI format natively.
type VertexOpenAITranslator struct {
	path     string
	stripper *translator.ResponseFieldStripper
}

// TranslateRequest rewrites the path to target Vertex AI's OpenAI-compatible endpoint.
// The request body is not mutated since the endpoint accepts OpenAI chat completions format as-is.
func (t *VertexOpenAITranslator) TranslateRequest(body map[string]any) (translatedBody map[string]any,
	headersToMutate map[string]string, headersToRemove []string, err error) {
	model, _ := body["model"].(string)
	if model == "" {
		return nil, nil, nil, errcommon.Error{Code: errcommon.BadRequest, Msg: "model field is required"}
	}

	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return nil, nil, nil, errcommon.Error{Code: errcommon.BadRequest, Msg: "messages field is required and must not be empty"}
	}

	headersToMutate = map[string]string{
		":path":        t.path,
		"content-type": "application/json",
	}

	return nil, headersToMutate, nil, nil
}

// TranslateResponse strips Vertex AI-specific fields from the response.
// The endpoint returns standard OpenAI format with extra fields like usage.extra_properties
// (verified against aiplatform.googleapis.com/v1/.../chat/completions).
func (t *VertexOpenAITranslator) TranslateResponse(body map[string]any, model string) (translatedBody map[string]any, err error) {
	result, _ := t.stripper.Strip(body)
	return result, nil
}
