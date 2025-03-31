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
	"net/http"
	"time"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/errors"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
	decoder              admission.Decoder
	CloudProviderManager *manager.ProviderManager
	eventRecorder        record.EventRecorder
}

func (pmh *PodMutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// decode request & get pod
	pod, err := getPodFromRequest(req, pmh.decoder)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if req.Operation == admissionv1.Create {
		pod, err = patchContainers(pmh.Client, pod, ctx)
		if err != nil {
			msg := fmt.Sprintf("Pod %s/%s patchContainers failed, because of %s", pod.Namespace, pod.Name, err.Error())
			return admission.Denied(msg)
		}
	}

	// get the plugin according to pod
	plugin, ok := pmh.CloudProviderManager.FindAvailablePlugins(pod)
	if !ok {
		msg := fmt.Sprintf("Pod %s/%s has no available plugin", pod.Namespace, pod.Name)
		klog.Infof(msg)
		return getAdmissionResponse(req, patchResult{pod: pod, err: nil})
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

func getPodFromRequest(req admission.Request, decoder admission.Decoder) (*corev1.Pod, error) {
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
		return admission.Denied(result.err.Error())
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

func NewPodMutatingHandler(client client.Client, decoder admission.Decoder, cpm *manager.ProviderManager, recorder record.EventRecorder) *PodMutatingHandler {
	return &PodMutatingHandler{
		Client:               client,
		decoder:              decoder,
		CloudProviderManager: cpm,
		eventRecorder:        recorder,
	}
}

func patchContainers(client client.Client, pod *corev1.Pod, ctx context.Context) (*corev1.Pod, error) {
	if _, ok := pod.GetLabels()[gameKruiseV1alpha1.GameServerOwnerGssKey]; !ok {
		return pod, nil
	}
	gs := &gameKruiseV1alpha1.GameServer{}
	err := client.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}, gs)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return pod, nil
		}
		return pod, err
	}
	if gs.Spec.Containers != nil {
		var containers []corev1.Container
		for _, podContainer := range pod.Spec.Containers {
			container := podContainer
			for _, gsContainer := range gs.Spec.Containers {
				if gsContainer.Name == podContainer.Name {
					// patch Image
					if gsContainer.Image != podContainer.Image && gsContainer.Image != "" {
						container.Image = gsContainer.Image
					}

					// patch Resources
					if limitCPU, ok := gsContainer.Resources.Limits[corev1.ResourceCPU]; ok {
						if container.Resources.Limits == nil {
							container.Resources.Limits = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Limits[corev1.ResourceCPU] = limitCPU
					}
					if limitMemory, ok := gsContainer.Resources.Limits[corev1.ResourceMemory]; ok {
						if container.Resources.Limits == nil {
							container.Resources.Limits = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Limits[corev1.ResourceMemory] = limitMemory
					}
					if requestCPU, ok := gsContainer.Resources.Requests[corev1.ResourceCPU]; ok {
						if container.Resources.Requests == nil {
							container.Resources.Requests = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Requests[corev1.ResourceCPU] = requestCPU
					}
					if requestMemory, ok := gsContainer.Resources.Requests[corev1.ResourceMemory]; ok {
						if container.Resources.Requests == nil {
							container.Resources.Requests = make(map[corev1.ResourceName]resource.Quantity)
						}
						container.Resources.Requests[corev1.ResourceMemory] = requestMemory
					}
				}
			}
			containers = append(containers, container)
		}
		pod.Spec.Containers = containers
	}
	return pod, nil
}
