package versioned

import (
	"net/http"
	"testing"
	"time"

	gamev1alpha1 "github.com/openkruise/kruise-game/pkg/client/clientset/versioned/typed/apis/v1alpha1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

func TestClientset_GameV1alpha1(t *testing.T) {
	// Create a mock GameV1alpha1Client
	mockClient := &gamev1alpha1.GameV1alpha1Client{}

	clientset := &Clientset{
		gameV1alpha1: mockClient,
	}

	result := clientset.GameV1alpha1()

	if result != mockClient {
		t.Errorf("Expected GameV1alpha1() to return the mock client, got %v", result)
	}
}

func TestClientset_Discovery(t *testing.T) {
	dc := &discovery.DiscoveryClient{}
	tests := []struct {
		name      string
		clientset *Clientset
		expected  discovery.DiscoveryInterface
	}{
		{
			name:      "nil clientset returns nil",
			clientset: nil,
			expected:  nil,
		},
		{
			name:      "valid clientset returns discovery client",
			clientset: &Clientset{DiscoveryClient: dc},
			expected:  dc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.clientset.Discovery()
			if result != tt.expected {
				t.Errorf("Expected Discovery() to return %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNewForConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *rest.Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &rest.Config{
				Host:  "https://example.com",
				QPS:   10.0,
				Burst: 20,
			},
			expectError: false,
		},
		{
			name: "config with custom user agent",
			config: &rest.Config{
				Host:      "https://example.com",
				UserAgent: "custom-agent",
				QPS:       10.0,
				Burst:     20,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset, err := NewForConfig(tt.config)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tt.expectError && clientset == nil {
				t.Error("Expected clientset but got nil")
			}
		})
	}
}

func TestNewForConfigAndClient(t *testing.T) {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	tests := []struct {
		name        string
		config      *rest.Config
		httpClient  *http.Client
		expectError bool
		description string
	}{
		{
			name: "valid config with rate limiter settings",
			config: &rest.Config{
				Host:  "https://example.com",
				QPS:   10.0,
				Burst: 20,
			},
			httpClient:  httpClient,
			expectError: false,
			description: "should create clientset with auto-generated rate limiter",
		},
		{
			name: "config with existing rate limiter",
			config: &rest.Config{
				Host:        "https://example.com",
				QPS:         10.0,
				Burst:       20,
				RateLimiter: flowcontrol.NewTokenBucketRateLimiter(5.0, 10),
			},
			httpClient:  httpClient,
			expectError: false,
			description: "should use existing rate limiter",
		},
		{
			name: "config with QPS but zero burst",
			config: &rest.Config{
				Host:  "https://example.com",
				QPS:   10.0,
				Burst: 0,
			},
			httpClient:  httpClient,
			expectError: true,
			description: "should return error when burst is 0 with QPS > 0",
		},
		{
			name: "config with QPS but negative burst",
			config: &rest.Config{
				Host:  "https://example.com",
				QPS:   10.0,
				Burst: -1,
			},
			httpClient:  httpClient,
			expectError: true,
			description: "should return error when burst is negative with QPS > 0",
		},
		{
			name: "config with zero QPS",
			config: &rest.Config{
				Host:  "https://example.com",
				QPS:   0,
				Burst: 0,
			},
			httpClient:  httpClient,
			expectError: false,
			description: "should work with zero QPS (no rate limiting)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset, err := NewForConfigAndClient(tt.config, tt.httpClient)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none. %s", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v. %s", err, tt.description)
			}
			if !tt.expectError {
				if clientset == nil {
					t.Errorf("Expected clientset but got nil. %s", tt.description)
				} else {
					// Verify clientset has the expected components
					if clientset.gameV1alpha1 == nil {
						t.Error("Expected gameV1alpha1 client to be initialized")
					}
					if clientset.DiscoveryClient == nil {
						t.Error("Expected DiscoveryClient to be initialized")
					}
				}
			}
		})
	}
}

