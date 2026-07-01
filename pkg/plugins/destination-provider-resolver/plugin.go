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

// Package destinationproviderresolver resolves a credential from the scheduler-chosen destination
// endpoint (x-gateway-destination-endpoint), not the request model, for hub-and-spoke routing. It
// matches the endpoint to an ExternalProvider and writes its credential ref to CycleState for
// apikey-injection.
package destinationproviderresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	logutil "github.com/llm-d/llm-d-inference-payload-processor/pkg/common/observability/logging"
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/plugin"
	"github.com/llm-d/llm-d-inference-payload-processor/pkg/framework/interface/requesthandling"

	inferencev1alpha1 "github.com/opendatahub-io/ai-gateway-payload-processing/api/inference/v1alpha1"
	"github.com/opendatahub-io/ai-gateway-payload-processing/pkg/plugins/common/auth"
	"github.com/opendatahub-io/ai-gateway-payload-processing/pkg/plugins/common/state"
)

const (
	// PluginType is the registered name for this plugin.
	PluginType = "destination-provider-resolver"

	defaultKeyHeader = "x-gateway-destination-endpoint"
)

// compile-time type validation
var _ requesthandling.RequestProcessor = &Plugin{}

// providerCred is the credential reference + upstream rewrite info resolved from an ExternalProvider.
type providerCred struct {
	authType        auth.Auth
	secretName      string
	secretNamespace string
	host            string            // rewrites :authority for SNI-routed external backends
	pathPrefix      string            // rewrites the :path prefix
	config          map[string]string // provider Config, passed to apikey-injection via ModelConfigKey
}

// endpointStore is a thread-safe index of ExternalProviders by endpoint host.
type endpointStore struct {
	lock         sync.RWMutex
	byEndpoint   map[string]*providerCred
	nameToEndpnt map[string]string
}

func newEndpointStore() *endpointStore {
	return &endpointStore{
		byEndpoint:   make(map[string]*providerCred),
		nameToEndpnt: make(map[string]string),
	}
}

func (s *endpointStore) set(key types.NamespacedName, endpoint string, cred *providerCred) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if prev, ok := s.nameToEndpnt[key.String()]; ok && prev != endpoint {
		delete(s.byEndpoint, prev) // endpoint changed on the CR; drop the stale index entry
	}
	s.nameToEndpnt[key.String()] = endpoint
	s.byEndpoint[endpoint] = cred
}

func (s *endpointStore) delete(key types.NamespacedName) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if endpoint, ok := s.nameToEndpnt[key.String()]; ok {
		delete(s.byEndpoint, endpoint)
		delete(s.nameToEndpnt, key.String())
	}
}

func (s *endpointStore) get(endpoint string) (*providerCred, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	cred, ok := s.byEndpoint[endpoint]
	return cred, ok
}

// reconciler keeps the endpoint store in sync with ExternalProvider CRs.
type reconciler struct {
	client.Reader
	store *endpointStore
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).V(logutil.DEFAULT)

	ep := &inferencev1alpha1.ExternalProvider{}
	err := r.Get(ctx, req.NamespacedName, ep)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("unable to get ExternalProvider: %w", err)
	}
	if errors.IsNotFound(err) || !ep.GetDeletionTimestamp().IsZero() {
		r.store.delete(req.NamespacedName)
		return ctrl.Result{}, nil
	}

	r.store.set(req.NamespacedName, ep.Spec.Endpoint, &providerCred{
		authType:        auth.Auth(ep.Spec.Auth.Type),
		secretName:      ep.Spec.Auth.SecretRef.Name,
		secretNamespace: req.Namespace,
		host:            ep.Spec.Config["host"],
		pathPrefix:      ep.Spec.Config["pathPrefix"],
		config:          ep.Spec.Config,
	})
	logger.Info("indexed ExternalProvider by endpoint", "endpoint", ep.Spec.Endpoint, "provider", ep.Spec.Provider)
	return ctrl.Result{}, nil
}

