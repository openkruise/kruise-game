package webhook

import (
	"context"
	"encoding/json"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	"github.com/openkruise/kruise-game/cloudprovider/alibabacloud"
	"github.com/openkruise/kruise-game/cloudprovider/manager"
	admissionv1 "k8s.io/api/admission/v1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestValidatingCreate(t *testing.T) {
	tests := []struct {
		gss     *gamekruiseiov1alpha1.GameServerSet
		cpm     *manager.ProviderManager
		allowed bool
	}{
		{
			gss: &gamekruiseiov1alpha1.GameServerSet{
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
							{
								Name:  "xx",
								Value: "xx",
							},
						},
					},
				},
			},
			cpm: &manager.ProviderManager{
				CloudProviders: map[string]cloudprovider.CloudProvider{},
			},
			allowed: false,
		},
		{
			gss: &gamekruiseiov1alpha1.GameServerSet{
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkType: "AlibabaCloud-LB",
					},
				},
			},
			cpm: &manager.ProviderManager{
				CloudProviders: map[string]cloudprovider.CloudProvider{
					"AlibabaCloud": func() cloudprovider.CloudProvider {
						acp, _ := alibabacloud.NewAlibabaCloudProvider()
						return acp
					}(),
				},
			},
			allowed: false,
		},
	}

	for i, test := range tests {
		actual := validatingCreate(test.gss, test.cpm)
		if actual.Allowed != test.allowed {
			t.Errorf("%d: expect %v, got %v", i, test.allowed, actual.Allowed)
		}
	}
}

