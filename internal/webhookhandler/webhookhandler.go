// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package webhookhandler contains the webhook that injects sidecars into pods.
package webhookhandler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/open-telemetry/opentelemetry-operator/internal/config"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,sideEffects=none,admissionReviewVersions=v1;v1beta1
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=list;watch
// +kubebuilder:rbac:groups=opentelemetry.io,resources=opentelemetrycollectors,verbs=get;list;watch
// +kubebuilder:rbac:groups=opentelemetry.io,resources=instrumentations,verbs=get;list;watch
// +kubebuilder:rbac:groups="apps",resources=replicasets,verbs=get;list;watch

var _ WebhookHandler = (*podSidecarInjector)(nil)

// WebhookHandler is a webhook handler that analyzes new pods and injects appropriate sidecars into it.
type WebhookHandler interface {
	admission.Handler
	admission.DecoderInjector
}

// the implementation.
type podSidecarInjector struct {
	config      config.Config
	logger      logr.Logger
	client      client.Client
	decoder     *admission.Decoder
	podMutators []PodMutator
}

// PodMutator mutates a pod.
type PodMutator interface {
	Mutate(ctx context.Context, ns corev1.Namespace, pod corev1.Pod) (corev1.Pod, error)
}

// NewWebhookHandler creates a new WebhookHandler.
func NewWebhookHandler(cfg config.Config, logger logr.Logger, cl client.Client, podMutators []PodMutator) WebhookHandler {
	return &podSidecarInjector{
		config:      cfg,
		logger:      logger,
		client:      cl,
		podMutators: podMutators,
	}
}

func (p *podSidecarInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := corev1.Pod{}
	err := p.decoder.Decode(req, &pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// we use the req.Namespace here because the pod might have not been created yet
	ns := corev1.Namespace{}
	err = p.client.Get(ctx, types.NamespacedName{Name: req.Namespace, Namespace: ""}, &ns)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	for _, m := range p.podMutators {
		pod, err = m.Mutate(ctx, ns, pod)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (p *podSidecarInjector) InjectDecoder(d *admission.Decoder) error {
	p.decoder = d
	return nil
}