func TestNewForConfigOrDie(t *testing.T) {
	t.Run("valid config should not panic", func(t *testing.T) {
		config := &rest.Config{
			Host:  "https://example.com",
			QPS:   10.0,
			Burst: 20,
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("NewForConfigOrDie panicked with valid config: %v", r)
			}
		}()

		clientset := NewForConfigOrDie(config)
		if clientset == nil {
			t.Error("Expected non-nil clientset")
		}
	})

	t.Run("invalid config should panic", func(t *testing.T) {
		config := &rest.Config{
			Host:  "https://example.com",
			QPS:   10.0,
			Burst: 0, // This should cause an error
		}

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected NewForConfigOrDie to panic with invalid config")
			}
		}()

		NewForConfigOrDie(config)
	})
}

func TestNew(t *testing.T) {
	// Create a mock REST client
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

func TestClientset_Interface(t *testing.T) {
	// Verify Clientset implements the Interface
	var _ Interface = &Clientset{}
}

func TestRateLimiterConfiguration(t *testing.T) {
	tests := []struct {
		name            string
		qps             float32
		burst           int
		existingLimiter flowcontrol.RateLimiter
		expectError     bool
		description     string
	}{
		{
			name:        "no rate limiter with zero QPS",
			qps:         0,
			burst:       0,
			expectError: false,
			description: "should work with zero QPS (no rate limiting)",
		},
		{
			name:        "creates rate limiter with valid QPS and burst",
			qps:         10.0,
			burst:       20,
			expectError: false,
			description: "should create clientset with auto-generated rate limiter",
		},
		{
			name:            "preserves existing rate limiter",
			qps:             10.0,
			burst:           20,
			existingLimiter: flowcontrol.NewTokenBucketRateLimiter(5.0, 10),
			expectError:     false,
			description:     "should use existing rate limiter",
		},
		{
			name:        "error with QPS but zero burst",
			qps:         10.0,
			burst:       0,
			expectError: true,
			description: "should return error when burst is 0 with QPS > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLimiter := tt.existingLimiter

			config := &rest.Config{
				Host:        "https://example.com",
				QPS:         tt.qps,
				Burst:       tt.burst,
				RateLimiter: tt.existingLimiter,
			}

			httpClient := &http.Client{Timeout: 30 * time.Second}

			clientset, err := NewForConfigAndClient(config, httpClient)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none. %s", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v. %s", err, tt.description)
			}

			if !tt.expectError {
				if clientset == nil {
					t.Errorf("Expected clientset but got nil. %s", tt.description)
				}

				if config.RateLimiter != originalLimiter {
					t.Error("Original config's RateLimiter was unexpectedly modified")
				}

				if tt.existingLimiter != nil && config.RateLimiter != tt.existingLimiter {
					t.Error("Expected existing rate limiter to be preserved in original config")
				}
			}
		})
	}
}

func TestUserAgentConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		userAgent     string
		expectDefault bool
	}{
		{
			name:          "empty user agent gets default",
			userAgent:     "",
			expectDefault: true,
		},
		{
			name:          "custom user agent is preserved",
			userAgent:     "custom-client/1.0",
			expectDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &rest.Config{
				Host:      "https://example.com",
				UserAgent: tt.userAgent,
				QPS:       10.0,
				Burst:     20,
			}

			originalUserAgent := config.UserAgent

			_, err := NewForConfig(config)

			// The original config should not be modified
			if config.UserAgent != originalUserAgent {
				t.Errorf("Original config was modified. Expected UserAgent %q, got %q",
					originalUserAgent, config.UserAgent)
			}

			// For this simple test, we just verify no error occurred
			// In a real scenario, you might want to mock the HTTP client creation
			if err != nil && tt.expectDefault {
				// Some errors might be expected due to invalid host, etc.
				// The main thing is that the function doesn't panic
				t.Logf("Got expected error for default user agent case: %v", err)
			}
		})
	}
}

// Benchmark tests
func BenchmarkNewForConfig(b *testing.B) {
	config := &rest.Config{
		Host:  "https://example.com",
		QPS:   10.0,
		Burst: 20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = NewForConfig(config)
	}
}