func TestHandleGatedUpdatePartitionChanges(t *testing.T) {
	s := runtime.NewScheme()
	if err := gamekruiseiov1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		status           metav1.ConditionStatus
		oldPartition     int32
		oldReplicas      int32
		newReplicas      *int32
		targetPartition  int32
		missingCondition bool
		newTemplate      bool
		withoutPolicy    bool
		removePolicy     bool
		removeStrategy   bool
		paused           bool
		omitPartition    bool
		allowed          bool
	}{
		{name: "completed allows preparation", status: metav1.ConditionFalse, targetPartition: 1, allowed: true},
		{name: "completed accepts new template", status: metav1.ConditionFalse, targetPartition: 1, newTemplate: true, allowed: true},
		{name: "active rejects higher partition", status: metav1.ConditionTrue, targetPartition: 1},
		{name: "active accepts unchanged partition", status: metav1.ConditionTrue, oldPartition: 1, targetPartition: 1, allowed: true},
		{name: "active rejects lower partition", status: metav1.ConditionTrue, oldPartition: 1},
		{name: "active accepts explicit default zero", status: metav1.ConditionTrue, allowed: true},
		{name: "active accepts omitted default zero", status: metav1.ConditionTrue, omitPartition: true, allowed: true},
		{name: "active rejects removing nonzero partition", status: metav1.ConditionTrue, oldPartition: 1, omitPartition: true},
		{name: "unknown remains restricted", status: metav1.ConditionUnknown, targetPartition: 1},
		{name: "missing condition remains restricted", missingCondition: true, targetPartition: 1},
		{name: "active new template cannot change partition", status: metav1.ConditionTrue, targetPartition: 1, newTemplate: true},
		{name: "active accepts new template with unchanged partition", status: metav1.ConditionTrue, newTemplate: true, allowed: true},
		{name: "unconfigured allows higher partition with stale progress", status: metav1.ConditionTrue, withoutPolicy: true, targetPartition: 1, allowed: true},
		{name: "unconfigured allows lower partition", status: metav1.ConditionTrue, withoutPolicy: true, oldPartition: 1, allowed: true},
		{name: "active allows cancellation", status: metav1.ConditionTrue, removePolicy: true, allowed: true},
		{name: "paused allows cancellation", status: metav1.ConditionTrue, removePolicy: true, paused: true, allowed: true},
		{name: "unknown allows cancellation", status: metav1.ConditionUnknown, removePolicy: true, allowed: true},
		{name: "missing condition allows cancellation", missingCondition: true, removePolicy: true, allowed: true},
		{name: "cancellation allows ordinary partition", status: metav1.ConditionTrue, oldPartition: 1, removePolicy: true, allowed: true},
		{name: "cancellation allows removing strategy", status: metav1.ConditionTrue, oldPartition: 1, removeStrategy: true, allowed: true},
		{name: "scale down adjusts target", status: metav1.ConditionTrue, oldReplicas: 10, newReplicas: ptr.To[int32](3), oldPartition: 5, targetPartition: 3, allowed: true},
		{name: "scale to zero adjusts target", status: metav1.ConditionTrue, oldReplicas: 10, newReplicas: ptr.To[int32](0), oldPartition: 5, allowed: true},
		{name: "scale down keeps valid target", status: metav1.ConditionTrue, oldReplicas: 10, newReplicas: ptr.To[int32](3), oldPartition: 1, targetPartition: 1, allowed: true},
		{name: "scale down cannot lower target further", status: metav1.ConditionTrue, oldReplicas: 10, newReplicas: ptr.To[int32](3), oldPartition: 5, targetPartition: 1},
		{name: "unchanged replicas cannot adjust target", status: metav1.ConditionTrue, oldReplicas: 10, newReplicas: ptr.To[int32](10), oldPartition: 5, targetPartition: 3},
		{name: "scale up cannot adjust target", status: metav1.ConditionTrue, oldReplicas: 5, newReplicas: ptr.To[int32](10), oldPartition: 5, targetPartition: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := &gamekruiseiov1alpha1.GameServerSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", Generation: 3},
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](2),
					GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{PodTemplateSpec: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "v2"}}},
					}},
					ServiceQualities: []gamekruiseiov1alpha1.ServiceQuality{{Name: "ready", ContainerName: "main"}},
					UpdateStrategy: gamekruiseiov1alpha1.UpdateStrategy{RollingUpdate: &gamekruiseiov1alpha1.RollingUpdateStatefulSetStrategy{
						CompletionPolicy: &gamekruiseiov1alpha1.UpdateCompletionPolicy{
							ServiceQualities: []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{
								{
									Name:  "ready",
									State: true,
								},
							},
						},
					}},
				},
				Status: gamekruiseiov1alpha1.GameServerSetStatus{UpdateProgress: &gamekruiseiov1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 3, StartedAt: metav1.Now()}},
			}
			if !tc.missingCondition {
				old.Status.Conditions = []metav1.Condition{{Type: "UpdateProgressing", Status: tc.status}}
			}
			if tc.oldPartition != 0 {
				old.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(tc.oldPartition)
			}
			if tc.withoutPolicy {
				old.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			}
			old.Spec.UpdateStrategy.RollingUpdate.Paused = tc.paused
			if tc.oldReplicas != 0 {
				old.Spec.Replicas = ptr.To(tc.oldReplicas)
			}
			updated := old.DeepCopy()
			if tc.newReplicas != nil {
				updated.Spec.Replicas = tc.newReplicas
			}
			if !tc.omitPartition {
				updated.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(tc.targetPartition)
			} else {
				updated.Spec.UpdateStrategy.RollingUpdate.Partition = nil
			}
			if tc.newTemplate {
				updated.Spec.GameServerTemplate.Spec.Containers[0].Image = "v3"
			}
			if tc.removePolicy {
				updated.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			}
			if tc.removeStrategy {
				updated.Spec.UpdateStrategy.RollingUpdate = nil
			}
			// No ASTS exists: admission must use only the old and new GSS.
			h := &GssValidaatingHandler{Client: fake.NewClientBuilder().WithScheme(s).Build(), decoder: admission.NewDecoder(s)}
			oldRaw, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			newRaw, err := json.Marshal(updated)
			if err != nil {
				t.Fatal(err)
			}
			response := h.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Update, Object: runtime.RawExtension{Raw: newRaw}, OldObject: runtime.RawExtension{Raw: oldRaw},
			}})
			if response.Allowed != tc.allowed {
				t.Fatalf("allowed=%v, want %v; result=%v", response.Allowed, tc.allowed, response.Result)
			}
			if !tc.allowed && (response.Result == nil || response.Result.Message != "rollingUpdate.partition cannot change during an active update") {
				t.Fatalf("expected partition validation rejection, got %v", response.Result)
			}
		})
	}
}

