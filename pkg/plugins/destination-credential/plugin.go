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

// Package destinationcredential injects a bearer credential keyed on the scheduler-chosen destination
// endpoint (x-gateway-destination-endpoint), not the model, for hub-and-spoke routing. Controller-free
// counterpart to destination-provider-resolver: credentials come from env vars, no CRs.
package destinationcredential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"

	logutil "github.com/llm-d/llm-d-inference-payload-processor/pkg/common/observability/logging"
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/plugin"
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/requesthandling"
)

const (
	// PluginType is the registered name for this plugin.
	PluginType = "destination-credential"

	defaultKeyHeader   = "x-gateway-destination-endpoint"
	defaultHeaderName  = "Authorization"
	defaultValuePrefix = "Bearer "
)

// compile-time type validation
var _ requesthandling.RequestProcessor = &Plugin{}

// credential maps a destination to the environment variable holding its token.
type credential struct {
	// TokenEnv is the name of the environment variable holding the bearer token for the destination.
	TokenEnv string `json:"tokenEnv"`
}

// Config defines the JSON configuration structure for the plugin.
type Config struct {
	// KeyHeader is the request header carrying the chosen destination. Defaults to x-gateway-destination-endpoint.
	KeyHeader string `json:"keyHeader"`
	// HeaderName is the header to inject the credential into. Defaults to Authorization.
	HeaderName string `json:"headerName"`
	// ValuePrefix is prepended to the token. Defaults to "Bearer ". Set to "" via valuePrefixSet for no prefix.
	ValuePrefix    string `json:"valuePrefix"`
	ValuePrefixSet bool   `json:"valuePrefixSet"`
	// Credentials maps a destination value (as emitted in KeyHeader, e.g. "10.0.0.1:443") to its token env var.
	Credentials map[string]credential `json:"credentials"`
}

// Plugin injects a per-destination bearer credential keyed on the scheduler-chosen endpoint header.
type Plugin struct {
	typedName   plugin.TypedName
	keyHeader   string
	headerName  string
	valuePrefix string
	credentials map[string]credential
}

// Factory defines the factory function for the destination-credential plugin.
func Factory(name string, rawParameters json.RawMessage, _ plugin.Handle) (plugin.Plugin, error) {
	var config Config
	if len(rawParameters) > 0 {
		if err := json.Unmarshal(rawParameters, &config); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' plugin - %w", PluginType, err)
		}
	}

	p, err := New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create '%s' plugin - %w", PluginType, err)
	}

	return p.WithName(name), nil
}

// New initializes a new Plugin from config, applying defaults.
func New(config Config) (*Plugin, error) {
	if len(config.Credentials) == 0 {
		return nil, fmt.Errorf("'%s' plugin requires at least one entry in 'credentials'", PluginType)
	}

	keyHeader := config.KeyHeader
	if keyHeader == "" {
		keyHeader = defaultKeyHeader
	}
	headerName := config.HeaderName
	if headerName == "" {
		headerName = defaultHeaderName
	}
	valuePrefix := defaultValuePrefix
	if config.ValuePrefixSet {
		valuePrefix = config.ValuePrefix
	}

	return &Plugin{
		typedName:   plugin.TypedName{Type: PluginType, Name: PluginType},
		keyHeader:   strings.ToLower(keyHeader),
		headerName:  headerName,
		valuePrefix: valuePrefix,
		credentials: config.Credentials,
	}, nil
}

// TypedName returns the type and name tuple of this plugin instance.
func (p *Plugin) TypedName() plugin.TypedName {
	return p.typedName
}

// WithName sets the name of this plugin instance.
func (p *Plugin) WithName(name string) *Plugin {
	p.typedName.Name = name
	return p
}

// ProcessRequest reads the chosen destination from the key header and injects its bearer credential.
// It is a no-op (not an error) when the header is absent or no credential is configured, so the request
// path stays fail-open; a missing token only means the upstream sees no Authorization header.
func (p *Plugin) ProcessRequest(ctx context.Context, _ *plugin.CycleState, request *requesthandling.InferenceRequest) error {
	logger := log.FromContext(ctx).V(logutil.DEFAULT)

	destination := p.readKeyHeader(request)
	if destination == "" {
		logger.V(logutil.VERBOSE).Info("destination header absent, skipping injection", "header", p.keyHeader)
		return nil
	}

	cred, ok := p.credentials[destination]
	if !ok {
		logger.Info("no credential configured for destination, skipping injection", "destination", destination)
		return nil
	}

	token := os.Getenv(cred.TokenEnv)
	if token == "" {
		logger.Info("credential env var is empty, skipping injection", "destination", destination, "env", cred.TokenEnv)
		return nil
	}

	request.SetHeader(p.headerName, p.valuePrefix+token)
	logger.Info("injected destination credential", "destination", destination, "header", p.headerName)
	return nil
}

// readKeyHeader returns the value of the key header, matched case-insensitively (ext_proc lowercases headers,
// but be defensive).
func (p *Plugin) readKeyHeader(request *requesthandling.InferenceRequest) string {
	if v, ok := request.Headers[p.keyHeader]; ok {
		return v
	}
	for k, v := range request.Headers {
		if strings.ToLower(k) == p.keyHeader {
			return v
		}
	}
	return ""
}
