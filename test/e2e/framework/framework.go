package framework

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	"github.com/openkruise/kruise-game/pkg/util"
	"github.com/openkruise/kruise-game/test/e2e/client"
	"github.com/openkruise/kruise-game/test/e2e/diagnostics"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
)

type Framework struct {
	client            *client.Client
	testStart         time.Time
	artifactCollector *diagnostics.ArtifactCollector
	currentTestName   string
	gssName           string
	// RestConfig is required to create SPDY executors for remote command execution (exec)
	// This is specifically needed for tests that trigger ServiceQualities via file existence checks
	RestConfig *restclient.Config
}

func NewFrameWork(config *restclient.Config) *Framework {
	kruisegameClient := kruisegameclientset.NewForConfigOrDie(config)
	kubeClient := clientset.NewForConfigOrDie(config)
	return &Framework{
		client:     client.NewKubeClient(kruisegameClient, kubeClient),
		gssName:    client.DefaultGameServerSetName,
		RestConfig: config,
	}
}

// GameServerSetName returns the current GSS name used by this framework.
func (f *Framework) GameServerSetName() string {
	if f.gssName == "" {
		f.gssName = client.DefaultGameServerSetName
	}
	return f.gssName
}

// SetGameServerSetName overrides the default GameServerSet name for the current test scope.
func (f *Framework) SetGameServerSetName(name string) {
	if name == "" {
		name = client.DefaultGameServerSetName
	}
	f.gssName = name
	if f.artifactCollector != nil {
		f.artifactCollector.SetGameServerSetLabel(gamekruiseiov1alpha1.GameServerOwnerGssKey, name)
	}
}

// KubeClientSet returns the Kubernetes clientset for accessing core APIs.
func (f *Framework) KubeClientSet() clientset.Interface {
	return f.client.GetKubeClient()
}

// GetPodList returns the list of pods matching the given label selector.
func (f *Framework) GetPodList(labelSelector string) (*corev1.PodList, error) {
	return f.client.GetPodList(labelSelector)
}

// GetGameServerList returns the list of GameServers matching the given label selector.
func (f *Framework) GetGameServerList(labelSelector string) (*gamekruiseiov1alpha1.GameServerList, error) {
	return f.client.GetGameServerList(labelSelector)
}

// GetService returns the service with the given name.
func (f *Framework) GetService(name string) (*corev1.Service, error) {
	return f.client.GetService(name)
}

// MarkTestStart records the approximate start time of the current test and initializes artifact collector.
func (f *Framework) MarkTestStart() {
	f.testStart = time.Now()
}

// InitTestArtifactCollector initializes artifact collector for a named test
func (f *Framework) InitTestArtifactCollector(testName string) {
	suffix := os.Getenv("E2E_ARTIFACT_SUFFIX")
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	dirName := fmt.Sprintf("%s_%s", sanitizeTestName(testName), suffix)
	f.currentTestName = fmt.Sprintf("%s (%s)", testName, suffix)
	artifactRoot := os.Getenv("E2E_ARTIFACT_ROOT")
	if artifactRoot == "" {
		artifactRoot = "./e2e-artifacts"
	}

	f.artifactCollector = diagnostics.NewArtifactCollector(
		dirName,
		artifactRoot,
		f.client.GetKubeClient(),
		f.client.GetKruiseGameClient(),
		client.Namespace,
	)
	f.artifactCollector.SetGameServerSetLabel(gamekruiseiov1alpha1.GameServerOwnerGssKey, f.GameServerSetName())
}

// CollectTestArtifacts collects all diagnostic artifacts for the current test
func (f *Framework) CollectTestArtifacts(passed bool, failureMsg string) {
	if f.artifactCollector == nil {
		return
	}

	logArtifacts := shouldLogArtifacts(passed)
	f.artifactCollector.MarkTestEnd(passed, failureMsg)

	// Find audit log path (try multiple possible locations for kind clusters)
	auditLogPath := f.findAuditLogPath()
	if logArtifacts {
		if auditLogPath != "" {
			fmt.Printf("Found audit log at: %s\n", auditLogPath)
		} else {
			fmt.Printf("WARNING: Could not locate audit log\n")
		}
	}

	if err := f.artifactCollector.CollectAll(auditLogPath); err != nil {
		fmt.Printf("Warning: failed to collect artifacts: %v\n", err)
	}

	// Also collect Tempo traces if available
	f.collectTempoTraces(logArtifacts)

	// Print summary of where artifacts are stored
	if logArtifacts {
		fmt.Printf("\n===== ARTIFACTS COLLECTED =====\n")
		fmt.Printf("Test: %s\n", f.currentTestName)
		fmt.Printf("Status: %s\n", map[bool]string{true: "PASSED", false: "FAILED"}[passed])
		fmt.Printf("Artifacts directory: %s\n", f.artifactCollector.GetTestDir())
		fmt.Printf("==============================\n\n")
	}
}

// SaveTextArtifact writes textual diagnostic content into the current test's artifact dir.
func (f *Framework) SaveTextArtifact(relPath, content string) {
	if f.artifactCollector == nil {
		fmt.Printf("artifact collector not initialized; cannot save %s\n", relPath)
		return
	}
	if err := f.artifactCollector.SaveTextArtifact(relPath, content); err != nil {
		fmt.Printf("Warning: failed to save artifact %s: %v\n", relPath, err)
	} else {
		fmt.Printf("Saved artifact: %s\n", relPath)
	}
}

