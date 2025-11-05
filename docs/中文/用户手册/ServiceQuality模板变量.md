# ServiceQuality 模板变量功能

## 概述

ServiceQuality 功能现已支持在 `ServiceQualityAction` 中使用模板变量，可以根据探测返回的结果动态设置 GameServer 的字段值。

## 功能特性

### 支持的字段

模板变量可以在以下字段中使用：

1. **OpsState** - 游戏服务器运维状态
2. **UpdatePriority** - 更新优先级（支持数值模板，自动验证）
3. **DeletionPriority** - 删除优先级（支持数值模板，自动验证）
4. **Annotations** - 注解（所有值均支持模板）
5. **Labels** - 标签（所有值均支持模板）

### 可用的模板变量

在模板中可以使用以下变量：

- `{{.Result}}` - 探测返回的结果（Message内容）

### 支持的模板函数

- `eq` - 相等比较: `{{if eq .Result "value"}}...{{end}}`
- `ne` - 不相等比较: `{{if ne .Result "0"}}...{{end}}`
- `lt` - 小于比较: `{{if lt .Result "50"}}...{{end}}`
- `le` - 小于等于比较: `{{if le .Result "100"}}...{{end}}`
- `gt` - 大于比较: `{{if gt .Result "80"}}...{{end}}`
- `ge` - 大于等于比较: `{{if ge .Result "10"}}...{{end}}`

## 使用示例

### 示例1：根据玩家数量设置标签和注解

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
        - state: true  # 探测成功时执行
          annotations:
            # 直接使用探测结果
            player-count: "{{.Result}}"
            # 组合使用探测结果
            info: "players-{{.Result}}"
          labels:
            # 直接使用结果作为标签值
            current-players: "{{.Result}}"
```

### 示例2：根据玩家数量动态设置 OpsState

```yaml
serviceQualities:
  - name: player-status
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]
    serviceQualityAction:
      - state: true
        # 根据玩家数量设置 OpsState
        # 无玩家时标记为等待删除，否则保持正常
        opsState: "{{if eq .Result \"0\"}}WaitToBeDeleted{{else}}None{{end}}"
        annotations:
          player-count: "{{.Result}}"
        labels:
          # 无玩家时标记为 empty，否则标记为 active
          status: "{{if eq .Result \"0\"}}empty{{else}}active{{end}}"
```

### 示例3：根据服务器负载设置多个字段

```yaml
serviceQualities:
  - name: server-load
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/check_load.sh"]
    serviceQualityAction:
      - state: true
        # 根据负载设置运维状态
        opsState: "{{if gt .Result \"90\"}}Maintaining{{else}}None{{end}}"
        annotations:
          load-percentage: "{{.Result}}"
          load-status: "{{if gt .Result \"90\"}}critical{{else if gt .Result \"70\"}}warning{{else}}normal{{end}}"
        labels:
          # 负载分级
          load-level: "{{if gt .Result \"80\"}}high{{else if gt .Result \"50\"}}medium{{else}}low{{end}}"
```

### 示例4：健康检查返回多状态

```yaml
serviceQualities:
  - name: health-check
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/health_check.sh"]
    serviceQualityAction:
      - state: true
        result: "healthy"  # 当返回 "healthy" 时
        opsState: "None"
        labels:
          health: "healthy"
      
      - state: true
        result: "degraded"  # 当返回 "degraded" 时
        opsState: "Maintaining"
        labels:
          health: "degraded"
        annotations:
          degraded-info: "performance-issue"
      
      - state: false  # 探测失败时
        opsState: "WaitToBeDeleted"
        labels:
          health: "unhealthy"
```

### 示例5：复杂条件判断

```yaml
serviceQualities:
  - name: resource-monitor
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "/monitor.sh"]  # 返回资源使用百分比
    serviceQualityAction:
      - state: true
        annotations:
          # 根据不同阈值设置状态
          status: "{{if ge .Result \"95\"}}critical{{else if ge .Result \"80\"}}warning{{else if ge .Result \"60\"}}notice{{else}}ok{{end}}"
          # 记录原始值
          usage-percent: "{{.Result}}"
        labels:
          # 简单的高低负载标签
          load: "{{if gt .Result \"75\"}}high{{else}}normal{{end}}"
