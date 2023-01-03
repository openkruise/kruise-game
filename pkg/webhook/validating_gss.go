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
	"fmt"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type GssValidaatingHandler struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (gvh *GssValidaatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := gvh.decoder.Decode(req, gss)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if allowed, reason := validatingGss(gss, gvh.Client); !allowed {
		admission.ValidationResponse(allowed, reason)
	}

	switch req.Operation {
	case admissionv1.Update:
		newGss := gss.DeepCopy()
		oldGss := &gamekruiseiov1alpha1.GameServerSet{}
		err := gvh.decoder.DecodeRaw(req.OldObject, oldGss)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return validatingUpdate(newGss, oldGss)
	}

	return admission.ValidationResponse(true, "pass validating")
}

func validatingGss(gss *gamekruiseiov1alpha1.GameServerSet, client client.Client) (bool, string) {
	// validate reserveGameServerIds
	rgsIds := gss.Spec.ReserveGameServerIds
	if util.IsRepeat(rgsIds) {
		return false, fmt.Sprintf("reserveGameServerIds should not be repeat. Now it is %v", rgsIds)
	}
	if util.IsHasNegativeNum(rgsIds) {
		return false, fmt.Sprintf("reserveGameServerIds should be greater or equal to 0. Now it is %v", rgsIds)
	}

	return true, "general validating success"
}

func validatingUpdate(newGss, oldGss *gamekruiseiov1alpha1.GameServerSet) admission.Response {
	if oldGss.Spec.Network != nil && newGss.Spec.Network != nil {
		if oldGss.Spec.Network.NetworkType != "" && newGss.Spec.Network.NetworkType != oldGss.Spec.Network.NetworkType {
			return admission.ValidationResponse(false, "change network type is not allowed")
		}
	}
	return admission.ValidationResponse(true, "validatingUpdate success")
}
