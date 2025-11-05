package versioned

import (
	"testing"

	gamev1alpha1 "github.com/openkruise/kruise-game/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

func TestClientset_GameV1alpha1(t *testing.T) {
	mockClient := &gamev1alpha1.GameV1alpha1Client{}
	clientset := &Clientset{gameV1alpha1: mockClient}

	if result := clientset.GameV1alpha1(); result != mockClient {
		t.Errorf("Expected GameV1alpha1() to return the mock client, got %v", result)
	}
}

func TestClientset_Discovery(t *testing.T) {
	t.Run("nil clientset returns nil", func(t *testing.T) {
		var nilClientset *Clientset
		if result := nilClientset.Discovery(); result != nil {
			t.Errorf("Expected nil, got %v", result)
		}
	})

	t.Run("valid clientset returns discovery client", func(t *testing.T) {
		dc := &discovery.DiscoveryClient{}
		clientset := &Clientset{DiscoveryClient: dc}
		if result := clientset.Discovery(); result != dc {
			t.Errorf("Expected %v, got %v", dc, result)
		}
	})
}

func TestNew(t *testing.T) {
	mockRESTClient := &rest.RESTClient{}
	clientset := New(mockRESTClient)

	if clientset == nil {
		t.Fatal("Expected non-nil clientset")
	}
	if clientset.gameV1alpha1 == nil {
		t.Error("Expected gameV1alpha1 client to be initialized")
	}
	if clientset.DiscoveryClient == nil {
		t.Error("Expected DiscoveryClient to be initialized")
	}
}

func TestBurstValidation(t *testing.T) {
	tests := []struct {
		name        string
		qps         float32
		burst       int
		shouldError bool
	}{
		{"zero QPS allowed", 0, 0, false},
		{"positive QPS and burst allowed", 10.0, 20, false},
		{"positive QPS with zero burst should error", 10.0, 0, true},
		{"positive QPS with negative burst should error", 10.0, -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &rest.Config{
				Host:  "https://example.com",
				QPS:   tt.qps,
				Burst: tt.burst,
			}

			_, err := NewForConfig(config)

			if tt.shouldError && err == nil {
				t.Error("Expected burst validation error but got none")
			} else if tt.shouldError && err != nil {
				t.Logf("Got expected error: %v", err)
			}
		})
	}
}

func TestConfigNotModified(t *testing.T) {
	originalConfig := &rest.Config{
		Host:      "https://example.com",
		UserAgent: "original-agent",
		QPS:       10.0,
		Burst:     20,
	}

	originalUA := originalConfig.UserAgent
	originalQPS := originalConfig.QPS
	originalBurst := originalConfig.Burst

	_, _ = NewForConfig(originalConfig)

	if originalConfig.UserAgent != originalUA {
		t.Error("Original config UserAgent was modified")
	}
	if originalConfig.QPS != originalQPS {
		t.Error("Original config QPS was modified")
	}
	if originalConfig.Burst != originalBurst {
		t.Error("Original config Burst was modified")
	}
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("invalid burst should panic", func(t *testing.T) {
		config := &rest.Config{
			Host:  "https://example.com",
			QPS:   10.0,
			Burst: 0,
		}

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with invalid config")
			}
		}()

		NewForConfigOrDie(config)
	})
}

func TestClientset_Interface(t *testing.T) {
	var _ Interface = &Clientset{}
}
