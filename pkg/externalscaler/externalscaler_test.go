package externalscaler

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"testing"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = gamekruiseiov1alpha1.AddToScheme(s)
	return s
}

func makeGSS(name, ns string, replicas int32) *gamekruiseiov1alpha1.GameServerSet {
	return &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: gamekruiseiov1alpha1.GameServerSetSpec{
			Replicas: ptr.To(replicas),
		},
	}
}

func makePod(name, ns, gssName, opsState, state string) *corev1.Pod {
	labels := map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
		gamekruiseiov1alpha1.GameServerOpsStateKey: opsState,
	}
	if state != "" {
		labels[gamekruiseiov1alpha1.GameServerStateKey] = state
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
	}
}

func newFakeClient(s *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		Build()
}

func TestGetMetrics(t *testing.T) {
	const gssName = "test-gss"
	const ns = "default"

	tests := []struct {
		name         string
		replicas     int32
		pods         []*corev1.Pod
		metadata     map[string]string
		wantReplicas int64
		wantErr      bool
	}{
		// --- scaleDownThreshold: absolute mode, exceeded ---
		{
			name:     "threshold exceeded (absolute): scale-down prioritized",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 2, string(gamekruiseiov1alpha1.None), ""),
				append(
					makePodsPrefix(gssName, ns, "w", 20, string(gamekruiseiov1alpha1.WaitToDelete), ""),
					makePodsPrefix(gssName, ns, "a", 78, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "3",
				"scaleDownThreshold": "10",
			},
			// totalNum=100, noneNum=2, WTBD=20, threshold=10, exceeded
			// desireReplicas = max(100-20, 3) = 80
			wantReplicas: 80,
		},
		// --- scaleDownThreshold: percentage mode, exceeded ---
		{
			name:     "threshold exceeded (percentage): scale-down prioritized",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 5, string(gamekruiseiov1alpha1.None), ""),
				append(
					makePodsPrefix(gssName, ns, "w", 20, string(gamekruiseiov1alpha1.WaitToDelete), ""),
					makePodsPrefix(gssName, ns, "a", 75, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "3",
				"scaleDownThreshold": "0.1",
			},
			// ratio = 20/100 = 0.2 > 0.1, exceeded
			// desireReplicas = max(100-20, 3) = 80
			wantReplicas: 80,
		},
		// --- scaleDownThreshold: not exceeded, falls through to existing logic ---
		{
			name:     "threshold not exceeded (absolute): existing scale-up logic",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 2, string(gamekruiseiov1alpha1.None), ""),
				append(
					makePodsPrefix(gssName, ns, "w", 3, string(gamekruiseiov1alpha1.WaitToDelete), ""),
					makePodsPrefix(gssName, ns, "a", 95, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "5",
				"scaleDownThreshold": "10",
			},
			// WTBD=3 <= 10, not exceeded. noneNum(2) < minNum(5)
			// desireReplicas = 100 + 5 - 2 = 103
			wantReplicas: 103,
		},
		// --- scaleDownThreshold: not exceeded, falls through to scale-down WTBD ---
		{
			name:     "threshold not exceeded, noneNum >= minNum: scale-down WTBD",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 10, string(gamekruiseiov1alpha1.None), ""),
				append(
					makePodsPrefix(gssName, ns, "w", 5, string(gamekruiseiov1alpha1.WaitToDelete), ""),
					makePodsPrefix(gssName, ns, "a", 85, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "3",
				"scaleDownThreshold": "10",
			},
			// WTBD=5 <= 10, not exceeded. noneNum(10) >= minNum(3)
			// desireReplicas = 100 - 5 = 95
			wantReplicas: 95,
		},
		// --- scaleDownThreshold: not set, existing behavior preserved ---
		{
			name:     "no threshold set, existing scale-up behavior",
			replicas: 10,
			pods: append(
				makePods(gssName, ns, 2, string(gamekruiseiov1alpha1.None), ""),
				makePodsPrefix(gssName, ns, "a", 8, string(gamekruiseiov1alpha1.Allocated), "")...,
			),
			metadata: map[string]string{
				"minAvailable": "5",
			},
			// noneNum(2) < minNum(5), desireReplicas = 10 + 5 - 2 = 13
			wantReplicas: 13,
		},
		// --- scaleDownThreshold: not set, existing scale-down behavior ---
		{
			name:     "no threshold set, existing scale-down WTBD behavior",
			replicas: 10,
			pods: append(
				makePods(gssName, ns, 5, string(gamekruiseiov1alpha1.None), ""),
				makePodsPrefix(gssName, ns, "w", 3, string(gamekruiseiov1alpha1.WaitToDelete), "")...,
			),
			metadata: map[string]string{
				"minAvailable": "3",
			},
			// noneNum(5) >= minNum(3), WTBD=3, desireReplicas = 10 - 3 = 7
			wantReplicas: 7,
		},
		// --- scaleDownThreshold: invalid string, falls through gracefully ---
		{
			name:     "invalid threshold string: falls through to existing logic",
			replicas: 10,
			pods: append(
				makePods(gssName, ns, 2, string(gamekruiseiov1alpha1.None), ""),
				makePodsPrefix(gssName, ns, "w", 5, string(gamekruiseiov1alpha1.WaitToDelete), "")...,
			),
			metadata: map[string]string{
				"minAvailable":  "5",
				"scaleDownThreshold": "invalid",
			},
			// invalid threshold, falls through. noneNum(2) < minNum(5)
			// desireReplicas = 10 + 5 - 2 = 13
			wantReplicas: 13,
		},
		// --- scaleDownThreshold: exceeded but minNum floor kicks in ---
		{
			name:     "threshold exceeded but minAvailable floor applied",
			replicas: 10,
			pods: append(
				makePodsPrefix(gssName, ns, "w", 8, string(gamekruiseiov1alpha1.WaitToDelete), ""),
				makePodsPrefix(gssName, ns, "a", 2, string(gamekruiseiov1alpha1.Allocated), "")...,
			),
			metadata: map[string]string{
				"minAvailable":  "5",
				"scaleDownThreshold": "5",
			},
			// totalNum=10, WTBD=8, threshold=5, exceeded
			// desireReplicas = max(10-8, 5) = max(2, 5) = 5
			wantReplicas: 5,
		},
		// --- scaleDownThreshold: zero WTBD, threshold check skipped ---
		{
			name:     "zero WTBD pods: threshold check skipped",
			replicas: 10,
			pods:     makePods(gssName, ns, 10, string(gamekruiseiov1alpha1.None), ""),
			metadata: map[string]string{
				"minAvailable":  "3",
				"scaleDownThreshold": "5",
			},
			// WTBD=0, threshold skipped. noneNum(10) >= minNum(3), WTBD=0
			// no maxAvailable, desireReplicas = 10
			wantReplicas: 10,
		},
		// --- scaleDownThreshold: percentage mode, not exceeded ---
		{
			name:     "threshold not exceeded (percentage): existing logic",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 10, string(gamekruiseiov1alpha1.None), ""),
				append(
					makePodsPrefix(gssName, ns, "w", 3, string(gamekruiseiov1alpha1.WaitToDelete), ""),
					makePodsPrefix(gssName, ns, "a", 87, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "5",
				"scaleDownThreshold": "0.1",
			},
			// ratio = 3/100 = 0.03 < 0.1, not exceeded. noneNum(10) >= minNum(5)
			// WTBD=3, desireReplicas = 100 - 3 = 97
			wantReplicas: 97,
		},
		// --- WTBD in Deleting state should not be counted ---
		{
			name:     "WTBD pods in Deleting state excluded from count",
			replicas: 100,
			pods: append(
				makePods(gssName, ns, 5, string(gamekruiseiov1alpha1.None), ""),
				append(
					append(
						makePodsPrefix(gssName, ns, "w1", 5, string(gamekruiseiov1alpha1.WaitToDelete), ""),
						makePodsPrefix(gssName, ns, "w2", 10, string(gamekruiseiov1alpha1.WaitToDelete), string(gamekruiseiov1alpha1.Deleting))...,
					),
					makePodsPrefix(gssName, ns, "a", 80, string(gamekruiseiov1alpha1.Allocated), "")...,
				)...,
			),
			metadata: map[string]string{
				"minAvailable":  "3",
				"scaleDownThreshold": "8",
			},
			// WTBD (non-Deleting) = 5, threshold=8, 5 <= 8, not exceeded
			// noneNum(5) >= minNum(3), WTBD=5, desireReplicas = 100 - 5 = 95
			wantReplicas: 95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestScheme()
			gss := makeGSS(gssName, ns, tt.replicas)

			objs := []client.Object{gss}
			for _, pod := range tt.pods {
				objs = append(objs, pod)
			}

			c := newFakeClient(s, objs...)
			scaler := NewExternalScaler(c)

			req := &GetMetricsRequest{
				ScaledObjectRef: &ScaledObjectRef{
					Name:           gssName,
					Namespace:      ns,
					ScalerMetadata: tt.metadata,
				},
			}

			resp, err := scaler.GetMetrics(context.Background(), req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetMetrics() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if len(resp.MetricValues) != 1 {
				t.Fatalf("expected 1 metric value, got %d", len(resp.MetricValues))
			}
			got := resp.MetricValues[0].MetricValue
			if got != tt.wantReplicas {
				t.Errorf("GetMetrics() desireReplicas = %d, want %d", got, tt.wantReplicas)
			}
		})
	}
}

