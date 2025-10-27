package logging

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/openkruise/kruise-game/pkg/version"
)

// JSONPreset defines the field layout to use for structured JSON logs.
type JSONPreset string

const (
	// JSONPresetKibana keeps the legacy Kibana-friendly field names.
	JSONPresetKibana JSONPreset = "kibana"
	// JSONPresetOTel aligns with OpenTelemetry log data model field names.
	JSONPresetOTel JSONPreset = "otel"

	defaultServiceName = "okg-controller-manager"
)

// ResourceMetadata holds environment identifiers attached to every log entry.
type ResourceMetadata struct {
	ServiceName       string
	ServiceVersion    string
	ServiceInstanceID string
	Namespace         string
	PodName           string
}

// JSONConfig captures JSON logging behaviour.
type JSONConfig struct {
	Preset   JSONPreset
	Resource ResourceMetadata
}

var (
	jsonConfigMu sync.RWMutex
	jsonConfig   = defaultJSONConfig()
	activeJSON   bool
)

// ParseJSONPreset validates preset strings coming from flags or env.
func ParseJSONPreset(value string) (JSONPreset, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(JSONPresetKibana):
		return JSONPresetKibana, nil
	case string(JSONPresetOTel):
		return JSONPresetOTel, nil
	default:
		return "", fmt.Errorf("unsupported json preset %q", value)
	}
}

// SetJSONConfig updates the global JSON logging configuration.
func SetJSONConfig(cfg JSONConfig) {
	jsonConfigMu.Lock()
	defer jsonConfigMu.Unlock()

	if cfg.Preset == "" {
		cfg.Preset = JSONPresetKibana
	}

	cfg.Resource = mergeResource(cfg.Resource, loadResourceMetadata())
	jsonConfig = cfg
}

func currentJSONConfig() JSONConfig {
	jsonConfigMu.RLock()
	defer jsonConfigMu.RUnlock()
	return jsonConfig
}

func setActiveJSON(value bool) {
	jsonConfigMu.Lock()
	defer jsonConfigMu.Unlock()
	activeJSON = value
}

func isActiveJSON() bool {
	jsonConfigMu.RLock()
	defer jsonConfigMu.RUnlock()
	return activeJSON
}

func defaultJSONConfig() JSONConfig {
	return JSONConfig{
		Preset:   JSONPresetKibana,
		Resource: loadResourceMetadata(),
	}
}

func mergeResource(primary, fallback ResourceMetadata) ResourceMetadata {
	if primary.ServiceName == "" {
		primary.ServiceName = fallback.ServiceName
	}
	if primary.ServiceVersion == "" {
		primary.ServiceVersion = fallback.ServiceVersion
	}
	if primary.ServiceInstanceID == "" {
		primary.ServiceInstanceID = fallback.ServiceInstanceID
	}
	if primary.Namespace == "" {
		primary.Namespace = fallback.Namespace
	}
	if primary.PodName == "" {
		primary.PodName = fallback.PodName
	}
	return primary
}

func loadResourceMetadata() ResourceMetadata {
	return ResourceMetadata{
		ServiceName:       firstNonEmpty(os.Getenv("OKG_SERVICE_NAME"), os.Getenv("SERVICE_NAME"), defaultServiceName),
		ServiceVersion:    firstNonEmpty(os.Getenv("OKG_SERVICE_VERSION"), os.Getenv("SERVICE_VERSION"), os.Getenv("OKG_VERSION"), version.Version),
		ServiceInstanceID: firstNonEmpty(os.Getenv("OKG_SERVICE_INSTANCE_ID"), os.Getenv("POD_UID")),
		Namespace:         firstNonEmpty(os.Getenv("OKG_NAMESPACE"), os.Getenv("POD_NAMESPACE")),
		PodName:           firstNonEmpty(os.Getenv("OKG_POD_NAME"), os.Getenv("POD_NAME"), os.Getenv("HOSTNAME")),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (cfg JSONConfig) resourceKeyValues() []interface{} {
	var kv []interface{}
	if cfg.Resource.ServiceName != "" {
		kv = append(kv, "service.name", cfg.Resource.ServiceName)
	}
	if cfg.Resource.ServiceVersion != "" {
		kv = append(kv, "service.version", cfg.Resource.ServiceVersion)
	}
	if cfg.Resource.ServiceInstanceID != "" {
		kv = append(kv, "service.instance.id", cfg.Resource.ServiceInstanceID)
	}
	if cfg.Resource.Namespace != "" {
		kv = append(kv, "k8s.namespace.name", cfg.Resource.Namespace)
	}
	if cfg.Resource.PodName != "" {
		kv = append(kv, "k8s.pod.name", cfg.Resource.PodName)
	}
	return kv
}

// ResourceKeyValues returns a copy of the resource key/value pairs used for loggers.
func ResourceKeyValues() []interface{} {
	cfg := currentJSONConfig()
	values := cfg.resourceKeyValues()
	if len(values) == 0 {
		return nil
	}
	out := make([]interface{}, len(values))
	copy(out, values)
	return out
}
