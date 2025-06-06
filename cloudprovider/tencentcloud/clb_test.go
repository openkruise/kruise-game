package tencentcloud

import (
	"context"
	"reflect"
	"sync"
	"testing"

	kruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/tencentcloud/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAllocateDeAllocate(t *testing.T) {
	test := struct {
		lbIds  []string
		clb    *ClbPlugin
		podKey string
	}{
		lbIds: []string{"lb-xxx"},
		clb: &ClbPlugin{
			maxPort:     int32(712),
			minPort:     int32(512),
			cache:       make(map[string]portAllocated),
			podAllocate: make(map[string][]string),
			mutex:       sync.RWMutex{},
		},
		podKey: "xxx/xxx",
	}

	scheme := runtime.NewScheme()
	_ = kruisev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "xxx", Namespace: "xxx"}}
	lbId, port := test.clb.allocate(context.TODO(), fakeClient, test.lbIds, test.podKey, pod)
	if _, exist := test.clb.podAllocate[test.podKey]; !exist {
		t.Errorf("podAllocate[%s] is empty after allocated", test.podKey)
	}
	if port > test.clb.maxPort || port < test.clb.minPort {
		t.Errorf("allocate port %d, unexpected", port)
	}
	if test.clb.cache[lbId][port] == false {
		t.Errorf("Allocate port %d failed", port)
	}
	test.clb.deAllocate(context.TODO(), fakeClient, test.podKey, pod.Namespace)
	if test.clb.cache[lbId][port] == true {
		t.Errorf("deAllocate port %d failed", port)
	}
	if _, exist := test.clb.podAllocate[test.podKey]; exist {
		t.Errorf("podAllocate[%s] is not empty after deallocated", test.podKey)
	}
}

func TestParseLbConfig(t *testing.T) {
	tests := []struct {
		conf      []kruisev1alpha1.NetworkConfParams
		clbConfig *clbConfig
	}{
		{
			conf: []kruisev1alpha1.NetworkConfParams{
				{
					Name:  ClbIdsConfigName,
					Value: "xxx-A",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "80",
				},
			},
			clbConfig: &clbConfig{
				lbIds: []string{"xxx-A"},
				targetPorts: []portProtocol{
					{
						port:     80,
						protocol: "TCP",
					},
				},
			},
		},
		{
			conf: []kruisev1alpha1.NetworkConfParams{
				{
					Name:  ClbIdsConfigName,
					Value: "xxx-A,xxx-B,",
				},
				{
					Name:  PortProtocolsConfigName,
					Value: "81/UDP,82,83/TCP",
				},
			},
			clbConfig: &clbConfig{
				lbIds: []string{"xxx-A", "xxx-B"},
				targetPorts: []portProtocol{
					{
						port:     81,
						protocol: "UDP",
					},
					{
						port:     82,
						protocol: "TCP",
					},
					{
						port:     83,
						protocol: "TCP",
					},
				},
			},
		},
	}

	for i, test := range tests {
		lc, err := parseLbConfig(test.conf)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(test.clbConfig, lc) {
			t.Errorf("case %d: lbId expect: %v, actual: %v", i, test.clbConfig, lc)
		}
	}
}

func TestInitLbCache(t *testing.T) {
	test := struct {
		listenerList []v1alpha1.DedicatedCLBListener
		minPort      int32
		maxPort      int32
		cache        map[string]portAllocated
		podAllocate  map[string][]string
	}{
		minPort: 512,
		maxPort: 712,
		cache: map[string]portAllocated{
			"xxx-A": map[int32]bool{
				666: true,
			},
			"xxx-B": map[int32]bool{
				555: true,
			},
		},
		podAllocate: map[string][]string{
			"ns-0/name-0": {"xxx-A:666"},
			"ns-1/name-1": {"xxx-B:555"},
		},
		listenerList: []v1alpha1.DedicatedCLBListener{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						OwnerPodKey: "name-0",
					},
					Namespace: "ns-0",
					Name:      "name-0-xxx",
				},
				Spec: v1alpha1.DedicatedCLBListenerSpec{
					LbId:     "xxx-A",
					LbPort:   666,
					Protocol: "TCP",
					TargetPod: &v1alpha1.TargetPod{
						PodName:    "name-0",
						TargetPort: 80,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						OwnerPodKey: "name-1",
					},
					Namespace: "ns-1",
					Name:      "name-1-xxx",
				},
				Spec: v1alpha1.DedicatedCLBListenerSpec{
					LbId:     "xxx-B",
					LbPort:   555,
					Protocol: "TCP",
					TargetPod: &v1alpha1.TargetPod{
						PodName:    "name-1",
						TargetPort: 80,
					},
				},
			},
		},
	}

	actualCache, actualPodAllocate := initLbCache(test.listenerList, test.minPort, test.maxPort)
	for lb, pa := range test.cache {
		for port, isAllocated := range pa {
			if actualCache[lb][port] != isAllocated {
				t.Errorf("lb %s port %d isAllocated, expect: %t, actual: %t", lb, port, isAllocated, actualCache[lb][port])
			}
		}
	}
	if !reflect.DeepEqual(actualPodAllocate, test.podAllocate) {
		t.Errorf("podAllocate expect %v, but actully got %v", test.podAllocate, actualPodAllocate)
	}
}