func TestHandleCompletionPolicyPreservesReserveIDsAdmission(t *testing.T) {
	s := runtime.NewScheme()
	if err := gamekruiseiov1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, tc := range []struct {
			name          string
			ids           []intstr.IntOrString
			withoutPolicy bool
			invalidPolicy bool
		}{
			{name: "negative id", ids: []intstr.IntOrString{intstr.FromInt(-1)}},
			{name: "duplicate id", ids: []intstr.IntOrString{intstr.FromInt(1), intstr.FromInt(1)}},
			{name: "invalid range", ids: []intstr.IntOrString{intstr.FromString("3-1")}},
			{name: "overlapping ranges", ids: []intstr.IntOrString{intstr.FromString("1-3"), intstr.FromString("2-4")}},
			{name: "without policy", ids: []intstr.IntOrString{intstr.FromInt(-1)}, withoutPolicy: true},
			{name: "invalid policy still rejected", ids: []intstr.IntOrString{intstr.FromInt(-1)}, invalidPolicy: true},
		} {
			t.Run(string(operation)+"/"+tc.name, func(t *testing.T) {
				gss := &gamekruiseiov1alpha1.GameServerSet{Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Replicas: ptr.To[int32](2), ReserveGameServerIds: tc.ids,
					GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{PodTemplateSpec: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "v2"}}}}},
					ServiceQualities:   []gamekruiseiov1alpha1.ServiceQuality{{Name: "ready", ContainerName: "main"}},
					UpdateStrategy: gamekruiseiov1alpha1.UpdateStrategy{RollingUpdate: &gamekruiseiov1alpha1.RollingUpdateStatefulSetStrategy{
						Paused:           true,
						CompletionPolicy: &gamekruiseiov1alpha1.UpdateCompletionPolicy{ServiceQualities: []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{{Name: "ready", State: true}}},
					}},
				}}
				if tc.withoutPolicy {
					gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
				}
				if tc.invalidPolicy {
					gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].Name = "missing"
				}
				raw, err := json.Marshal(gss)
				if err != nil {
					t.Fatal(err)
				}
				h := &GssValidaatingHandler{decoder: admission.NewDecoder(s)}
				response := h.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: operation, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw},
				}})
				if response.Allowed == tc.invalidPolicy {
					t.Fatalf("unexpected admission: allowed=%v result=%v", response.Allowed, response.Result)
				}
			})
		}
	}
}

func TestValidateCompletionPolicy(t *testing.T) {

	newGSS := func() *gamekruiseiov1alpha1.GameServerSet {
		replicas := int32(5)
		return &gamekruiseiov1alpha1.GameServerSet{Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Replicas: ptr.To(replicas),
			GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{PodTemplateSpec: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
			}},
			ServiceQualities: []gamekruiseiov1alpha1.ServiceQuality{{Name: "business-ready", ContainerName: "main"}},
			UpdateStrategy: gamekruiseiov1alpha1.UpdateStrategy{
				Type: apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &gamekruiseiov1alpha1.RollingUpdateStatefulSetStrategy{
					CompletionPolicy: &gamekruiseiov1alpha1.UpdateCompletionPolicy{
						ServiceQualities: []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{{Name: "business-ready", State: true}},
					},
				},
			},
		}}
	}

	tests := []struct {
		name   string
		change func(*gamekruiseiov1alpha1.GameServerSet)
		valid  bool
	}{
		{name: "valid", valid: true},
		{name: "short custom observation window", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](1)
		}},
		{name: "zero observation window", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](0)
		}},
		{name: "negative observation window", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](-1)
		}},
		{name: "empty conditions", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities = nil
		}},
		{name: "missing reference", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].Name = "missing"
		}},
		{name: "duplicate conditions", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			conditions := g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities = append(conditions, conditions[0])
		}},
		{name: "ambiguous probe", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities = append(g.Spec.ServiceQualities, g.Spec.ServiceQualities[0])
		}},
		{name: "unknown probe container", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].ContainerName = "missing"
		}},
		{name: "partition exceeds replicas", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			partition := int32(6)
			g.Spec.UpdateStrategy.RollingUpdate.Partition = &partition
		}},
		{name: "wrong strategy", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.Type = apps.OnDeleteStatefulSetStrategyType
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gss := newGSS()
			if tt.change != nil {
				tt.change(gss)
			}
			valid, _ := validateCompletionPolicy(gss)
			if valid != tt.valid {
				t.Fatalf("valid = %v, want %v", valid, tt.valid)
			}
		})
	}

}