func makePods(gssName, ns string, count int, opsState, state string) []*corev1.Pod {
	return makePodsPrefix(gssName, ns, "", count, opsState, state)
}

func makePodsPrefix(gssName, ns, prefix string, count int, opsState, state string) []*corev1.Pod {
	pods := make([]*corev1.Pod, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s%s-%d", prefix, gssName, i)
		pods[i] = makePod(name, ns, gssName, opsState, state)
	}
	return pods
}

func TestParseScaleDownThreshold(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantThresh float64
		wantPct    bool
		wantErr    bool
	}{
		{
			name:       "valid absolute integer",
			input:      "10",
			wantThresh: 10,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "valid absolute integer 1",
			input:      "1",
			wantThresh: 1,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.1",
			input:      "0.1",
			wantThresh: 0.1,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.5",
			input:      "0.5",
			wantThresh: 0.5,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.99",
			input:      "0.99",
			wantThresh: 0.99,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:    "invalid - zero",
			input:   "0",
			wantErr: true,
		},
		{
			name:    "invalid - negative",
			input:   "-5",
			wantErr: true,
		},
		{
			name:    "invalid - not a number",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "invalid - empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:       "float >= 1 treated as absolute",
			input:      "3.7",
			wantThresh: 4, // math.Ceil(3.7)
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "1.0 treated as absolute 1",
			input:      "1.0",
			wantThresh: 1,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "large absolute value",
			input:      "100",
			wantThresh: 100,
			wantPct:    false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotThresh, gotPct, err := parseScaleDownThreshold(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseScaleDownThreshold(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if gotPct != tt.wantPct {
				t.Errorf("parseScaleDownThreshold(%q) isPercentage = %v, want %v", tt.input, gotPct, tt.wantPct)
			}
			if math.Abs(gotThresh-tt.wantThresh) > 1e-9 {
				t.Errorf("parseScaleDownThreshold(%q) threshold = %v, want %v", tt.input, gotThresh, tt.wantThresh)
			}
		})
	}
}

