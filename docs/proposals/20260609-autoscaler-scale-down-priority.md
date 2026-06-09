---
title: External Scaler Scale-Down Priority Control via Threshold Parameter
authors:
  - "@ChrisLiu"
reviewers:
  - "@openkruise/kruise-game-maintainers"
creation-date: 2026-06-09
last-updated: 2026-06-09
status: provisional
---

# External Scaler Scale-Down Priority Control via Threshold Parameter

## Table of Contents

- [External Scaler Scale-Down Priority Control via Threshold Parameter](#external-scaler-scale-down-priority-control-via-threshold-parameter)
  - [Table of Contents](#table-of-contents)
  - [Glossary](#glossary)
  - [Summary](#summary)
  - [Motivation](#motivation)
    - [Goals](#goals)
    - [Non-Goals/Future Work](#non-goalsfuture-work)
  - [Proposal](#proposal)
    - [User Stories](#user-stories)
      - [Story 1: Large-Scale Cluster with WTBD Accumulation](#story-1-large-scale-cluster-with-wtbd-accumulation)
      - [Story 2: Small-Scale Cluster with Normal WTBD Flow](#story-2-small-scale-cluster-with-normal-wtbd-flow)
    - [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
      - [Current Architecture and Problem Analysis](#current-architecture-and-problem-analysis)
      - [Proposed Solution: Threshold-Based Priority Control](#proposed-solution-threshold-based-priority-control)
      - [Threshold Parameter Design](#threshold-parameter-design)
      - [Pseudocode](#pseudocode)
      - [Behavior Walkthrough](#behavior-walkthrough)
    - [Risks and Mitigations](#risks-and-mitigations)
  - [Upgrade Strategy](#upgrade-strategy)
  - [Implementation History](#implementation-history)

## Glossary

- **GSS**: GameServerSet, the workload type in OpenKruiseGame.
- **GS**: GameServer, the individual game server instance managed by GSS.
- **WTBD**: WaitToBeDeleted, the opsState of a GS that indicates it is ready to be removed during scale-down.
- **External Scaler**: The gRPC-based scaler in OKG that integrates with KEDA to provide game-server-aware autoscaling decisions.
- **minAvailable**: A ScaledObject metadata parameter specifying the minimum number of GS with opsState=None.
- **KEDA**: Kubernetes Event-driven Autoscaling, an external autoscaler framework used by OKG.

## Summary

The OKG External Scaler currently uses a mutually exclusive two-branch logic in `GetMetrics`: when the number of None-opState game servers is below `minAvailable`, it prioritizes scale-up and returns early without considering scale-down; when None count is sufficient, it proceeds to scale down WTBD game servers.

In large-scale clusters, this design causes a problem: the None count frequently drops below `minAvailable` due to slow pod startup, game server allocation, or frequent state changes. As a result, the scaler consistently enters the scale-up branch, the overall `spec.Replicas` keeps growing, and WTBD game servers are never deleted.

This proposal introduces a configurable threshold parameter (`wtdbThreshold`) to the ScaledObject metadata. When the WTBD count (or WTBD ratio) exceeds this threshold, the scaler prioritizes scale-down to clear the WTBD backlog before considering scale-up. When WTBD is below the threshold, the existing scale-up-first behavior is preserved for backward compatibility.

## Motivation

### Goals

- Fix the issue where WTBD game servers accumulate and are never deleted in large-scale clusters.
- Prevent unbounded growth of `spec.Replicas` caused by the mutually exclusive scale-up/scale-down logic.
- Provide users with a configurable parameter to control the trade-off between scale-up priority and scale-down urgency.
- Maintain backward compatibility: existing clusters without the new parameter should behave identically to the current implementation.

### Non-Goals/Future Work

- Changing the GameServerSet controller's pod deletion priority logic. The controller's behavior (WTBD first, then None, then Allocated, etc.) remains unchanged.
- Modifying KEDA or HPA behavior. The proposal only changes the `desiredReplicas` calculation within the OKG External Scaler.
- Implementing a fully dynamic or predictive autoscaling algorithm. This proposal is a pragmatic, incremental improvement.
- Supporting complex multi-trigger scaling scenarios beyond the external scaler trigger.

## Proposal

### User Stories

#### Story 1: Large-Scale Cluster with WTBD Accumulation

A game operator runs a cluster with 500 game servers. The `minAvailable` is set to 10. During off-peak hours, many game servers finish their sessions and transition to WTBD via ServiceQuality auto-detection. However, the None count also drops because active servers are being allocated to players.

**Current behavior**: The scaler always sees `noneNum < minAvailable`, keeps scaling up, and `spec.Replicas` grows from 500 to 600+. The 50+ WTBD servers are never deleted.

**Expected behavior**: The operator sets `wtdbThreshold: "10"`. When WTBD count exceeds 10, the scaler returns a `desiredReplicas` lower than the current pod count, triggering scale-down. The controller deletes the WTBD servers. Once WTBD drops below 10, the scaler resumes normal scale-up-first behavior.

#### Story 2: Small-Scale Cluster with Normal WTBD Flow

A game operator runs a cluster with 10 game servers and `minAvailable: 3`. WTBD servers appear occasionally (1-2 at a time) and are deleted quickly during normal scaling cycles.

**Current behavior**: Works correctly. The None count is usually sufficient, and the scaler reaches the scale-down branch to delete WTBD servers.

**Expected behavior**: The operator does not set `wtdbThreshold` (or sets it high). The behavior remains identical to the current implementation. No disruption.

### Implementation Details/Notes/Constraints

#### Current Architecture and Problem Analysis

The OKG External Scaler's responsibility is limited to computing a `desiredReplicas` value and returning it to KEDA via the `GetMetrics` gRPC call. KEDA then adjusts `gss.Spec.Replicas` accordingly. The GameServerSet controller reconciles the actual state:
- If `spec.Replicas > current pods` -> scale up (create new pods).
- If `spec.Replicas < current pods` -> scale down (delete pods by priority: WTBD first).

The current `GetMetrics` logic (in `pkg/externalscaler/externalscaler.go`):

```
if noneNum < minNum:
    desireReplicas = spec.Replicas + (minNum - noneNum)  // Scale up
    return desireReplicas  // EARLY RETURN: scale-down branch never reached

// Scale down WTBD
desireReplicas = spec.Replicas - numWaitToBeDeleted
return desireReplicas
```

The problem: the scale-up branch has an early return. When `noneNum < minNum` (frequent in large-scale clusters), the scaler always returns a value >= current pod count, so KEDA never triggers scale-down. The `spec.Replicas` grows monotonically.

#### Proposed Solution: Threshold-Based Priority Control

Introduce a new ScaledObject metadata parameter `wtdbThreshold` that controls when scale-down takes priority over scale-up.

**Decision logic**:

```
1. Count totalNum, noneNum, numWaitToBeDeleted (same as current)
2. Check if WTBD exceeds threshold
3. If WTBD exceeds threshold:
   - Prioritize scale-down: return totalNum - numWaitToBeDeleted
   - This ensures all WTBD pods are deleted by the controller
4. If WTBD does not exceed threshold:
   - Use existing logic (scale-up-first when noneNum < minNum)
```

The key insight: when WTBD exceeds the threshold, the scaler deliberately allows the None count to temporarily drop below `minAvailable` in order to clear the WTBD backlog. In the next scaling cycle, once WTBD is cleared, the scaler will naturally enter the scale-up branch and restore the None count.

#### Threshold Parameter Design

The `wtdbThreshold` parameter supports two formats:

**Absolute value** (integer >= 1):

```yaml
metadata:
  wtdbThreshold: "10"   # Trigger scale-down priority when WTBD count > 10
```

- Simple and intuitive for small to medium clusters.
- The scaler triggers scale-down when `numWaitToBeDeleted > threshold`.

**Percentage** (float between 0 and 1, expressed as string):

```yaml
metadata:
  wtdbThreshold: "0.1"  # Trigger scale-down priority when WTBD ratio > 10%
```

- The WTBD ratio is calculated as: `numWaitToBeDeleted / totalNum`.
- More suitable for clusters of varying sizes. 10 WTBD in a 20-pod cluster (50%) is very different from 10 WTBD in a 1000-pod cluster (1%).
- The denominator uses `totalNum` (current actual pod count), not `spec.Replicas`, because `totalNum` reflects the real cluster state while `spec.Replicas` may lag behind KEDA's reconciliation.

**Default behavior** (parameter not set):

- When `wtdbThreshold` is not specified, the scaler behaves identically to the current implementation (scale-up always takes priority when `noneNum < minNum`).
- This ensures full backward compatibility.

The parameter is parsed using the same approach as `minAvailable` (via `strconv.ParseFloat`), distinguishing between integer and percentage values.

#### Pseudocode

```go
func (e *ExternalScaler) GetMetrics(ctx context.Context, metricRequest *GetMetricsRequest) (*GetMetricsResponse, error) {
    // ... existing code: get GSS, list pods, count totalNum, noneNum, numWaitToBeDeleted ...

    // --- NEW: WTBD threshold check ---
    thresholdStr := metricRequest.ScaledObjectRef.GetScalerMetadata()[WTBDThresholdKey]
    if thresholdStr != "" {
        threshold, isPercentage := parseThreshold(thresholdStr)
        exceeded := false
        if isPercentage {
            // Percentage mode: compare ratio
            if totalNum > 0 {
                ratio := float64(numWaitToBeDeleted) / float64(totalNum)
                exceeded = ratio > threshold
            }
        } else {
            // Absolute mode: compare count
            exceeded = numWaitToBeDeleted > int(threshold)
        }

        if exceeded {
            // Prioritize scale-down: delete all WTBD first
            desireReplicas := totalNum - numWaitToBeDeleted
            // Apply minAvailable floor to avoid scaling down too aggressively
            minNum, _ := handleMinNum(totalNum, noneNum, minNumStr)
            if minNum > 0 && desireReplicas < minNum {
                desireReplicas = minNum
            }
            return desireReplicas
        }
    }
    // --- END NEW ---

    // ... existing logic: scale-up-first when noneNum < minNum, then scale-down WTBD ...
}
```

Note on the `minAvailable` floor: when prioritizing scale-down, we still apply `minNum` as a lower bound to prevent the cluster from scaling down below the minimum required capacity in a single step. This means not all WTBD pods may be deleted in one cycle, but the replicas count will decrease, and subsequent cycles will continue to reduce it until WTBD is cleared.

#### Behavior Walkthrough

Consider: `minAvailable: 3`, `wtdbThreshold: "5"`, cluster with 100 pods (noneNum=2, WTBD=20).

| Cycle | totalNum | noneNum | WTBD | WTBD > threshold? | desireReplicas | Action |
|-------|----------|---------|------|-------------------|----------------|--------|
| T0 | 100 | 2 | 20 | Yes (20 > 5) | max(100-20, 3) = 80 | Scale down: delete 20 WTBD |
| T1 | 80 | 5 | 0 | No (0 <= 5) | Check noneNum >= minNum: 5 >= 3, scale-down branch: 80-0 = 80 | No action |
| T2 | 80 | 5 | 0 | No | 80 | Stable |

If WTBD does not exceed threshold (e.g., WTBD=3):

| Cycle | totalNum | noneNum | WTBD | WTBD > threshold? | desireReplicas | Action |
|-------|----------|---------|------|-------------------|----------------|--------|
| T0 | 100 | 2 | 3 | No (3 <= 5) | noneNum(2) < minNum(3): 100+3-2 = 101 | Scale up |
| T1 | 101 | 2 | 3 | No | 101+3-2 = 102 | Scale up (existing behavior) |

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Aggressive scale-down may temporarily drop None count below `minAvailable` | Apply `minNum` as a floor in the scale-down branch. The cluster will recover None count in subsequent cycles. |
| Users may misconfigure the threshold | Provide clear documentation with recommended values for different cluster sizes. Default behavior (no threshold set) preserves current behavior. |
| Percentage threshold may cause instability near the boundary | The threshold comparison uses `>` (strict greater than), so values exactly at the boundary do not trigger scale-down. HPA/KEDA stabilization windows also dampen oscillation. |
| Scale-down and scale-up oscillation when WTBD hovers around the threshold | KEDA's built-in `stabilizationWindowSeconds` and `cooldownPeriod` naturally mitigate this. The threshold is a coarse-grained control, not a real-time PID controller. |
| Backward compatibility breakage | When `wtdbThreshold` is not set, the scaler behaves identically to the current implementation. No breaking changes. |

## Upgrade Strategy

- No changes to CRD or API types. The `wtdbThreshold` parameter is added to the ScaledObject metadata, which is already a free-form `map[string]string`.
- Existing ScaledObject configurations work without any modification.
- Users who want to enable the new behavior add `wtdbThreshold` to their ScaledObject metadata.
- No controller restart or rolling update is required. The External Scaler reads the parameter on each `GetMetrics` call.

## Implementation History

- [ ] 06/09/2026: Proposed idea in design document
- [ ] TBD: Community review and feedback
- [ ] TBD: Implementation and testing
- [ ] TBD: Open PR

---

# 中文版

# External Scaler 缩容优先级控制：基于阈值参数的方案

## 术语表

- **GSS**: GameServerSet，OpenKruiseGame 中的工作负载类型。
- **GS**: GameServer，由 GSS 管理的单个游戏服实例。
- **WTBD**: WaitToBeDeleted，表示游戏服处于等待被删除的运维状态。
- **External Scaler**: OKG 中基于 gRPC 的扩缩器，与 KEDA 集成，提供感知游戏服状态的自动扩缩决策。
- **minAvailable**: ScaledObject 元数据参数，指定 opsState 为 None 的游戏服的最小数量。
- **KEDA**: Kubernetes Event-driven Autoscaling，OKG 使用的外部自动扩缩框架。

## 概述

OKG External Scaler 当前在 `GetMetrics` 中使用互斥的双分支逻辑：当 None 状态的游戏服数量低于 `minAvailable` 时，优先扩容并直接返回，不考虑缩容；当 None 数量充足时，才执行缩容 WTBD 游戏服的逻辑。

在大规模集群中，这个设计会导致问题：由于 Pod 启动慢、游戏服被分配、状态频繁变化等原因，None 数量经常低于 `minAvailable`。Scaler 持续进入扩容分支，`spec.Replicas` 不断上涨，WTBD 游戏服永远无法被删除。

本方案引入一个可配置的阈值参数 `wtdbThreshold` 到 ScaledObject 元数据中。当 WTBD 数量（或比例）超过此阈值时，Scaler 优先缩容以清理 WTBD 积压；当 WTBD 低于阈值时，保留现有的扩容优先行为，确保向后兼容。

## 动机

### 目标

- 修复大规模集群中 WTBD 游戏服堆积无法删除的问题。
- 防止 `spec.Replicas` 因互斥的扩缩逻辑而无限增长。
- 为用户提供可配置参数，控制扩容优先与缩容紧急度之间的权衡。
- 保持向后兼容：未设置新参数的集群行为与现有实现完全一致。

### 非目标/未来工作

- 不改变 GameServerSet Controller 的 Pod 删除优先级逻辑。Controller 的行为（WTBD 优先，然后 None，然后 Allocated 等）保持不变。
- 不修改 KEDA 或 HPA 的行为。本方案仅修改 OKG External Scaler 中 `desiredReplicas` 的计算逻辑。
- 不实现完全动态或预测性的自动扩缩算法。本方案是务实的增量改进。
- 不支持 External Scaler trigger 之外的复杂多触发器伸缩场景。

## 方案设计

### 用户场景

#### 场景 1：大规模集群的 WTBD 堆积

游戏运营商运行一个 500 个游戏服的集群，`minAvailable` 设为 10。低峰时段，大量游戏服结束会话后通过 ServiceQuality 自动检测转为 WTBD 状态。但 None 数量也在下降，因为活跃的游戏服正在被分配给玩家。

**当前行为**：Scaler 始终看到 `noneNum < minAvailable`，持续扩容，`spec.Replicas` 从 500 涨到 600+。50+ 个 WTBD 游戏服永远无法被删除。

**预期行为**：运营商设置 `wtdbThreshold: "10"`。当 WTBD 数量超过 10 时，Scaler 返回一个小于当前 Pod 数量的 `desiredReplicas`，触发缩容。Controller 删除 WTBD 游戏服。一旦 WTBD 降至 10 以下，Scaler 恢复正常的扩容优先行为。

#### 场景 2：小规模集群的正常 WTBD 流转

游戏运营商运行一个 10 个游戏服的集群，`minAvailable: 3`。WTBD 游戏服偶尔出现（每次 1-2 个），在正常扩缩周期中很快被删除。

**当前行为**：正常工作。None 数量通常充足，Scaler 能走到缩容分支删除 WTBD 游戏服。

**预期行为**：运营商不设置 `wtdbThreshold`（或设置较高值）。行为与现有实现完全一致。无影响。

### 实现细节/注意事项/约束

#### 当前架构与问题分析

OKG External Scaler 的职责仅限于计算 `desiredReplicas` 值并通过 `GetMetrics` gRPC 调用返回给 KEDA。KEDA 据此调整 `gss.Spec.Replicas`。GameServerSet Controller 负责实际的调和：
- `spec.Replicas > 当前 Pod 数` -> 扩容（创建新 Pod）。
- `spec.Replicas < 当前 Pod 数` -> 缩容（按优先级删除 Pod：WTBD 优先）。

当前 `GetMetrics` 逻辑（位于 `pkg/externalscaler/externalscaler.go`）：

```
if noneNum < minNum:
    desireReplicas = spec.Replicas + (minNum - noneNum)  // 扩容
    return desireReplicas  // 直接返回：缩容分支永远不会执行

// 缩容 WTBD
desireReplicas = spec.Replicas - numWaitToBeDeleted
return desireReplicas
```

问题：扩容分支有直接返回（early return）。当 `noneNum < minNum`（大规模集群中经常出现）时，Scaler 始终返回一个 >= 当前 Pod 数量的值，KEDA 永远不会触发缩容。`spec.Replicas` 单调递增。

#### 方案：基于阈值的优先级控制

在 ScaledObject 元数据中新增 `wtdbThreshold` 参数，控制缩容何时优先于扩容。

**决策逻辑**：

```
1. 统计 totalNum、noneNum、numWaitToBeDeleted（与现有逻辑相同）
2. 判断 WTBD 是否超过阈值
3. 如果 WTBD 超过阈值：
   - 优先缩容：返回 totalNum - numWaitToBeDeleted
   - 确保 Controller 能删除所有 WTBD Pod
4. 如果 WTBD 未超过阈值：
   - 使用现有逻辑（noneNum < minNum 时扩容优先）
```

核心思路：当 WTBD 超过阈值时，Scaler 有意允许 None 数量暂时低于 `minAvailable`，以清理 WTBD 积压。在下一轮扩缩周期中，一旦 WTBD 被清理，Scaler 自然会进入扩容分支补充 None 数量。

#### 阈值参数设计

`wtdbThreshold` 参数支持两种格式：

**绝对值**（整数 >= 1）：

```yaml
metadata:
  wtdbThreshold: "10"   # 当 WTBD 数量 > 10 时触发缩容优先
```

- 对小到中型集群简单直观。
- 当 `numWaitToBeDeleted > threshold` 时触发缩容。

**百分比**（0 到 1 之间的浮点数，以字符串形式表示）：

```yaml
metadata:
  wtdbThreshold: "0.1"  # 当 WTBD 占比 > 10% 时触发缩容优先
```

- WTBD 占比计算公式：`numWaitToBeDeleted / totalNum`。
- 更适合不同规模的集群。20 个 Pod 的集群中 10 个 WTBD（50%）与 1000 个 Pod 的集群中 10 个 WTBD（1%）严重程度完全不同。
- 分母使用 `totalNum`（当前实际 Pod 数），而非 `spec.Replicas`，因为 `totalNum` 反映真实的集群状态，而 `spec.Replicas` 可能因 KEDA 尚未调和而滞后。

**默认行为**（参数未设置时）：

- 当未指定 `wtdbThreshold` 时，Scaler 行为与现有实现完全一致（当 `noneNum < minNum` 时始终扩容优先）。
- 确保完全向后兼容。

参数解析方式与 `minAvailable` 相同（通过 `strconv.ParseFloat`），区分整数和百分比值。

#### 伪代码

```go
func (e *ExternalScaler) GetMetrics(ctx context.Context, metricRequest *GetMetricsRequest) (*GetMetricsResponse, error) {
    // ... 现有代码：获取 GSS、列出 Pod、统计 totalNum、noneNum、numWaitToBeDeleted ...

    // --- 新增：WTBD 阈值检查 ---
    thresholdStr := metricRequest.ScaledObjectRef.GetScalerMetadata()[WTBDThresholdKey]
    if thresholdStr != "" {
        threshold, isPercentage := parseThreshold(thresholdStr)
        exceeded := false
        if isPercentage {
            // 百分比模式：比较占比
            if totalNum > 0 {
                ratio := float64(numWaitToBeDeleted) / float64(totalNum)
                exceeded = ratio > threshold
            }
        } else {
            // 绝对值模式：比较数量
            exceeded = numWaitToBeDeleted > int(threshold)
        }

        if exceeded {
            // 优先缩容：先删除所有 WTBD
            desireReplicas := totalNum - numWaitToBeDeleted
            // 应用 minAvailable 下限，避免缩容过于激进
            minNum, _ := handleMinNum(totalNum, noneNum, minNumStr)
            if minNum > 0 && desireReplicas < minNum {
                desireReplicas = minNum
            }
            return desireReplicas
        }
    }
    // --- 新增结束 ---

    // ... 现有逻辑：noneNum < minNum 时扩容优先，然后缩容 WTBD ...
}
```

关于 `minAvailable` 下限的说明：在优先缩容时，仍然将 `minNum` 作为下限，防止集群在单次缩容中降至最低要求容量以下。这意味着可能无法在一个周期内删除所有 WTBD Pod，但副本数会下降，后续周期会继续减少，直到 WTBD 被清理完毕。

#### 行为推演

场景：`minAvailable: 3`，`wtdbThreshold: "5"`，集群 100 个 Pod（noneNum=2，WTBD=20）。

| 轮次 | totalNum | noneNum | WTBD | WTBD > 阈值? | desireReplicas | 动作 |
|------|----------|---------|------|-------------|----------------|------|
| T0 | 100 | 2 | 20 | 是 (20 > 5) | max(100-20, 3) = 80 | 缩容：删除 20 个 WTBD |
| T1 | 80 | 5 | 0 | 否 (0 <= 5) | noneNum(5) >= minNum(3)，缩容分支：80-0 = 80 | 无动作 |
| T2 | 80 | 5 | 0 | 否 | 80 | 稳定 |

WTBD 未超过阈值的场景（如 WTBD=3）：

| 轮次 | totalNum | noneNum | WTBD | WTBD > 阈值? | desireReplicas | 动作 |
|------|----------|---------|------|-------------|----------------|------|
| T0 | 100 | 2 | 3 | 否 (3 <= 5) | noneNum(2) < minNum(3)：100+3-2 = 101 | 扩容 |
| T1 | 101 | 2 | 3 | 否 | 101+3-2 = 102 | 扩容（现有行为） |

### 风险与缓解措施

| 风险 | 缓解措施 |
|------|----------|
| 激进的缩容可能导致 None 数量暂时低于 `minAvailable` | 在缩容分支中应用 `minNum` 作为下限。集群会在后续周期中恢复 None 数量。 |
| 用户可能错误配置阈值 | 提供清晰的文档，包含不同集群规模的推荐值。默认行为（不设置阈值）保留当前行为。 |
| 百分比阈值在边界附近可能导致不稳定 | 阈值比较使用 `>`（严格大于），恰好在边界上的值不会触发缩容。HPA/KEDA 的稳定窗口也会抑制振荡。 |
| WTBD 在阈值附近波动时可能导致扩缩振荡 | KEDA 内置的 `stabilizationWindowSeconds` 和 `cooldownPeriod` 自然会缓解此问题。阈值是粗粒度的控制，不是实时的 PID 控制器。 |
| 向后兼容性破坏 | 未设置 `wtdbThreshold` 时，Scaler 行为与现有实现完全一致。无破坏性变更。 |

## 升级策略

- 无需修改 CRD 或 API 类型。`wtdbThreshold` 参数添加到 ScaledObject 元数据中，该字段已经是自由格式的 `map[string]string`。
- 现有的 ScaledObject 配置无需任何修改即可继续使用。
- 需要启用新行为的用户只需在 ScaledObject 元数据中添加 `wtdbThreshold`。
- 不需要 Controller 重启或滚动更新。External Scaler 在每次 `GetMetrics` 调用时读取该参数。

## 实施历史

- [ ] 2026/06/09：提出设计方案
- [ ] 待定：社区评审与反馈
- [ ] 待定：实现与测试
- [ ] 待定：提交 PR