func TestValidatingUpdateActiveCompletionPolicy(t *testing.T) {
	old := &gamekruiseiov1alpha1.GameServerSet{Spec: gamekruiseiov1alpha1.GameServerSetSpec{
		UpdateStrategy: gamekruiseiov1alpha1.UpdateStrategy{
			RollingUpdate: &gamekruiseiov1alpha1.RollingUpdateStatefulSetStrategy{
				CompletionPolicy: &gamekruiseiov1alpha1.UpdateCompletionPolicy{
					ServiceQualities: []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{{Name: "ready", State: true}},
				},
			},
		},
		ServiceQualities: []gamekruiseiov1alpha1.ServiceQuality{{Name: "ready"}},
	}, Status: gamekruiseiov1alpha1.GameServerSetStatus{
		UpdateProgress: &gamekruiseiov1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 2},
		Conditions:     []metav1.Condition{{Type: "UpdateProgressing", Status: metav1.ConditionTrue}},
	}}

	tests := []struct {
		name          string
		change        func(*gamekruiseiov1alpha1.GameServerSet)
		valid         bool
		completed     bool
		withoutPolicy bool
	}{
		{name: "ordinary change", valid: true},
		{name: "change observation window", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](60)
		}},
		{name: "new template and observation window", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.GameServerTemplate.Annotations = map[string]string{"release": "next"}
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](60)
		}},
		{name: "completed changes observation window", completed: true, valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](60)
		}},
		{name: "remove policy", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
		}},
		{name: "change condition", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].State = false
		}},
		{name: "change referenced probe", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].ContainerName = "main"
		}},
		{name: "new template and condition", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.GameServerTemplate.Annotations = map[string]string{"release": "next"}
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].State = false
		}},
		{name: "new template and referenced probe", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.GameServerTemplate.Annotations = map[string]string{"release": "next"}
			g.Spec.ServiceQualities[0].ContainerName = "main"
		}},
		{name: "referenced permanent field", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].Permanent = true
		}},
		{name: "referenced action", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].ServiceQualityAction = []gamekruiseiov1alpha1.ServiceQualityAction{{State: true}}
		}},
		{name: "referenced probe period", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].PeriodSeconds = 10
		}},
		{name: "referenced probe command", change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities[0].Exec = &corev1.ExecAction{Command: []string{"/bin/ready"}}
		}},
		{name: "pause", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.Paused = true
		}},
		{name: "template only", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.GameServerTemplate.Annotations = map[string]string{"release": "next"}
		}},
		{name: "unreferenced quality", valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.ServiceQualities = append(g.Spec.ServiceQualities, gamekruiseiov1alpha1.ServiceQuality{Name: "other"})
		}},
		{name: "completed changes condition and probe", completed: true, valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].State = false
			g.Spec.ServiceQualities[0].ContainerName = "main"
		}},
		{name: "unconfigured changes quality and template", withoutPolicy: true, valid: true, change: func(g *gamekruiseiov1alpha1.GameServerSet) {
			g.Spec.GameServerTemplate.Annotations = map[string]string{"release": "next"}
			g.Spec.ServiceQualities[0].ContainerName = "main"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := old.DeepCopy()
			if tt.completed {
				previous.Status.Conditions[0].Status = metav1.ConditionFalse
			}
			if tt.withoutPolicy {
				previous.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			}
			gss := previous.DeepCopy()
			if tt.change != nil {
				tt.change(gss)
			}
			response := validatingUpdate(gss, previous)
			if response.Allowed != tt.valid {
				t.Fatalf("allowed = %v, want %v", response.Allowed, tt.valid)
			}
		})
	}
}

func TestCompletionConditionsEqual(t *testing.T) {
	a := []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{
		{Name: "a", State: true}, {Name: "b", State: false, Result: "offline"},
	}
	b := []gamekruiseiov1alpha1.ServiceQualityCompletionCondition{
		{Name: "b", State: false, Result: "offline"}, {Name: "a", State: true},
	}
	if !completionConditionsEqual(a, b) {
		t.Fatal("condition list order must not change the policy")
	}
	b[0].Result = "other"
	if completionConditionsEqual(a, b) {
		t.Fatal("different results must change the policy")
	}
}