// Plugin resolves the credential reference for the scheduler-chosen destination endpoint.
type Plugin struct {
	typedName plugin.TypedName
	keyHeader string
	store     *endpointStore
}

// Config defines the JSON configuration structure for the plugin.
type Config struct {
	// KeyHeader is the request header carrying the chosen destination. Defaults to x-gateway-destination-endpoint.
	KeyHeader string `json:"keyHeader"`
}

// Factory defines the factory function for the destination-provider-resolver plugin.
func Factory(name string, rawParameters json.RawMessage, handle plugin.Handle) (plugin.Plugin, error) {
	var config Config
	if len(rawParameters) > 0 {
		if err := json.Unmarshal(rawParameters, &config); err != nil {
			return nil, fmt.Errorf("failed to parse the parameters of the '%s' plugin - %w", PluginType, err)
		}
	}

	store := newEndpointStore()
	rec := &reconciler{Reader: handle.Client(), store: store}
	if err := handle.ReconcilerBuilder().For(&inferencev1alpha1.ExternalProvider{}).Complete(rec); err != nil {
		return nil, fmt.Errorf("failed to register ExternalProvider reconciler for plugin '%s' - %w", PluginType, err)
	}

	keyHeader := config.KeyHeader
	if keyHeader == "" {
		keyHeader = defaultKeyHeader
	}

	return (&Plugin{
		typedName: plugin.TypedName{Type: PluginType, Name: PluginType},
		keyHeader: strings.ToLower(keyHeader),
		store:     store,
	}).WithName(name), nil
}

// TypedName returns the type and name tuple of this plugin instance.
func (p *Plugin) TypedName() plugin.TypedName { return p.typedName }

// WithName sets the name of this plugin instance.
func (p *Plugin) WithName(name string) *Plugin {
	p.typedName.Name = name
	return p
}

// ProcessRequest resolves the destination endpoint to an ExternalProvider and writes its credential ref to
// CycleState for apikey-injection. No-op (fail-open) if the header is absent or no provider matches.
func (p *Plugin) ProcessRequest(ctx context.Context, cycleState *plugin.CycleState, request *requesthandling.InferenceRequest) error {
	logger := log.FromContext(ctx).V(logutil.DEFAULT)

	destination := p.readKeyHeader(request)
	if destination == "" {
		logger.V(logutil.VERBOSE).Info("destination header absent, skipping resolution", "header", p.keyHeader)
		return nil
	}
	host := destination
	if h, _, found := strings.Cut(destination, ":"); found {
		host = h // drop the :port; ExternalProvider.Endpoint is host-only
	}

	cred, ok := p.store.get(host)
	if !ok {
		logger.Info("no ExternalProvider matches destination, skipping resolution", "destination", host)
		return nil
	}

	cycleState.Write(state.AuthTypeKey, cred.authType)
	cycleState.Write(state.CredsRefName, cred.secretName)
	cycleState.Write(state.CredsRefNamespace, cred.secretNamespace)
	modelConfig := cred.config
	if modelConfig == nil {
		modelConfig = map[string]string{}
	}
	cycleState.Write(state.ModelConfigKey, modelConfig) // apikey-injection's auth generator requires this key

	// Rewrite :authority (upstream SNI/routing) and :path prefix, when configured on the ExternalProvider.
	if cred.host != "" {
		request.SetHeader(":authority", cred.host)
	}
	if cred.pathPrefix != "" {
		if p, ok := request.Headers[":path"]; ok {
			request.SetHeader(":path", strings.TrimRight(cred.pathPrefix, "/")+p)
		}
	}
	logger.Info("resolved destination to provider credential", "destination", host, "authType", cred.authType, "rewriteHost", cred.host)
	return nil
}

// readKeyHeader returns the value of the key header, matched case-insensitively.
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
