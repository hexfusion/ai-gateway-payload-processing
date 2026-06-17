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

package openai

import (
	"github.com/opendatahub-io/ai-gateway-payload-processing/pkg/plugins/api-translation/translator"
	errcommon "github.com/llm-d/llm-d-inference-payload-processor/pkg/common/error"
)

const (
	// OpenAI-compatible endpoint path
	openAIPath = "/v1/chat/completions"
)

// compile-time interface check
var _ translator.Translator = &OpenAITranslator{}

// NewOpenAITranslator initializes a new OpenAITranslator and returns its pointer.
func NewOpenAITranslator() *OpenAITranslator {
	return &OpenAITranslator{}
}

// OpenAITranslator only set the relative path.
// this is needed in case the original request uses different relative path.
type OpenAITranslator struct{}

// TranslateRequest rewrites the path and headers for OpenAI.
func (t *OpenAITranslator) TranslateRequest(body map[string]any) (map[string]any, map[string]string, []string, error) {
	model, _ := body["model"].(string)
	if model == "" {
		return nil, nil, nil, errcommon.Error{Code: errcommon.BadRequest, Msg: "model field is required"}
	}

	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return nil, nil, nil, errcommon.Error{Code: errcommon.BadRequest, Msg: "messages field is required and must not be empty"}
	}

	headers := map[string]string{
		":path": openAIPath, // needed in case the original request uses different relative path and include the model.
	}

	// Return nil body — no body mutation is needed, OpenAI request format is used as-is.
	return nil, headers, nil, nil
}

// TranslateResponse is a no-op.
func (t *OpenAITranslator) TranslateResponse(body map[string]any, model string) (map[string]any, error) {
	return nil, nil
}