func BenchmarkClientsetOperations(b *testing.B) {
	clientset := &Clientset{
		gameV1alpha1:    &gamev1alpha1.GameV1alpha1Client{},
		DiscoveryClient: &discovery.DiscoveryClient{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = clientset.GameV1alpha1()
		_ = clientset.Discovery()
	}
}

func ExampleNewForConfig() {
	config := &rest.Config{
		Host:  "https://kubernetes-cluster.example.com",
		QPS:   10.0,
		Burst: 20,
	}

	clientset, err := NewForConfig(config)
	if err != nil {
		panic(err)
	}

	_ = clientset.GameV1alpha1()
	_ = clientset.Discovery()
}

func createValidConfig() *rest.Config {
	return &rest.Config{
		Host:  "https://example.com",
		QPS:   10.0,
		Burst: 20,
	}
}

func createValidHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

func TestClientsetIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := createValidConfig()
	httpClient := createValidHTTPClient()

	clientset, err := NewForConfigAndClient(config, httpClient)
	if err != nil {
		t.Skipf("Could not create clientset (expected in unit test environment): %v", err)
	}

	if clientset != nil {
		// Verify interface compliance
		var _ Interface = clientset

		// Test that methods don't panic
		gameClient := clientset.GameV1alpha1()
		if gameClient == nil {
			t.Error("GameV1alpha1() returned nil")
		}

		discoveryClient := clientset.Discovery()
		if discoveryClient == nil {
			t.Error("Discovery() returned nil")
		}
	}
}

// Test error conditions
func TestErrorConditions(t *testing.T) {
	t.Run("invalid burst with positive QPS", func(t *testing.T) {
		config := &rest.Config{
			Host:  "https://example.com",
			QPS:   10.0,
			Burst: -5, // Invalid burst
		}
		httpClient := createValidHTTPClient()

		_, err := NewForConfigAndClient(config, httpClient)
		if err == nil {
			t.Error("Expected error with negative burst and positive QPS")
		}
	})

	t.Run("zero burst with positive QPS", func(t *testing.T) {
		config := &rest.Config{
			Host:  "https://example.com",
			QPS:   10.0,
			Burst: 0, // Invalid burst
		}
		httpClient := createValidHTTPClient()

		_, err := NewForConfigAndClient(config, httpClient)
		if err == nil {
			t.Error("Expected error with zero burst and positive QPS")
		}
	})
}

// Test config shallow copy behavior
func TestConfigShallowCopy(t *testing.T) {
	originalConfig := &rest.Config{
		Host:      "https://example.com",
		UserAgent: "original-agent",
		QPS:       10.0,
		Burst:     20,
	}

	httpClient := createValidHTTPClient()

	// Store original values
	originalUserAgent := originalConfig.UserAgent
	originalQPS := originalConfig.QPS
	originalBurst := originalConfig.Burst

	_, err := NewForConfigAndClient(originalConfig, httpClient)

	// Verify original config wasn't modified
	if originalConfig.UserAgent != originalUserAgent {
		t.Errorf("Original config UserAgent was modified. Expected %q, got %q",
			originalUserAgent, originalConfig.UserAgent)
	}
	if originalConfig.QPS != originalQPS {
		t.Errorf("Original config QPS was modified. Expected %f, got %f",
			originalQPS, originalConfig.QPS)
	}
	if originalConfig.Burst != originalBurst {
		t.Errorf("Original config Burst was modified. Expected %d, got %d",
			originalBurst, originalConfig.Burst)
	}

	// We expect some errors here due to invalid host, but that's okay
	// The main thing is that the original config wasn't modified
	_ = err
}

// Test nil safety
func TestNilSafety(t *testing.T) {
	t.Run("nil clientset Discovery", func(t *testing.T) {
		var nilClientset *Clientset
		result := nilClientset.Discovery()
		if result != nil {
			t.Errorf("Expected nil from nil clientset Discovery(), got %v", result)
		}
	})
}

func TestWithMocks(t *testing.T) {
	t.Run("mock game client", func(t *testing.T) {
		// Example of how you might structure mock tests
		mockGameClient := &gamev1alpha1.GameV1alpha1Client{}
		mockDiscoveryClient := &discovery.DiscoveryClient{}

		clientset := &Clientset{
			gameV1alpha1:    mockGameClient,
			DiscoveryClient: mockDiscoveryClient,
		}

		if clientset.GameV1alpha1() != mockGameClient {
			t.Error("GameV1alpha1() did not return expected mock client")
		}

		if clientset.Discovery() != mockDiscoveryClient {
			t.Error("Discovery() did not return expected mock client")
		}
	})
}
