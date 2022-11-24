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
	"github.com/openkruise/kruise-game/pkg/webhook/cloudprovider"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type PodMutatingHandler struct {
	Client               client.Client
	decoder              *admission.Decoder
	CloudProviderManager *cloudprovider.ProviderManager
}

func (pmh *PodMutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Delete {
		return pmh.handleDelete(ctx, req)
	}
	return pmh.handleNormal(ctx, req)
}

func (pmh *PodMutatingHandler) handleDelete(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := pmh.decoder.DecodeRaw(req.OldObject, pod)
	if err != nil {
		return admission.Allowed("pod has no content to decode")
	}
	_, err = mutatingPod(pmh.CloudProviderManager, pod, req, pmh.Client)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Allowed("no error found")
}

func (pmh *PodMutatingHandler) handleNormal(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := pmh.decoder.Decode(req, pod)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	pod, err = mutatingPod(pmh.CloudProviderManager, pod, req, pmh.Client)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func mutatingPod(cpm *cloudprovider.ProviderManager, pod *corev1.Pod, req admission.Request, client client.Client) (*corev1.Pod, error) {
	action := req.Operation

	plugin, ok := cpm.FindAvailablePlugins(pod)
	if !ok {
		klog.Warningf("Pod %s has no available plugin", pod.Name)
		return pod, nil
	}

	switch action {
	case admissionv1.Create:
		p, err := plugin.OnPodAdded(client, pod)
		if err != nil {
			klog.Warningf("Failed to handle pod %s added,because of %s", pod.Name, err.Error())
		} else {
			return p, nil
		}
	case admissionv1.Update:
		p, err := plugin.OnPodUpdated(client, pod)
		if err != nil {
			klog.Warningf("Failed to handle pod %s updated,because of %s", pod.Name, err.Error())
		} else {
			return p, nil
		}
	case admissionv1.Delete:
		err := plugin.OnPodDeleted(client, pod)

		if err != nil {
			klog.Warningf("Failed to handle pod %s deleted,because of %s", pod.Name, err.Error())
		}
	}

	return pod, nil
}

func NewPodMutatingHandler(client client.Client, decoder *admission.Decoder, cpm *cloudprovider.ProviderManager) *PodMutatingHandler {
	return &PodMutatingHandler{
		Client:               client,
		decoder:              decoder,
		CloudProviderManager: cpm,
	}
}
