# ServiceQuality Template Variables

## Overview

ServiceQuality now supports using template variables in `ServiceQualityAction` to dynamically set GameServer field values based on probe results.

## Features

### Supported Fields

Template variables can be used in the following fields:

1. **OpsState** - Game server operational state
2. **UpdatePriority** - Update priority (supports numeric templates with automatic validation)
3. **DeletionPriority** - Deletion priority (supports numeric templates with automatic validation)
4. **Annotations** - Annotations (all values support templates)
5. **Labels** - Labels (all values support templates)

### Available Template Variables

The following variables can be used in templates:

- `{{.Result}}` - Probe result (Message content)

### Supported Template Functions

- `eq` - Equal comparison: `{{if eq .Result "value"}}...{{end}}`
- `ne` - Not equal comparison: `{{if ne .Result "0"}}...{{end}}`
- `lt` - Less than comparison: `{{if lt .Result "50"}}...{{end}}`
- `le` - Less than or equal comparison: `{{if le .Result "100"}}...{{end}}`
- `gt` - Greater than comparison: `{{if gt .Result "80"}}...{{end}}`
- `ge` - Greater than or equal comparison: `{{if ge .Result "10"}}...{{end}}`

## Usage Examples

### Example 1: Set Labels and Annotations Based on Player Count

```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: minecraft
spec:
  serviceQualities:
    - name: player-count
      containerName: game-server
      permanent: false
      exec:
        command: ["/bin/sh", "-c", "cat /tmp/player_count"]
      serviceQualityAction:
        - state: true  # Execute when probe succeeds
          annotations:
            # Use probe result directly
            player-count: "{{.Result}}"
            # Combine with probe result
            info: "players-{{.Result}}"
          labels:
            # Use result as label value
            current-players: "{{.Result}}"
```

### Example 2: Dynamically Set OpsState Based on Player Count

```yaml
serviceQualities:
  - name: player-status
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]
    serviceQualityAction:
      - state: true
        # Set OpsState based on player count
        # Mark for deletion when empty, otherwise keep normal
        opsState: "{{if eq .Result \"0\"}}WaitToBeDeleted{{else}}None{{end}}"
        annotations:
          player-count: "{{.Result}}"
        labels:
          # Mark as empty when no players, otherwise active
          status: "{{if eq .Result \"0\"}}empty{{else}}active{{end}}"
```

### Example 3: Set Multiple Fields Based on Server Load

```yaml
serviceQualities:
  - name: server-load
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/check_load.sh"]
    serviceQualityAction:
      - state: true
        # Set operational state based on load
        opsState: "{{if gt .Result \"90\"}}Maintaining{{else}}None{{end}}"
        annotations:
          load-percentage: "{{.Result}}"
          load-status: "{{if gt .Result \"90\"}}critical{{else if gt .Result \"70\"}}warning{{else}}normal{{end}}"
        labels:
          # Load classification
          load-level: "{{if gt .Result \"80\"}}high{{else if gt .Result \"50\"}}medium{{else}}low{{end}}"
```

### Example 4: Health Check with Multiple States

```yaml
serviceQualities:
  - name: health-check
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/health_check.sh"]
    serviceQualityAction:
      - state: true
        result: "healthy"  # When returns "healthy"
        opsState: "None"
        labels:
          health: "healthy"
      
      - state: true
        result: "degraded"  # When returns "degraded"
        opsState: "Maintaining"
        labels:
          health: "degraded"
        annotations:
          degraded-info: "performance-issue"
      
      - state: false  # When probe fails
        opsState: "WaitToBeDeleted"
        labels:
          health: "unhealthy"
```

### Example 5: Complex Conditional Logic

```yaml
serviceQualities:
  - name: resource-monitor
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/monitor.sh"]  # Returns resource usage percentage
    serviceQualityAction:
      - state: true
        annotations:
          # Set status based on different thresholds
          status: "{{if ge .Result \"95\"}}critical{{else if ge .Result \"80\"}}warning{{else if ge .Result \"60\"}}notice{{else}}ok{{end}}"
          # Record raw value
          usage-percent: "{{.Result}}"
        labels:
          # Simple high/low load label
          load: "{{if gt .Result \"75\"}}high{{else}}normal{{end}}"
```

### Example 6: Dynamically Set UpdatePriority (Player Count)

```yaml
serviceQualities:
  - name: player-count-priority
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]  # Returns current player count
    serviceQualityAction:
      - state: true
        # Use player count directly as update priority: servers with more players have higher priority and are less likely to be updated
        updatePriority: "{{.Result}}"
        annotations:
          player-count: "{{.Result}}"
```

**Note**: Template rendering automatically converts to numeric type. If conversion fails, a Kubernetes Event warning is recorded.

### Example 7: Dynamically Set DeletionPriority (Based on Load)

```yaml
serviceQualities:
  - name: deletion-priority-by-load
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]
    serviceQualityAction:
      - state: true
        # Prioritize deletion when empty (priority=1), low priority when has players (priority=100)
        deletionPriority: "{{if eq .Result \"0\"}}1{{else}}100{{end}}"
        annotations:
          deletion-reason: "{{if eq .Result \"0\"}}empty-server{{else}}active-server{{end}}"
```

**Note**:
- Priority values are automatically validated and converted to integers
- If template rendering result cannot be parsed as a valid number, an `InvalidUpdatePriority` or `InvalidDeletionPriority` Event is triggered
- View detailed error messages in events

## Implementation Details

1. **Probe Execution**: PodProbeMarker executes the probe script and writes results to Pod Condition's Message field
2. **Result Extraction**: Controller extracts Message from Pod Condition as probe result
3. **Template Rendering**: Uses Go template engine to render template strings, replacing variables like `{{.Result}}` with actual values
4. **Field Application**: Sets rendered values to corresponding GameServer fields

## Important Notes

1. **Template Syntax Errors**: If template syntax is invalid, the original value (containing template syntax) is used
2. **Numeric Comparison**: Comparison functions attempt to parse strings as numbers for comparison; if parsing fails, string comparison is used
3. **No Template Marker**: If field value doesn't contain `{{`, the original value is used directly with no performance overhead
4. **Permanent Field**: When set to true, action executes only once; when false, executes every time probe result changes
5. **Priority Field Validation**:
   - Template rendering results for UpdatePriority and DeletionPriority must be convertible to numbers
   - If validation fails, a Kubernetes Event is recorded with type `InvalidUpdatePriority` or `InvalidDeletionPriority`
   - Priority is not set and retains its original value
   - Use `kubectl describe gs <name>` to view Event details for error information

## Backward Compatibility

This feature is fully backward compatible. Existing static configurations remain valid:

```yaml
serviceQualityAction:
  - state: true
    opsState: "None"  # Static value, no template
    annotations:
      key: "value"     # Static value, no template
```

## Common Use Cases

### 1. Player Counter
Automatically mark server status based on current player count to facilitate scaling decisions.

### 2. Resource Monitoring
Automatically set operational state based on CPU/memory usage to prevent high-load servers from being updated.

### 3. Health Classification
Set different levels of labels based on health check results for monitoring and alerting.

### 4. Auto Offline
Automatically mark servers with no players for deletion to optimize resource utilization.

## Debugging Tips

1. Use `kubectl describe gs <name>` to view applied annotations and labels
2. Check Pod Condition's Message field to confirm probe return value
3. For simple scenarios, use `{{.Result}}` directly first, then add conditional logic after confirming the value is correct