```

### 示例6：动态设置 UpdatePriority（玩家数量）

```yaml
serviceQualities:
  - name: player-count-priority
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]  # 返回当前玩家数量
    serviceQualityAction:
      - state: true
        # 直接使用玩家数量作为更新优先级：玩家多的服务器优先级高，不容易被更新
        updatePriority: "{{.Result}}"
        annotations:
          player-count: "{{.Result}}"
```

**说明**：模板渲染后会自动转换为数字类型。如果转换失败，会记录 Kubernetes Event 告警。

### 示例7：动态设置 DeletionPriority（基于负载）

```yaml
serviceQualities:
  - name: deletion-priority-by-load
    containerName: game-server
    permanent: false
    exec:
      command: ["/bin/sh", "-c", "cat /tmp/player_count"]
    serviceQualityAction:
      - state: true
        # 无玩家时优先删除（优先级=1），有玩家时低优先级（优先级=100）
        deletionPriority: "{{if eq .Result \"0\"}}1{{else}}100{{end}}"
        annotations:
          deletion-reason: "{{if eq .Result \"0\"}}empty-server{{else}}active-server{{end}}"
```

**说明**：
- 优先级值会自动验证和转换为整数
- 如果模板渲染结果无法解析为有效数字，会触发 `InvalidUpdatePriority` 或 `InvalidDeletionPriority` Event
- 可以在事件中查看详细的错误信息

## 实现原理

1. **探测执行**：PodProbeMarker 执行探测脚本，将结果写入 Pod Condition 的 Message 字段
2. **结果提取**：控制器从 Pod Condition 中提取 Message 作为探测结果
3. **模板渲染**：使用 Go template 引擎渲染模板字符串，将 `{{.Result}}` 等变量替换为实际值
4. **字段应用**：将渲染后的值设置到 GameServer 的对应字段

## 注意事项

1. **模板语法错误**：如果模板语法错误，会使用原始值（包含模板语法的字符串）
2. **数值比较**：比较函数会尝试将字符串解析为数值进行比较，解析失败则按字符串比较
3. **无模板标记**：如果字段值不包含 `{{`，则直接使用原始值，无性能损耗
4. **Permanent 字段**：设置为 true 时，action 只执行一次；false 时每次探测结果变化都会执行
5. **优先级字段验证**：
   - UpdatePriority 和 DeletionPriority 的模板渲染结果必须能转换为数字或字符串
   - 如果验证失败，会记录 Kubernetes Event 事件，类型为 `InvalidUpdatePriority` 或 `InvalidDeletionPriority`
   - 优先级不会被设置，保持原有值
   - 可通过 `kubectl describe gs <name>` 查看 Event 事件获取错误详情

## 与现有功能的兼容性

此功能完全向后兼容，原有的静态配置方式仍然有效：

```yaml
serviceQualityAction:
  - state: true
    opsState: "None"  # 静态值，无模板
    annotations:
      key: "value"     # 静态值，无模板
```

## 常见用例

### 1. 玩家计数器
根据当前玩家数量自动标记服务器状态，方便扩缩容决策。

### 2. 资源监控
根据 CPU/内存使用率自动设置运维状态，避免高负载服务器被更新。

### 3. 健康分级
根据健康检查结果设置不同级别的标签，便于监控和告警。

### 4. 自动下线
无玩家的服务器自动标记为等待删除，优化资源利用。

## 调试建议

1. 使用 `kubectl describe gs <name>` 查看应用后的 annotations 和 labels
2. 检查 Pod Condition 的 Message 字段确认探测返回值
3. 简单场景先使用 `{{.Result}}` 直接输出，确认值正确后再添加条件判断
