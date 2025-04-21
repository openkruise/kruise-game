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
	"net/http"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	"github.com/openkruise/kruise-game/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type GssValidaatingHandler struct {
	Client               client.Client
	decoder              admission.Decoder
	CloudProviderManager *manager.ProviderManager
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
	case admissionv1.Create:
		newGss := gss.DeepCopy()
		return validatingCreate(newGss, gvh.CloudProviderManager)
	}

	return admission.ValidationResponse(true, "pass validating")
}

func validatingGss(gss *gamekruiseiov1alpha1.GameServerSet, client client.Client) (bool, string) {
	// validate reserveGameServerIds
	vset := sets.Set[int]{}

	validate := func(ids intstr.IntOrString) (bool, string) {
		switch ids.Type {
		case intstr.Int:
			id := ids.IntVal
			if id < 0 {
				return false, fmt.Sprintf("reserveGameServerIds should be greater or equal to 0. Now it is %d", id)
			}
			if vset.Has(int(id)) {
				return false, fmt.Sprintf("reserveGameServerIds should not be repeat. Now it is %d", id)
			}
			vset.Insert(int(id))
		case intstr.String:
			start, end, err := util.ParseRange(ids.StrVal)
			if err != nil {
				return false, fmt.Sprintf("invalid range reserveGameServerIds found, an empty slice will be returned: %s", ids.StrVal)
			}
			if start < 0 {
				return false, fmt.Sprintf("reserveGameServerIds should be greater or equal to 0. Now it is %d", start)
			}
			if end < 0 {
				return false, fmt.Sprintf("reserveGameServerIds should be greater or equal to 0. Now it is %d", end)
			}
			if start > end {
				return false, fmt.Sprintf("invalid range reserveGameServerIds found, an empty slice will be returned: %s", ids.StrVal)
			}
			if vset.Has(int(start)) || vset.Has(int(end)) {
				return false, fmt.Sprintf("reserveGameServerIds should not be repeat. Now it is %d-%d", start, end)
			}
			for i := start; i <= end; i++ {
				if vset.Has(int(i)) {
					return false, fmt.Sprintf("reserveGameServerIds should not be repeat. Now it is %d-%d", start, end)
				}
				vset.Insert(int(i))
			}
		}
		return true, ""
	}

	for _, id := range gss.Spec.ReserveGameServerIds {
		if ok, reason := validate(id); !ok {
			return false, reason
		}
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

func validatingCreate(gss *gamekruiseiov1alpha1.GameServerSet, cpm *manager.ProviderManager) admission.Response {
	if gss.Spec.Network != nil {
		if gss.Spec.Network.NetworkType == "" {
			return admission.ValidationResponse(false, "network type is required")
		}
		if pn := listPluginNames(cpm); !util.IsStringInList(gss.Spec.Network.NetworkType, pn) {
			return admission.ValidationResponse(false, fmt.Sprintf("network type must be one of %v", pn))
		}
	}
	return admission.ValidationResponse(true, "validatingCreate success")
}

func listPluginNames(cpm *manager.ProviderManager) []string {
	var pluginNames []string
	for _, cp := range cpm.CloudProviders {
		plugins, _ := cp.ListPlugins()
		for _, p := range plugins {
			pluginNames = append(pluginNames, p.Name())
		}
	}
	return pluginNames
}