func TestHandleMinNum(t *testing.T) {
	tests := []struct {
		name      string
		totalNum  int
		noneNum   int
		minNumStr string
		wantMin   int
		wantErr   bool
	}{
		{
			name:      "invalid minNumStr - not a number",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "abc",
			wantMin:   0,
			wantErr:   true,
		},
		{
			name:      "empty minNumStr - no scale up needed",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - delta <= 0, no scale up needed",
			totalNum:  10,
			noneNum:   5,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - delta > 0, scale up needed",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "0.5",
			wantMin:   8,
			wantErr:   false,
		},
		{
			name:      "percentage - delta > 0, minNum > totalNum",
			totalNum:  5,
			noneNum:   1,
			minNumStr: "0.8",
			wantMin:   16,
			wantErr:   false,
		},
		{
			name:      "percentage - exact match, no scale up",
			totalNum:  20,
			noneNum:   10,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - slightly below, scale up by 1",
			totalNum:  19,
			noneNum:   9,
			minNumStr: "0.5",
			wantMin:   10,
			wantErr:   false,
		},
		{
			name:      "integer - minNum >= 1",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "3",
			wantMin:   3,
			wantErr:   false,
		},
		{
			name:      "integer - minNum >= 1, float string",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "3.1",
			wantMin:   4,
			wantErr:   false,
		},
		{
			name:      "integer - minNum is 1",
			totalNum:  10,
			noneNum:   0,
			minNumStr: "1",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "invalid n - zero",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "0",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "invalid n - negative",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "-1",
			wantMin:   0,
			wantErr:   true,
		},
		{
			name:      "invalid n - percentage >= 1 (e.g. 1.0 treated as integer 1)",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "1.0",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 0, noneNum is 0",
			totalNum:  0,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "integer - totalNum is 0, noneNum is 0",
			totalNum:  0,
			noneNum:   0,
			minNumStr: "5",
			wantMin:   5,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 1, noneNum is 0, minNumStr 0.5",
			totalNum:  1,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 2, noneNum is 0, minNumStr 0.5",
			totalNum:  2,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   2,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum 100, noneNum 10, minNumStr 0.2",
			totalNum:  100,
			noneNum:   10,
			minNumStr: "0.2",
			wantMin:   23,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, err := handleMinNum(tt.totalNum, tt.noneNum, tt.minNumStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleMinNum() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				// If wantErr is true, we don't need to check gotMin
				return
			}
			if gotMin != tt.wantMin {
				// For debugging float calculations
				if n, parseErr := strconv.ParseFloat(tt.minNumStr, 32); parseErr == nil && n > 0 && n < 1 {
					delta := (float64(tt.totalNum)*n - float64(tt.noneNum)) / (1 - n)
					fmt.Printf("Debug for %s: totalNum=%d, noneNum=%d, minNumStr=%s, n=%f, delta=%f, ceil(delta)=%f, calculatedMinNum=%d\n",
						tt.name, tt.totalNum, tt.noneNum, tt.minNumStr, n, delta, math.Ceil(delta), int(math.Ceil(delta))+tt.noneNum)
				}
				t.Errorf("handleMinNum() = %v, want %v", gotMin, tt.wantMin)
			}
		})
	}
}
