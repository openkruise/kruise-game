package fields

// Minimal collection of common telemetry attribute field names to avoid import cycles
// between logging and tracing packages. This package contains only string constants used
// by both tracing and logging code to name key attributes.
const (
	FieldK8sNamespaceName = "k8s.namespace.name"
	FieldK8sPodName       = "k8s.pod.name"
	FieldK8sNodeName      = "k8s.node.name"
	FieldServiceName      = "service.name"
	FieldServiceNamespace = "service.namespace"
)
