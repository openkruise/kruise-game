#!/bin/bash
# Copyright 2025 The Kruise Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

# E2E Test Artifact Analyzer
# This script helps analyze E2E test artifacts collected by the new infrastructure
# Usage: ./scripts/analyze-artifacts.sh <artifact-dir>

ARTIFACT_DIR="${1:-./e2e-artifacts}"

if [ ! -d "$ARTIFACT_DIR" ]; then
    echo "Error: Artifact directory not found: $ARTIFACT_DIR"
    echo "Usage: $0 <artifact-dir>"
    exit 1
fi

echo "=== E2E Test Artifact Analyzer ==="
echo "Analyzing artifacts in: $ARTIFACT_DIR"
echo ""

# Function to print section headers
section() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# 1. Show overall structure
section "1. Artifact Directory Structure"
if command -v tree >/dev/null 2>&1; then
    tree -L 3 "$ARTIFACT_DIR"
else
    find "$ARTIFACT_DIR" -type d | sort | sed 's|[^/]*/| |g'
fi

# 2. List all tests
section "2. Test Summary"
TESTS_DIR="$ARTIFACT_DIR/tests"
if [ -d "$TESTS_DIR" ]; then
    TEST_COUNT=$(find "$TESTS_DIR" -mindepth 1 -maxdepth 1 -type d | wc -l)
    echo "Total tests found: $TEST_COUNT"
    echo ""
    
    for test_dir in "$TESTS_DIR"/*; do
        if [ -d "$test_dir" ]; then
            test_name=$(basename "$test_dir")
            info_file="$test_dir/test-info.json"
            
            echo "Test: $test_name"
            if [ -f "$info_file" ]; then
                status=$(jq -r '.status' "$info_file" 2>/dev/null || echo "unknown")
                start=$(jq -r '.startTime' "$info_file" 2>/dev/null || echo "unknown")
                end=$(jq -r '.endTime' "$info_file" 2>/dev/null || echo "unknown")
                
                echo "  Status: $status"
                echo "  Start:  $start"
                echo "  End:    $end"
                
                if [ "$status" = "failed" ]; then
                    failure_msg=$(jq -r '.failureMessage' "$info_file" 2>/dev/null || echo "")
                    if [ -n "$failure_msg" ] && [ "$failure_msg" != "null" ]; then
                        echo "  Failure: $failure_msg"
                    fi
                fi
            else
                echo "  ⚠ No test-info.json found"
            fi
            echo ""
        fi
    done
else
    echo "⚠ No tests directory found at $TESTS_DIR"
fi

# 3. Analyze failed tests
section "3. Failed Tests Analysis"
FAILED_TESTS=0
if [ -d "$TESTS_DIR" ]; then
    for test_dir in "$TESTS_DIR"/*; do
        if [ -d "$test_dir" ]; then
            info_file="$test_dir/test-info.json"
            if [ -f "$info_file" ]; then
                status=$(jq -r '.status' "$info_file" 2>/dev/null || echo "unknown")
                if [ "$status" = "failed" ]; then
                    FAILED_TESTS=$((FAILED_TESTS + 1))
                    test_name=$(basename "$test_dir")
                    echo "❌ $test_name"
                    
                    # Show failure message
                    failure_msg=$(jq -r '.failureMessage' "$info_file" 2>/dev/null || echo "")
                    if [ -n "$failure_msg" ] && [ "$failure_msg" != "null" ]; then
                        echo "   → $failure_msg"
                    fi
                    
                    # Check for traces
                    traces_file="$test_dir/observability/tempo-traces.json"
                    if [ -f "$traces_file" ]; then
                        trace_count=$(jq '. | length' "$traces_file" 2>/dev/null || echo "0")
                        echo "   → Found $trace_count traces"
                    fi
                    
                    # Check for pods
                    pods_file="$test_dir/k8s-resources/pods.yaml"
                    if [ -f "$pods_file" ]; then
                        pod_count=$(grep -c "^kind: Pod$" "$pods_file" 2>/dev/null || echo "0")
                        echo "   → Found $pod_count pods"
                    fi
                    echo ""
                fi
            fi
        fi
    done
fi

if [ $FAILED_TESTS -eq 0 ]; then
    echo "✅ No failed tests found"
fi

# 4. Check tracing data
section "4. Tracing Data Analysis"
TRACES_FOUND=0
if [ -d "$TESTS_DIR" ]; then
    for test_dir in "$TESTS_DIR"/*; do
        traces_file="$test_dir/observability/tempo-traces.json"
        if [ -f "$traces_file" ]; then
            test_name=$(basename "$test_dir")
            trace_count=$(jq '. | length' "$traces_file" 2>/dev/null || echo "0")
            
            if [ "$trace_count" -gt 0 ]; then
                TRACES_FOUND=$((TRACES_FOUND + trace_count))
                echo "Test: $test_name"
                echo "  Traces: $trace_count"
                
                # Show trace IDs
                echo "  Trace IDs:"
                jq -r '.[].traceID' "$traces_file" 2>/dev/null | head -n 5 | while read -r trace_id; do
                    echo "    - $trace_id"
                done
                
                # Show span counts
                echo "  Span counts:"
                jq -r '.[] | "\(.traceID): \(.spans | length) spans"' "$traces_file" 2>/dev/null | head -n 5
                echo ""
            fi
        fi
    done
fi

if [ $TRACES_FOUND -eq 0 ]; then
    echo "⚠ No traces found in any test"
else
    echo "Total traces collected: $TRACES_FOUND"
fi

# 5. Check logs for errors
section "5. Error Analysis in Logs"
if [ -d "$TESTS_DIR" ]; then
    for test_dir in "$TESTS_DIR"/*; do
        logs_dir="$test_dir/controller-logs"
        if [ -d "$logs_dir" ]; then
            test_name=$(basename "$test_dir")
            
            # Count errors in logs
            error_count=0
            for log_file in "$logs_dir"/*.log; do
                if [ -f "$log_file" ]; then
                    count=$(grep -ci "error\|fatal\|panic" "$log_file" 2>/dev/null || echo "0")
                    error_count=$((error_count + count))
                fi
            done
            
            if [ $error_count -gt 0 ]; then
                echo "Test: $test_name"
                echo "  Errors/Warnings: $error_count"
                
                # Show sample errors
                echo "  Sample errors:"
                for log_file in "$logs_dir"/*.log; do
                    if [ -f "$log_file" ]; then
                        grep -i "error\|fatal\|panic" "$log_file" 2>/dev/null | head -n 3 | while read -r line; do
                            echo "    $(basename "$log_file"): $line"
                        done
                    fi
                done
                echo ""
            fi
        fi
    done
fi

# 6. Infrastructure logs summary
section "6. Infrastructure Logs Summary"
INFRA_DIR="$ARTIFACT_DIR/infrastructure"
if [ -d "$INFRA_DIR" ]; then
    echo "Audit logs:"
    if [ -d "$INFRA_DIR/audit-logs" ]; then
        audit_size=$(du -sh "$INFRA_DIR/audit-logs" 2>/dev/null | cut -f1)
        echo "  Size: $audit_size"
    else
        echo "  ⚠ Not found"
    fi
    
    echo ""
    echo "Observability logs:"
    if [ -d "$INFRA_DIR/observability-logs" ]; then
        for log_file in "$INFRA_DIR/observability-logs"/*.log; do
            if [ -f "$log_file" ]; then
                log_name=$(basename "$log_file")
                log_size=$(du -sh "$log_file" 2>/dev/null | cut -f1)
                line_count=$(wc -l < "$log_file" 2>/dev/null || echo "0")
                echo "  $log_name: $log_size ($line_count lines)"
            fi
        done
    else
        echo "  ⚠ Not found"
    fi
else
    echo "⚠ Infrastructure directory not found"
fi

# 7. Generate recommendations
section "7. Recommendations"
if [ $FAILED_TESTS -gt 0 ]; then
    echo "→ Review failed test details in section 3"
    echo "→ Check controller logs in tests/*/controller-logs/"
    echo "→ Examine K8s resources in tests/*/k8s-resources/"
fi

if [ $TRACES_FOUND -eq 0 ]; then
    echo "→ No traces found - check OTel Collector configuration"
    echo "→ Review observability infrastructure logs"
    echo "→ Verify OTLP exporter endpoint in controller"
fi

echo ""
echo "=== Analysis Complete ==="