func shouldLogArtifacts(passed bool) bool {
	if !passed {
		return true
	}
	return isEnvTruthy(os.Getenv("E2E_ARTIFACTS_ALWAYS"))
}

// findAuditLogPath tries to locate the audit log file from the kind cluster
func (f *Framework) findAuditLogPath() string {
	// Try environment variable first
	if path := os.Getenv("E2E_AUDIT_LOG_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Try common locations for kind clusters
	possiblePaths := []string{
		"/tmp/kube-apiserver-audit.log",
		"/var/log/kubernetes/kube-apiserver-audit.log",
		"./audit.log",
		"../audit.log",
	}

	// Also try the artifact root directory
	artifactRoot := os.Getenv("E2E_ARTIFACT_ROOT")
	if artifactRoot == "" {
		artifactRoot = "./e2e-artifacts"
	}
	possiblePaths = append(possiblePaths, fmt.Sprintf("%s/../audit.log", artifactRoot))

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// collectTempoTraces collects trace data from Tempo for the test time range
func (f *Framework) collectTempoTraces(verbose bool) {
	if f.artifactCollector == nil {
		return
	}

	startTime, endTime := f.artifactCollector.GetTimeRange()
	var startSec, endSec int64
	if !startTime.IsZero() {
		startSec = startTime.Add(-1 * time.Minute).Unix()
	}
	if !endTime.IsZero() {
		endSec = endTime.Add(1 * time.Minute).Unix()
	}

	// Query Tempo for all traces in test time range
	filters := map[string]string{}
	if gss := f.GameServerSetName(); gss != "" {
		filters["game.kruise.io.game_server_set.name"] = gss
	}
	result, err := f.SearchTracesInTempo(context.Background(), "okg-controller-manager", 0, 500, filters, startSec, endSec)
	if err != nil {
		fmt.Printf("Warning: failed to query Tempo traces: %v\n", err)
		return
	}

	// Save trace search results
	observabilityDir := fmt.Sprintf("%s/observability", f.artifactCollector.GetTestDir())
	if err := os.MkdirAll(observabilityDir, 0755); err != nil {
		return
	}

	tracesPath := fmt.Sprintf("%s/tempo-traces.json", observabilityDir)
	if data, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = os.WriteFile(tracesPath, data, 0644)
	}

	// Also retrieve full trace details for each trace
	if len(result.Traces) > 0 {
		detailsPath := fmt.Sprintf("%s/tempo-trace-details.json", observabilityDir)
		allTraces := make(map[string]*tracepb.TracesData)

		// Increased limit from 10 to 500 to ensure we capture webhook traces
		// (webhook traces may appear later in the list due to lower frequency than controller reconcile traces)
		maxTraces := 500
		for i, t := range result.Traces {
			if i >= maxTraces {
				break
			}
			traceID := t.TraceID
			trace, err := f.GetTraceByID(traceID)
			if err != nil {
				fmt.Printf("Warning: failed to fetch trace %s: %v\n", traceID, err)
				continue
			}
			allTraces[traceID] = trace
		}

		if verbose {
			fmt.Printf("Collected %d trace details from %d total traces\n", len(allTraces), len(result.Traces))
		}

		serialized := make(map[string]json.RawMessage, len(allTraces))
		marshaler := protojson.MarshalOptions{
			Multiline:       true,
			Indent:          "  ",
			EmitUnpopulated: false,
		}
		for id, trace := range allTraces {
			raw, err := marshaler.Marshal(trace)
			if err != nil {
				fmt.Printf("Warning: failed to serialize trace %s: %v\n", id, err)
				continue
			}
			serialized[id] = raw
		}
		if data, err := json.MarshalIndent(serialized, "", "  "); err == nil {
			_ = os.WriteFile(detailsPath, data, 0644)
		}
	}

	if verbose {
		fmt.Printf("Collected %d traces from Tempo (time range: %s to %s)\n",
			len(result.Traces), startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	}
}

// QueryPrometheus executes a Prometheus query against the local observability stack.
func (f *Framework) QueryPrometheus(query string) ([]byte, error) {
	params := map[string]string{
		"query": query,
	}
	req := f.KubeClientSet().CoreV1().Services("observability").ProxyGet(
		"http",
		"prometheus-server",
		"80",
		"api/v1/query",
		params,
	)
	return req.DoRaw(context.TODO())
}

// FetchControllerMetricsSample fetches raw metrics from the controller manager Service (native Prom endpoint).
func (f *Framework) FetchControllerMetricsSample(limit int) (string, error) {
	svcName, err := f.findControllerMetricsService()
	if err != nil {
		return "", err
	}
	req := f.KubeClientSet().CoreV1().Services("kruise-game-system").ProxyGet(
		"",
		svcName,
		"http-metrics",
		"metrics",
		nil,
	)
	data, err := req.DoRaw(context.TODO())
	if err != nil {
		return "", err
	}
	content := string(data)
	if limit > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > limit {
			lines = lines[:limit]
		}
		return strings.Join(lines, "\n"), nil
	}
	return content, nil
}

func (f *Framework) findControllerMetricsService() (string, error) {
	svcList, err := f.KubeClientSet().CoreV1().Services("kruise-game-system").List(
		context.TODO(),
		metav1.ListOptions{LabelSelector: "control-plane=controller-manager"},
	)
	if err != nil {
		return "", err
	}
	for _, svc := range svcList.Items {
		if strings.Contains(svc.Name, "metrics-service") {
			return svc.Name, nil
		}
	}
	return "", fmt.Errorf("controller metrics service not found in kruise-game-system namespace")
}

// ======================== Tracing Query Utilities ========================

// TempoSearchResult mirrors the JSON that Tempo's /api/search endpoint returns.
// Tempo encodes some numeric values (like startTimeUnixNano) as strings, so we
// keep them as-is here to avoid JSON decoding failures and convert later when needed.
type TempoSearchResult struct {
	Traces []TempoTraceSummary `json:"traces"`
}

type TempoTraceSummary struct {
	TraceID           string `json:"traceID"`
	RootServiceName   string `json:"rootServiceName"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	DurationMs        int    `json:"durationMs"`
}

// SpanView is a lightweight helper that exposes frequently used span metadata
// while keeping a reference to the original OTLP span for advanced inspection
// (links, events, attributes with richer typing, etc.).
type SpanView struct {
	SpanID        string                 `json:"spanID"`
	TraceID       string                 `json:"traceID"`
	OperationName string                 `json:"operationName"`
	StartTime     int64                  `json:"startTime"`
	Duration      int64                  `json:"duration"`
	Tags          map[string]interface{} `json:"tags,omitempty"`
	ServiceName   string                 `json:"serviceName,omitempty"`
	ParentSpanID  string                 `json:"parentSpanID,omitempty"`
	Raw           *tracepb.Span          `json:"-"`
}

func newSpanView(serviceName string, span *tracepb.Span) *SpanView {
	if span == nil {
		return nil
	}

	duration := int64(span.GetEndTimeUnixNano() - span.GetStartTimeUnixNano())
	view := &SpanView{
		SpanID:        hex.EncodeToString(span.GetSpanId()),
		TraceID:       hex.EncodeToString(span.GetTraceId()),
		OperationName: span.GetName(),
		StartTime:     int64(span.GetStartTimeUnixNano()),
		Duration:      duration,
		Tags:          otlpAttributesToMap(span.GetAttributes()),
		ServiceName:   serviceName,
		ParentSpanID:  hex.EncodeToString(span.GetParentSpanId()),
		Raw:           span,
	}

	// Normalize zero parent span ID to empty string for easier checks
	if view.ParentSpanID == "" || view.ParentSpanID == "0000000000000000" {
		view.ParentSpanID = ""
	}
	return view
}

// ExtractSpanViews converts an OTLP trace into SpanView helpers for convenient assertions.
func ExtractSpanViews(trace *tracepb.TracesData) []*SpanView {
	if trace == nil {
		return nil
	}

	var views []*SpanView
	for _, resourceSpans := range trace.GetResourceSpans() {
		serviceName := extractServiceName(resourceSpans)
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				views = append(views, newSpanView(serviceName, span))
			}
		}
	}
	return views
}

func extractServiceName(rs *tracepb.ResourceSpans) string {
	if rs == nil || rs.Resource == nil {
		return "unknown"
	}
	for _, attr := range rs.Resource.Attributes {
		if attr.GetKey() == "service.name" {
			if value := attr.Value.GetStringValue(); value != "" {
				return value
			}
		}
	}
	return "unknown"
}

func otlpAttributesToMap(attrs []*commonpb.KeyValue) map[string]interface{} {
	result := make(map[string]interface{}, len(attrs))
	for _, attr := range attrs {
		if attr == nil {
			continue
		}
		if attr.Value == nil {
			continue
		}
		switch v := attr.Value.GetValue().(type) {
		case *commonpb.AnyValue_StringValue:
			result[attr.Key] = v.StringValue
		case *commonpb.AnyValue_IntValue:
			result[attr.Key] = v.IntValue
		case *commonpb.AnyValue_DoubleValue:
			result[attr.Key] = v.DoubleValue
		case *commonpb.AnyValue_BoolValue:
			result[attr.Key] = v.BoolValue
		case *commonpb.AnyValue_BytesValue:
			result[attr.Key] = hex.EncodeToString(v.BytesValue)
		case *commonpb.AnyValue_ArrayValue:
			result[attr.Key] = v.ArrayValue
		case *commonpb.AnyValue_KvlistValue:
			result[attr.Key] = v.KvlistValue
		default:
			// Fallback to the string representation if value type is not explicitly handled
			result[attr.Key] = attr.Value.String()
		}
	}
	return result
}

func countTraceSpans(trace *tracepb.TracesData) int {
	if trace == nil {
		return 0
	}
	count := 0
	for _, rs := range trace.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			count += len(ss.GetSpans())
		}
	}
	return count
}

const (
	// Use K8s Service DNS for in-cluster access (can be overridden by env vars)
	defaultTempoURL = "http://tempo.observability.svc.cluster.local:3200"
	defaultLokiURL  = "http://loki.observability.svc.cluster.local:3100"
)

var tempoDebugEnabled = isEnvTruthy(os.Getenv("E2E_TEMPO_DEBUG"))

func tempoDebugf(format string, args ...interface{}) {
	if tempoDebugEnabled {
		fmt.Printf(format, args...)
	}
}

func isEnvTruthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// getTempoURL returns the Tempo URL, checking environment variable first
func getTempoURL() string {
	if url := os.Getenv("TEMPO_URL"); url != "" {
		return url
	}
	return defaultTempoURL
}

// getLokiURL returns the Loki URL, checking environment variable first
func getLokiURL() string {
	if url := os.Getenv("LOKI_URL"); url != "" {
		return url
	}
	return defaultLokiURL
}

// WaitForTempoReady waits until Tempo API is accessible (health check after HA failover)
// This is critical after control plane restarts where CoreDNS may be temporarily unstable
func (f *Framework) WaitForTempoReady(timeout time.Duration) error {
	tempoURL := getTempoURL()
	healthURL := fmt.Sprintf("%s/ready", tempoURL)

	tempoDebugf("  [Tempo] Checking if Tempo is ready (timeout: %v)\n", timeout)
	tempoDebugf("  [Tempo] Health check URL: %s\n", healthURL)

	err := wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true,
		func(ctx context.Context) (done bool, err error) {
			// Build the request using the polling context so the HTTP call can be canceled
			// when the overall wait times out.
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			if err != nil {
				tempoDebugf("  [Tempo] Failed to build health request (will retry): %v\n", err)
				return false, nil
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				tempoDebugf("  [Tempo] Health check failed (will retry): %v\n", err)
				return false, nil
			}
			// Close the body immediately before returning to avoid leaking file descriptors
			// across each Poll iteration.
			closeResp := func() {
				if resp.Body != nil {
					_ = resp.Body.Close()
				}
			}

			if resp.StatusCode == http.StatusOK {
				closeResp()
				tempoDebugf("  [Tempo] ✓ Tempo is ready (status: %d)\n", resp.StatusCode)
				return true, nil
			}

			tempoDebugf("  [Tempo] Tempo not ready yet (status: %d, will retry)\n", resp.StatusCode)
			closeResp()
			return false, nil
		})

	if err != nil {
		return fmt.Errorf("tempo failed to become ready within %v: %w", timeout, err)
	}

	return nil
}

// SearchTracesInTempo queries Tempo API to search for traces matching criteria
// serviceName: filter by service.name tag (e.g., "okg-controller-manager")
// minDuration: minimum trace duration in milliseconds (0 = no filter)
// limit: maximum number of traces to return (default 20)
// startNano/endNano: optional Unix nano time window (0 = not set)
func (f *Framework) SearchTracesInTempo(ctx context.Context, serviceName string, minDuration int, limit int, filters map[string]string, startSec, endSec int64) (*TempoSearchResult, error) {
	if limit == 0 {
		limit = 20
	}

	// Build search query
	tempoURL := getTempoURL()
	tagsParam := buildTempoTagsParam(serviceName, filters)
	query := fmt.Sprintf("%s/api/search?tags=%s&minDuration=%dms&limit=%d",
		tempoURL, tagsParam, minDuration, limit)
	if startSec > 0 {
		query += fmt.Sprintf("&start=%d", startSec)
	}
	if endSec > 0 {
		query += fmt.Sprintf("&end=%d", endSec)
	}

	tempoDebugf("  [Tempo] Querying: %s\n", query)
	tempoDebugf("  [Tempo] Using Tempo URL: %s (from env: %s)\n", tempoURL, os.Getenv("TEMPO_URL"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build tempo query request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Tempo at %s: %w", query, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tempo API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result TempoSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode Tempo response: %w", err)
	}

	return &result, nil
}

func buildTempoTagsParam(serviceName string, filters map[string]string) string {
	tagPairs := []string{fmt.Sprintf("service.name=%s", serviceName)}
	for k, v := range filters {
		if k == "" || v == "" {
			continue
		}
		tagPairs = append(tagPairs, fmt.Sprintf("%s=%s", k, v))
	}
	// Join with spaces as required by Tempo logfmt tags, then URL-escape
	return url.QueryEscape(strings.Join(tagPairs, " "))
}

var invalidArtifactChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeTestName(name string) string {
	sanitized := invalidArtifactChars.ReplaceAllString(name, "_")
	sanitized = strings.Trim(sanitized, "_")
	if sanitized == "" {
		sanitized = "test"
	}
	if len(sanitized) > 200 {
		sanitized = sanitized[:200]
	}
	return sanitized
}

// GetTraceByID retrieves a complete trace from Tempo by its ID
// Uses official OpenTelemetry protobuf format (NOT Jaeger JSON)
func (f *Framework) GetTraceByID(traceID string) (*tracepb.TracesData, error) {
	tempoBase := getTempoURL()

	// Tempo 2.3.1 returns OpenTelemetry protobuf format
	// Request it explicitly for more efficient parsing
	tempoDebugf("  [Tempo] Querying trace %s via /api/traces (protobuf)\n", traceID)
	url := fmt.Sprintf("%s/api/traces/%s", tempoBase, traceID)

	trace, err := f.fetchAndParseOTelProtobuf(url, traceID)
	if err == nil && trace != nil {
		tempoDebugf("  [Tempo] ✓ Found %d spans\n", countTraceSpans(trace))
		return trace, nil
	}

	if err != nil {
		fmt.Printf("  [Tempo] ✗ Failed to fetch trace: %v\n", err)
	} else if trace != nil {
		fmt.Printf("  [Tempo] ✗ Trace found but contains 0 spans\n")
	}

	return nil, fmt.Errorf("trace %s not found or empty: %w", traceID, err)
}

// fetchAndParseOTelProtobuf fetches trace from Tempo using OpenTelemetry protobuf format
// This is the OFFICIAL format returned by Tempo (NOT Jaeger JSON)
func (f *Framework) fetchAndParseOTelProtobuf(url string, traceID string) (*tracepb.TracesData, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// Request protobuf format (more efficient than JSON)
	req.Header.Set("Accept", "application/protobuf")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get trace: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	tempoDebugf("  [Tempo] Response: status=%d, size=%d bytes, Content-Type=%s\n",
		resp.StatusCode, len(bodyBytes), resp.Header.Get("Content-Type"))

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("trace %s not found (404)", traceID)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tempo API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse OpenTelemetry protobuf format using official library
	tracesData := &tracepb.TracesData{}
	if err := proto.Unmarshal(bodyBytes, tracesData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OTLP protobuf: %w", err)
	}
	tempoDebugf("  [Tempo] ✓ Parsed OTLP protobuf: %d resource spans, %d spans total\n",
		len(tracesData.ResourceSpans), countTraceSpans(tracesData))

	return tracesData, nil
}

// WaitForTraceInTempo polls Tempo until at least one trace matching criteria is found
// Returns the first matching trace ID or error after timeout
func (f *Framework) WaitForTraceInTempo(serviceName string, operationName string, timeout time.Duration, filters map[string]string) (string, error) {
	var foundTraceID string
	var lastErr error
	attemptCount := 0

	tempoDebugf("  [Tempo] Starting trace search (timeout: %v)\n", timeout)
	tempoDebugf("  [Tempo] Target service: %s, operation: %s\n", serviceName, operationName)
	tempoDebugf("  [Tempo] Tempo URL from env: %s\n", os.Getenv("TEMPO_URL"))

	// CRITICAL: After HA failover, DNS/network may be unstable
	// Wait for Tempo to be accessible before attempting trace searches
	tempoDebugf("  [Tempo] Pre-flight: Waiting for Tempo to be accessible...\n")
	if err := f.WaitForTempoReady(30 * time.Second); err != nil {
		return "", fmt.Errorf("tempo not accessible before trace search: %w", err)
	}
	tempoDebugf("  [Tempo] Pre-flight: ✓ Tempo is accessible, starting trace search\n")

	err := wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, timeout, true,
		func(ctx context.Context) (done bool, err error) {
			attemptCount++
			tempoDebugf("  [Tempo] Attempt #%d\n", attemptCount)

			result, err := f.SearchTracesInTempo(ctx, serviceName, 0, 50, filters, 0, 0)
			if err != nil {
				lastErr = err
				tempoDebugf("  [Tempo] Search error (will retry): %v\n", err)
				tempoDebugf("  [Tempo] Error type: %T\n", err)
				return false, nil
			}

			traces := result.Traces
			if len(traces) == 0 {
				tempoDebugf("  [Tempo] No traces found yet (attempt #%d, will retry)...\n", attemptCount)
				return false, nil
			}

			tempoDebugf("  [Tempo] Found %d traces on attempt #%d\n", len(traces), attemptCount)

			// If operationName specified, filter by root trace name
			if operationName != "" {
				for _, t := range traces {
					if t.RootTraceName == operationName {
						foundTraceID = t.TraceID
						return true, nil
					}
				}
				tempoDebugf("  [Tempo] Found %d traces, but none match operation '%s' (will retry)...\n",
					len(traces), operationName)
				return false, nil
			}

			// No operation filter, return first trace
			foundTraceID = traces[0].TraceID
			return true, nil
		})

	if err != nil {
		if lastErr != nil {
			return "", fmt.Errorf("timeout waiting for trace in Tempo (last error: %v): %w", lastErr, err)
		}
		return "", fmt.Errorf("timeout waiting for trace in Tempo after %d attempts: %w", attemptCount, err)
	}

	return foundTraceID, nil
}

// QueryLogsInLoki queries Loki for logs matching a LogQL query
// query: LogQL query string (e.g., `{namespace="e2e-test"} |= "trace_id"`)
// start: start time for query range (Unix nano)
// end: end time for query range (Unix nano)
func (f *Framework) QueryLogsInLoki(query string, start, end int64) ([]byte, error) {
	lokiURL := getLokiURL()
	u, err := url.Parse(fmt.Sprintf("%s/loki/api/v1/query_range", lokiURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Loki URL: %w", err)
	}

	values := url.Values{}
	values.Set("query", query)
	values.Set("start", strconv.FormatInt(start, 10))
	values.Set("end", strconv.FormatInt(end, 10))
	u.RawQuery = values.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to query Loki: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki API returned status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// FindSpanByName searches for a span with given operation name in a trace
func (f *Framework) FindSpanByName(trace *tracepb.TracesData, operationName string) *SpanView {
	for _, span := range ExtractSpanViews(trace) {
		if span.OperationName == operationName {
			return span
		}
	}
	return nil
}

// VerifySpanAttributes checks if a span has expected attributes
func (f *Framework) VerifySpanAttributes(span *SpanView, expectedAttrs map[string]interface{}) error {
	if span == nil {
		return fmt.Errorf("span is nil")
	}
	for key, expectedVal := range expectedAttrs {
		actualVal, ok := span.Tags[key]
		if !ok {
			return fmt.Errorf("span missing expected attribute %s", key)
		}
		if actualVal != expectedVal {
			return fmt.Errorf("span attribute %s: expected %v, got %v", key, expectedVal, actualVal)
		}
	}
	return nil
}

func (f *Framework) BeforeSuit() error {
	err := f.client.CreateNamespace()
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			err = f.client.DeleteGameServerSet(f.GameServerSetName())
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (f *Framework) AfterSuit() error {
	return f.client.DeleteNamespace()
}

func (f *Framework) AfterEach() error {
	defer func() {
		f.gssName = client.DefaultGameServerSetName
	}()
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			err = f.client.DeleteGameServerSet(f.GameServerSetName())
			if err != nil && !apierrors.IsNotFound(err) {
				{
					return false, err
				}
			}

			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: f.GameServerSetName(),
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(podList.Items) != 0 {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) DeployGameServerSet() (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet(f.GameServerSetName())
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) DeployGameServerSetWithNetwork(networkType string, conf []gamekruiseiov1alpha1.NetworkConfParams, ports []corev1.ContainerPort) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet(f.GameServerSetName())
	gss.Spec.Network = &gamekruiseiov1alpha1.Network{
		NetworkType: networkType,
		NetworkConf: conf,
	}
	if len(ports) > 0 && len(gss.Spec.GameServerTemplate.Spec.Containers) > 0 {
		gss.Spec.GameServerTemplate.Spec.Containers[0].Ports = append(gss.Spec.GameServerTemplate.Spec.Containers[0].Ports, ports...)
	}
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) GetGameServer(name string) (*gamekruiseiov1alpha1.GameServer, error) {
	return f.client.GetGameServer(name)
}

func (f *Framework) DeployGameServerSetWithReclaimPolicy(reclaimPolicy gamekruiseiov1alpha1.GameServerReclaimPolicy) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet(f.GameServerSetName())
	gss.Spec.GameServerTemplate.ReclaimPolicy = reclaimPolicy
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) DeployGssWithServiceQualities() (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet(f.GameServerSetName())
	up := intstr.FromInt(20)
	dp := intstr.FromInt(10)
	sqs := []gamekruiseiov1alpha1.ServiceQuality{
		{
			Name:          "healthy",
			ContainerName: client.GameContainerName,
			Probe: corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/sh", "-c", "ls /"},
					},
				},
			},
			Permanent: false,
			ServiceQualityAction: []gamekruiseiov1alpha1.ServiceQualityAction{
				{
					State: true,
					GameServerSpec: gamekruiseiov1alpha1.GameServerSpec{
						UpdatePriority: &up,
					},
				},
				{
					State: false,
					GameServerSpec: gamekruiseiov1alpha1.GameServerSpec{
						DeletionPriority: &dp,
					},
				},
			},
		},
	}
	gss.Spec.ServiceQualities = sqs
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) GameServerScale(gss *gamekruiseiov1alpha1.GameServerSet, desireNum int, reserveGsId *intstr.IntOrString) (*gamekruiseiov1alpha1.GameServerSet, error) {
	// TODO: change patch type
	newReserves := gss.Spec.ReserveGameServerIds
	if reserveGsId != nil {
		newReserves = append(newReserves, *reserveGsId)
	}

	numJson := map[string]interface{}{"spec": map[string]interface{}{"replicas": desireNum, "reserveGameServerIds": newReserves}}
	data, err := json.Marshal(numJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(f.GameServerSetName(), data)
}

// PatchGssSpec applies a generic spec-only merge patch using a map of fields.
func (f *Framework) PatchGssSpec(specFields map[string]interface{}) (*gamekruiseiov1alpha1.GameServerSet, error) {
	patch := map[string]interface{}{"spec": specFields}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(f.GameServerSetName(), data)
}

// PatchGss applies a generic merge patch to the GameServerSet resource.
func (f *Framework) PatchGss(patch map[string]interface{}) (*gamekruiseiov1alpha1.GameServerSet, error) {
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(f.GameServerSetName(), data)
}

// PatchGameServerSpec applies a merge patch to GameServer.spec using provided fields.
func (f *Framework) PatchGameServerSpec(gsName string, specFields map[string]interface{}) (*gamekruiseiov1alpha1.GameServer, error) {
	patch := map[string]interface{}{"spec": specFields}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
}
func (f *Framework) ImageUpdate(gss *gamekruiseiov1alpha1.GameServerSet, name, image string) (*gamekruiseiov1alpha1.GameServerSet, error) {
	var newContainers []corev1.Container
	for _, c := range gss.Spec.GameServerTemplate.Spec.Containers {
		newContainer := c
		if c.Name == name {
			newContainer.Image = image
		}
		newContainers = append(newContainers, newContainer)
	}

	conJson := map[string]interface{}{"spec": map[string]interface{}{"gameServerTemplate": map[string]interface{}{"spec": map[string]interface{}{"containers": newContainers}}}}
	data, err := json.Marshal(conJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(f.GameServerSetName(), data)
}

func (f *Framework) MarkGameServerOpsState(gsName string, opsState string) (*gamekruiseiov1alpha1.GameServer, error) {
	osJson := map[string]interface{}{"spec": map[string]string{"opsState": opsState}}
	data, err := json.Marshal(osJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
}

func (f *Framework) ChangeGameServerDeletionPriority(gsName string, deletionPriority string) (*gamekruiseiov1alpha1.GameServer, error) {
	dpJson := map[string]interface{}{"spec": map[string]string{"deletionPriority": deletionPriority}}
	data, err := json.Marshal(dpJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
}

func (f *Framework) WaitForGsCreated(gss *gamekruiseiov1alpha1.GameServerSet) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gssName := gss.GetName()
			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(podList.Items) != int(*gss.Spec.Replicas) {
				return false, nil
			}
			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(gsList.Items) != int(*gss.Spec.Replicas) {
				return false, nil
			}

			return true, nil
		})
}

func (f *Framework) WaitForUpdated(gss *gamekruiseiov1alpha1.GameServerSet, name, image string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gssName := gss.GetName()
			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			updated := 0

			for _, pod := range podList.Items {
				for _, c := range pod.Status.ContainerStatuses {
					if name == c.Name && strings.Contains(c.Image, image) {
						updated++
						break
					}
				}
			}

			if gss.Spec.UpdateStrategy.RollingUpdate == nil || gss.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
				if int32(updated) != *gss.Spec.Replicas {
					return false, nil
				}
			} else {
				if int32(updated) != *gss.Spec.Replicas-*gss.Spec.UpdateStrategy.RollingUpdate.Partition {
					return false, nil
				}
			}
			return true, nil
		})
}

// WaitForGssCounts waits until both Pod and GameServer counts equal desired.
func (f *Framework) WaitForGssCounts(gss *gamekruiseiov1alpha1.GameServerSet, desired int) error {
	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, nil
			}
			if len(podList.Items) != desired {
				return false, nil
			}
			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, nil
			}
			if len(gsList.Items) != desired {
				return false, nil
			}
			return true, nil
		})
}

// WaitForGssObservedGeneration waits until status.observedGeneration reaches at least the targetGeneration.
func (f *Framework) WaitForGssObservedGeneration(targetGeneration int64) error {
	return f.WaitForGss(func(g *gamekruiseiov1alpha1.GameServerSet) (bool, error) {
		if g.Generation < targetGeneration {
			return false, nil
		}
		if g.Status.ObservedGeneration < targetGeneration {
			return false, nil
		}
		return true, nil
	})
}

// WaitForReplicasConverge waits until gss.Spec.Replicas equals desired and
// on timeout returns a detailed snapshot of last observed state to aid debugging.
func (f *Framework) WaitForReplicasConverge(gss *gamekruiseiov1alpha1.GameServerSet, desired int) error {
	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()
	var lastSpecReplicas int
	var lastStatusReplicas int
	var lastCurrentReplicas int
	var lastPodCount, lastGsCount int
	var lastPodOrdinals []int
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			// Fetch latest GSS
			g, err := f.client.GetGameServerSet(f.GameServerSetName())
			if err != nil {
				return false, nil
			}
			if g.Spec.Replicas != nil {
				lastSpecReplicas = int(*g.Spec.Replicas)
			}
			lastStatusReplicas = int(g.Status.Replicas)
			lastCurrentReplicas = int(g.Status.CurrentReplicas)
			// Fetch Pods and GameServers for context
			if podList, err := f.client.GetPodList(labelSelector); err == nil {
				lastPodCount = len(podList.Items)
				lastPodOrdinals = util.GetIndexListFromPodList(podList.Items)
			}
			if gsList, err := f.client.GetGameServerList(labelSelector); err == nil {
				lastGsCount = len(gsList.Items)
			}
			return lastSpecReplicas == desired, nil
		})
	if err != nil {
		return fmt.Errorf(
			"WaitForReplicasConverge timeout: want=%d spec=%d status.replicas=%d status.current=%d pods=%d gs=%d ordinals=%v",
			desired, lastSpecReplicas, lastStatusReplicas, lastCurrentReplicas, lastPodCount, lastGsCount, lastPodOrdinals,
		)
	}
	return nil
}

// GetGameServerSet fetches the current GameServerSet from cluster.
func (f *Framework) GetGameServerSet() (*gamekruiseiov1alpha1.GameServerSet, error) {
	return f.client.GetGameServerSet(f.GameServerSetName())
}

// WaitForGssReplicas waits until .spec.replicas equals the desired value.
// WaitForGss fetches GSS periodically and evaluates a predicate until it returns true.
func (f *Framework) WaitForGss(predicate func(*gamekruiseiov1alpha1.GameServerSet) (bool, error)) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gss, err := f.client.GetGameServerSet(f.GameServerSetName())
			if err != nil {
				return false, err
			}
			return predicate(gss)
		})
}

func (f *Framework) ExpectGssCorrect(gss *gamekruiseiov1alpha1.GameServerSet, expectIndex []int) error {
	// First wait until the number of objects matches the expected replica count
	// (avoids relying on spec.replicas which may be delayed).
	desired := len(expectIndex)
	if err := f.WaitForGssCounts(gss, desired); err != nil {
		return err
	}

	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()

	// capture last observed snapshot for better error messages
	var lastPodIndexes []int
	var lastPodCount, lastGsCount int

	// Then poll only for whether the ordinals match (while ensuring the counts remain consistent).
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, nil
			}
			lastPodCount = len(podList.Items)
			lastPodIndexes = util.GetIndexListFromPodList(podList.Items)

			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, nil
			}
			lastGsCount = len(gsList.Items)

			// ensure the counts still match the expected value
			if lastPodCount != desired || lastGsCount != desired {
				return false, nil
			}
			if util.IsSliceEqual(expectIndex, lastPodIndexes) {
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("ExpectGssCorrect timeout: desired=%d expectedOrdinals=%v lastPodOrdinals=%v lastPodCount=%d lastGsCount=%d", desired, expectIndex, lastPodIndexes, lastPodCount, lastGsCount)
	}
	return nil
}

func (f *Framework) WaitForGsOpsStateUpdate(gsName string, opsState string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentOpsState := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOpsStateKey]
			if currentOpsState == opsState {
				return true, nil
			}
			return false, nil
		})
}

// WaitForGsSpecOpsState waits until GameServer.spec.opsState reaches the desired value.
func (f *Framework) WaitForGsSpecOpsState(gsName string, opsState string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, err
			}
			if string(gs.Spec.OpsState) == opsState {
				return true, nil
			}
			return false, nil
		})
}

// WaitForPodOpsStateOrDeleted waits until the pod label opsState equals the target or the pod is deleted.
func (f *Framework) WaitForPodOpsStateOrDeleted(gsName string, opsState string) error {
	var lastPodOpsState, lastPodPhase, lastPodUID, lastGsSpecOps string
	var lastPodErr, lastGsErr error
	var lastDeletionTimestamp string
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			if pod, err0 := f.client.GetPod(gsName); err0 == nil {
				lastPodOpsState = pod.GetLabels()[gamekruiseiov1alpha1.GameServerOpsStateKey]
				lastPodPhase = string(pod.Status.Phase)
				lastPodUID = string(pod.UID)
				if pod.DeletionTimestamp != nil {
					lastDeletionTimestamp = pod.DeletionTimestamp.Format(time.RFC3339)
				} else {
					lastDeletionTimestamp = "<nil>"
				}
				if lastPodOpsState == opsState {
					return true, nil
				}
			} else {
				if apierrors.IsNotFound(err0) {
					return true, nil
				}
				lastPodErr = err0
			}

			if gs, err1 := f.client.GetGameServer(gsName); err1 == nil {
				lastGsSpecOps = string(gs.Spec.OpsState)
			} else {
				lastGsErr = err1
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("WaitForPodOpsStateOrDeleted timeout: wantOps=%s lastPodOps=%s lastPodPhase=%s lastPodUID=%s lastPodDeletionTs=%s lastGsSpecOps=%s podErr=%v gsErr=%v",
			opsState, lastPodOpsState, lastPodPhase, lastPodUID, lastDeletionTimestamp, lastGsSpecOps, lastPodErr, lastGsErr)
	}
	return nil
}

func (f *Framework) WaitForGsDeletionPriorityUpdated(gsName string, deletionPriority string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentPriority := pod.GetLabels()[gamekruiseiov1alpha1.GameServerDeletePriorityKey]
			if currentPriority == deletionPriority {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) DeletePodDirectly(index int) error {
	var uid types.UID
	podName := f.GameServerSetName() + "-" + strconv.Itoa(index)

	// get
	if err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {

			pod, err := f.client.GetPod(podName)
			if err != nil {
				return false, err
			}
			uid = pod.UID
			return true, nil
		}); err != nil {
		return err
	}

	// delete
	if err := f.client.DeletePod(podName); err != nil {
		return err
	}

	// check
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(podName)
			if err != nil {
				return false, err
			}
			if pod.UID == uid {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) WaitForPodDeleted(podName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			_, err = f.client.GetPod(podName)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) ExpectGsCorrect(gsName, opsState, dp, up string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 5*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, nil
			}

			if gs.Status.DeletionPriority.String() != dp || gs.Status.UpdatePriority.String() != up || string(gs.Spec.OpsState) != opsState {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) WaitForGsUpdatePriorityUpdated(gsName string, updatePriority string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentPriority := pod.GetLabels()[gamekruiseiov1alpha1.GameServerUpdatePriorityKey]
			if currentPriority == updatePriority {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) WaitForGsDesiredNetworkState(gsName string, desired gamekruiseiov1alpha1.NetworkState) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, err
			}
			if gs.Status.NetworkStatus.DesiredNetworkState == desired {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) WaitForNodePortServiceSelector(gsName string, disabled bool) error {
	const (
		activeKey   = "statefulset.kubernetes.io/pod-name"
		disabledKey = "game.kruise.io/svc-selector-disabled"
	)
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			svc, err := f.client.GetService(gsName)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			selector := svc.Spec.Selector
			_, hasActive := selector[activeKey]
			disabledVal, hasDisabled := selector[disabledKey]
			if disabled {
				if !hasActive && hasDisabled && disabledVal == gsName {
					return true, nil
				}
				return false, nil
			}
			if hasActive && selector[activeKey] == gsName && !hasDisabled {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) WaitForGsNetworkDisabled(gsName string, want bool) error {
	var lastPodValue, lastSpecValue string
	var lastSpecNil bool
	var lastPodErr, lastGsErr error
	wantStr := strconv.FormatBool(want)

	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			specMatched := false
			if gs, err0 := f.client.GetGameServer(gsName); err0 == nil {
				lastSpecValue = strconv.FormatBool(ptr.Deref(gs.Spec.NetworkDisabled, false))
				lastSpecNil = gs.Spec.NetworkDisabled == nil
				lastGsErr = nil
				if lastSpecValue == wantStr {
					specMatched = true
				}
			} else {
				lastGsErr = err0
			}

			if pod, err1 := f.client.GetPod(gsName); err1 == nil {
				lastPodValue = pod.GetLabels()[gamekruiseiov1alpha1.GameServerNetworkDisabled]
				lastPodErr = nil
				if specMatched && lastPodValue == wantStr {
					return true, nil
				}
			} else {
				lastPodErr = err1
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("WaitForGsNetworkDisabled timeout: want=%s lastPod=%s lastSpec=%s lastSpecNil=%t podErr=%v gsErr=%v",
			wantStr, lastPodValue, lastSpecValue, lastSpecNil, lastPodErr, lastGsErr)
	}
	return nil
}
