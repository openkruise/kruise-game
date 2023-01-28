/*
Copyright 2022 The Kruise Authors.

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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"time"
)

const (
	podMutatingTimeout    = 8 * time.Second
	mutatingTimeoutReason = "MutatingTimeout"
)

type patchResult struct {
	pod *corev1.Pod
	err errors.PluginError
}

type PodMutatingHandler struct {
	Client               client.Client
	decoder              *admission.Decoder
	CloudProviderManager *manager.ProviderManager
	eventRecorder        record.EventRecorder
}

func (pmh *PodMutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// decode request & get pod
	pod, err := getPodFromRequest(req, pmh.decoder)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// get the plugin according to pod
	plugin, ok := pmh.CloudProviderManager.FindAvailablePlugins(pod)
	if !ok {
		msg := fmt.Sprintf("Pod %s/%s has no available plugin", pod.Namespace, pod.Name)
		return admission.Allowed(msg)
	}

	// define context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), podMutatingTimeout)
	defer cancel()

	// cloud provider plugin patches pod
	resultCh := make(chan patchResult, 1)
	go func() {
		var newPod *corev1.Pod
		var pluginError errors.PluginError
		switch req.Operation {
		case admissionv1.Create:
			newPod, pluginError = plugin.OnPodAdded(pmh.Client, pod, ctx)
		case admissionv1.Update:
			newPod, pluginError = plugin.OnPodUpdated(pmh.Client, pod, ctx)
		case admissionv1.Delete:
			pluginError = plugin.OnPodDeleted(pmh.Client, pod, ctx)
		}
		if pluginError != nil {
			msg := fmt.Sprintf("Failed to %s pod %s/%s ,because of %s", req.Operation, pod.Namespace, pod.Name, pluginError.Error())
			klog.Warningf(msg)
			pmh.eventRecorder.Eventf(pod, corev1.EventTypeWarning, string(pluginError.Type()), msg)
			newPod = pod.DeepCopy()
		}
		resultCh <- patchResult{
			pod: newPod,
			err: pluginError,
		}
	}()

	select {
	// timeout
	case <-ctx.Done():
		msg := fmt.Sprintf("Failed to %s pod %s/%s, because plugin %s exec timed out", req.Operation, pod.Namespace, pod.Name, plugin.Name())
		pmh.eventRecorder.Eventf(pod, corev1.EventTypeWarning, mutatingTimeoutReason, msg)
		return admission.Allowed(msg)
	// completed before timeout
	case result := <-resultCh:
		return getAdmissionResponse(req, result)
	}
}

func getPodFromRequest(req admission.Request, decoder *admission.Decoder) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if req.Operation == admissionv1.Delete {
		err := decoder.DecodeRaw(req.OldObject, pod)
		return pod, err
	}
	err := decoder.Decode(req, pod)
	return pod, err
}

func getAdmissionResponse(req admission.Request, result patchResult) admission.Response {
	if result.err != nil {
		return admission.Allowed(result.err.Error())
	}
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("delete successfully")
	}
	marshaledPod, err := json.Marshal(result.pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func NewPodMutatingHandler(client client.Client, decoder *admission.Decoder, cpm *manager.ProviderManager, recorder record.EventRecorder) *PodMutatingHandler {
	return &PodMutatingHandler{
		Client:               client,
		decoder:              decoder,
		CloudProviderManager: cpm,
		eventRecorder:        recorder,
	}
}
