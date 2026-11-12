# 基于 ServiceQuality 完成条件的滚动更新

## 1. 概要

本提案允许 GameServerSet **使用 ServiceQuality 结果控制滚动更新的放量**。OpenKruise 继续执行 Pod 更新；OpenKruiseGame（OKG）在每个 Pod 更新后观察业务条件，并据此推进 Advanced StatefulSet partition。更新期间持续复查所有已更新 Pod，业务条件退化时停止新增放量。

第一期只在现有 `rollingUpdate` 下增加一个可选字段 **`completionPolicy`**：

```yaml
spec:
  updateStrategy:
    rollingUpdate:
      maxUnavailable: 2
      podUpdatePolicy: InPlaceIfPossible
      completionPolicy:  # 新增字段
        serviceQualities:
          - name: version-a
            state: true
            result: offline
          - name: version-b
            state: true
            result: ready
```

- **不配置 `completionPolicy`：行为与现有版本完全一致**；
- 配置后：GSS PodTemplate 变更发起更新，ASTS 执行更新，OKG 根据 ServiceQuality 的当前结果和业务观察规则控制后续放量；
- 继续复用 `partition` 和 `maxUnavailable`，不增加 `batchSize` 或第二个并发字段。启用业务门禁后，**`maxUnavailable` 同时作为 OKG 放量窗口的容量**；ASTS 原有的 Pod 不可用数量限制保持不变。

## 2. 背景

部分游戏服通过主备进程或 A/B 容器切换业务版本，切换过程可能不修改镜像。即使备用进程启动失败，或者主进程已经停止，Pod 仍可能保持 Ready。因此，**仅依靠 Pod Ready 和 ASTS `maxUnavailable` 无法判断业务切换是否完成**。

readinessProbe 也不是该场景的通用解法。部分 OKG 网络插件需要 Pod Ready 后才能产生 Network Ready，而业务进程又必须在 Network Ready 后启动；继续让 Pod Ready 依赖业务进程会形成循环依赖。

ServiceQuality 已经能够提供业务探测结果，本提案只补充“使用该结果控制滚动更新继续放量”的能力。

## 3. 目标与非目标

目标：

- 使用业务信号控制更新进度，并在观察到业务条件退化后停止新增放量；
- 支持镜像与非镜像模板变更、原地升级和重建更新；
- 支持 Controller 重启恢复、新版本接管、回退及扩缩容；
- Revision、Pod 更新状态或业务条件缺失、冲突时停止放量；明确接受观察窗口无法证明原地升级后一定产生过新探测结果的局限；
- 不配置新字段时完全向后兼容。

第一期不提供：

- GSS 首次创建或普通扩容的并发节流；
- 严格批次屏障；
- OR、嵌套布尔表达式等复杂条件组合；
- 自动修改业务版本进行回滚；
- 流量切分和金丝雀分析；
- 运行期业务可用性 SLA；
- 进度超时后的自动处置。

## 4. API 设计

```go
type RollingUpdateStatefulSetStrategy struct {
    // Existing fields omitted.
    CompletionPolicy *UpdateCompletionPolicy `json:"completionPolicy,omitempty"`
}

type UpdateCompletionPolicy struct {
    // Optional override; omitted means probe-derived timing.
    // +kubebuilder:validation:Minimum=1
    ObservationWindowSeconds *int32 `json:"observationWindowSeconds,omitempty"`
    // +listType=map
    // +listMapKey=name
    ServiceQualities []ServiceQualityCompletionCondition `json:"serviceQualities"`
}

type ServiceQualityCompletionCondition struct {
    Name   string `json:"name"`
    State  bool   `json:"state"`
    Result string `json:"result,omitempty"`
}
```

`state + result` 与现有 `ServiceQualityAction` 的匹配方式一致：

- `name` 引用 `spec.serviceQualities` 中的探针；
- `state` 匹配探针对应 PodCondition 的 True 或 False；
- `result` 可选，精确匹配探针结果；
- PodCondition 为 Unknown 时不匹配。

`serviceQualities` 中的条件采用 **AND 语义**：全部满足且通过第 6.3 节的业务观察后，才认为单个 GameServer 完成本轮更新。第一期不增加 `matchPolicy`；出现明确的 OR 需求后，可增加默认值为 `All` 的匹配策略，保持现有配置兼容。

**业务脚本不需要返回版本号或发布标识**，可以始终返回固定的 `ready`。被引用的 ServiceQuality 应持续反映当前业务状态；例如切换失败后继续报告不满足条件，OKG 才能在后续调谐中止损。探针周期内发生且结束的短暂异常可能无法被采到。

该字段按 `name` 作为列表键，数组顺序不参与语义比较；仅调整 YAML 顺序不视为完成条件变化。

### 4.1 校验

- `completionPolicy` 只能配置在 `rollingUpdate` 下；
- `serviceQualities` 至少包含一个元素；
- `observationWindowSeconds` 可省略；配置时必须为正整数，不限制其大于自动计算窗口。活动更新中不允许单独修改，随新 PodTemplate 一起修改时开启新一轮更新；
- 每个 `name` 必须非空，在 `completionPolicy.serviceQualities` 中不得重复，并且在 `spec.serviceQualities` 中唯一存在；
- 被引用的 ServiceQuality 必须指定 `containerName`，且对应容器存在于 PodTemplate；
- 每个条件的 `state` 必填，`result` 可选；
- 无需在 `result` 中配置发布标识；Admission 只能校验引用和字段格式，不能证明业务探针确实反映切换状态；
- OKG 使用与 ASTS 相同的公开 `IntOrString` 解析方式：默认值 1、以期望副本数为百分比基数、向下取整且最小值为 1；ASTS 仍独立限制更新期间不可用的 Pod 数量；
- `partition` 必须非负且不得大于 `replicas`；门禁更新进行中禁止修改 GSS partition，因此缩容不能低于当前目标 partition；需调整目标时先完成本轮，再调整 partition 和副本数；

新增 `completionPolicy` 与修改 PodTemplate 可以在同一次 GSS 更新中完成，此时新策略直接控制这轮新模板更新。若仅新增策略，则不追溯接管当前版本，也不触发一轮更新，而是从下一次 PodTemplate 变化开始生效。

## 5. 核心设计模型

**本方案不改变 ASTS 的更新机制**，只在其外层增加业务观察门禁：ASTS 负责选择并更新 Pod，OKG 负责检查业务条件与短观察窗口，并据此调整 ASTS partition。

一次正常更新的主流程如下：

1. GSS PodTemplate 变化后，OKG 先关闭更新窗口；ASTS 识别目标模板后，OKG 按 `maxUnavailable` 开放首个窗口。
2. ASTS 按 `podUpdatePolicy` 更新窗口内的 Pod；OKG 等待每个 Pod 完成更新，并按第 6.3 节观察 ServiceQuality 条件。
3. 每次放量前，OKG 重新计算全局窗口：一个 GameServer 通过观察就释放一个名额，业务条件退化则重新占用名额，直到达到 GSS 的目标 partition。

### 5.1 职责边界

| 对象 | 职责 |
| --- | --- |
| GSS spec | 表达目标 PodTemplate、最终 partition、更新方式和业务完成条件 |
| OKG GSS Controller | 计算业务窗口、调整 ASTS partition、输出更新进度 |
| ASTS | 创建或复用 ControllerRevision，按更新策略处理 Pod，并实施 Pod 可用性约束 |
| ServiceQuality | 提供单个 GameServer 当前业务状态的探测信号 |

OKG 不创建独立版本，不改变 ASTS Revision 语义，也不向 PodTemplate 注入仅用于制造新 Revision 的内部 annotation。

### 5.2 字段语义

| 字段 | 启用 `completionPolicy` 后的语义 |
| --- | --- |
| `completionPolicy` | 定义单个 GameServer 必须同时满足的业务完成条件 |
| `partition` | 可选的最终停止位置，未配置时默认为 0；内部称 `targetPartition` |
| `maxUnavailable` | 同一个上限分别约束 ASTS 更新期间不可用的 Pod 数，以及 OKG 已放行但当前尚未通过业务观察的 GameServer 名额数 |
| `paused` | 停止继续放量，不撤销已经发生的更新 |
| `podUpdatePolicy` | 仍由 ASTS 决定原地升级或重建 |
| `minReadySeconds`、更新优先级 | 保持 ASTS 原有语义 |

未配置 `completionPolicy` 时，所有字段含义与现有版本完全一致。

`maxUnavailable=2` 时，两个尚未通过业务观察的名额就会占满 OKG 窗口；ASTS 仍独立执行原有的 Pod 不可用约束，**两者不合并计数**。已放行但 ASTS 尚未开始更新的 Pod 也占用 OKG 窗口，无需在 OKG 复制 ASTS 的健康与更新判定。业务异常可能在放量后才被探测到，OKG 无法撤销已发生的更新，也不承诺实际异常 Pod 数始终不超过 `maxUnavailable`。

该设计不能表达“最多 1 个 Pod 不可用，但允许 5 个业务切换同时进行”。没有明确需求前不增加第二个并发字段。

### 5.3 partition 模型与不变量

启用业务门禁后，存在两个 partition：

- `targetPartition`：GSS spec 中现有 partition 的解析值，未配置时为 0，表示最终更新全部 Pod；只有需要部分更新时用户才主动配置；
- `currentPartition`：ASTS spec 中当前设置的 partition，表示已经放行的范围。

用户不需要维护 `currentPartition`。它由 OKG 在 ASTS 上动态调整，用户仍只通过 GSS 的现有字段表达最终更新范围。下文计算时以 ASTS spec 为准；GSS status 不复制该字段。

例如 `replicas=5`、`maxUnavailable=2`：

- **用户未配置 GSS partition**：`targetPartition` 默认为 0。ASTS partition（`currentPartition`）从 5 开始，首批放行两个后变为 3，随后随着业务观察通过逐步降至 0，例如 `5 → 3 → 2 → 1 → 0`，最终更新全部 5 个 Pod。
- **用户配置 GSS `partition=2`**：ASTS partition（`currentPartition`）同样从 5 开始，但只推进到 2，例如 `5 → 3 → 2`，然后停止；最终更新 3 个 Pod，保留 2 个 Pod 不更新。

同一个目标版本更新期间必须满足：

```text
targetPartition <= currentPartition <= replicas
```

副本数不变时，`currentPartition` 从 `replicas` 开始单调降低，最终与 `targetPartition` 一致。GSS PodTemplate 再次变化时可以重新设置为 `replicas`；扩缩容时可以随新副本数调整，但在 ASTS 和 Pod 集合收敛前不继续降低 partition。

**提高 partition 不能撤销已经发生的更新**，因此不能作为回滚手段。用户直接修改 GSS 所属 ASTS 的 partition 不受支持。

仅对配置了 `completionPolicy` 的 GSS，活动更新期间禁止修改目标 partition，包括同时修改 PodTemplate 的请求。省略与显式 0 视为相同值。完成后允许调整目标；准备更高目标走普通策略同步，降低目标以继续更新剩余 Pod 时仍按业务门禁容量推进。未配置 `completionPolicy` 时不增加 partition 限制。

## 6. 业务放行判定

一个 GameServer 只有同时满足三项条件，才算完成本轮业务观察：**Pod 已完成本轮更新、业务观察已通过、当前 ServiceQuality 满足要求**。业务观察可以由本轮新出现的匹配结果提前通过；否则等待短观察窗口。随后 OKG 才能释放窗口名额，继续更新其他 Pod。Pod Ready 不参与业务判定。

### 6.1 确认本轮更新

每次 GSS PodTemplate 变化都开始新一轮受控更新，包括回退到历史模板。OKG 在 GSS status 中记录这轮更新的标识和开始时间，以便 Controller 重启后仍能区分新旧轮次：

```yaml
status:
  updateProgress:
    updateGeneration: 12
    startedAt: "2026-10-19T08:00:00Z"
```

`updateGeneration` 是触发本轮 PodTemplate 更新时的 GSS generation；`startedAt` 用于标识和展示本轮开始时间，不与其他组件的时间戳比较；该展示时间缺失不阻塞更新，业务观察仍由 Pod checkpoint 和容器启动时间决定。

ASTS 识别新模板后，会在 `status.updateRevision` 中给出目标 Revision。OKG 此后才用它判断 Pod 是否完成本轮更新；GSS 不另存 Revision。仅修改 partition、`maxUnavailable` 或 `paused` 不会开始新一轮。状态与模板的写入顺序见第 7.2 节。

### 6.2 判断单个 GameServer 是否完成

OKG 对每个已放行更新的 GameServer 依次检查：

1. **更新完成**：Pod 已进入本轮目标 Revision，且 ASTS 对该 Pod 的本轮更新已经结束。仅 Revision 匹配、但原地升级仍在进行时，不算完成。
2. **业务观察通过**：匹配条件的状态转换时间晚于本轮观察起点；若被探测容器存在本次启动时间，还必须晚于该启动时间，才能提前通过。否则等待第 6.3 节的观察窗口结束。
3. **业务条件满足**：`completionPolicy.serviceQualities` 中的每一项当前都存在且匹配。任一条件缺失、为 Unknown 或不匹配，均不能释放窗口名额。

ServiceQuality 结果由 PodProbeMarker 写入自定义 PodCondition，再同步到 GS status；这里直接读取 PodCondition，避免再等待 GS status 同步。结果清洗和 `state/result` 匹配沿用现有 ServiceQualityAction 的语义，但不修改原有 Action 执行路径。Revision 和原地升级状态仅用于确认 Pod 更新是否完成。要求 True 的条件还需要被探测容器处于 Running 且有有效启动时间；要求 False 时允许匹配已结束容器的结果。

重建更新的 Pod UID 不同，不会继承旧 Pod 的 ServiceQuality 结果。原地升级是否完成以 OpenKruise 状态为准，不通过镜像 tag、imageID 或重启次数自行推断。两种更新方式都必须通过业务观察；如果无法确认被引用探针所检查的容器状态，就不能认定该 GameServer 已完成。

### 6.3 观察窗口与局限

观察窗口的作用是：Pod 更新结束后，给业务探针足够时间写回可能的失败结果，再读取当前条件。**它不是“新探测已执行”的证明**。

- 对每个被引用的 ServiceQuality，读取 GSS 配置的 `periodSeconds`、`timeoutSeconds`、`failureThreshold` 和 `initialDelaySeconds`；未配置时使用与下发 PodProbeMarker 相同的 OKG 默认值，分别为 3 秒、5 秒、3 次和 10 秒。
- 若匹配 PodCondition 的 `lastTransitionTime` 晚于本轮观察起点，且晚于被探测容器本次 `StartedAt`（存在时），**立即通过该探针的观察**，无需等满窗口；容器重启前的成功信号不能提前放行。
- 未配置 `completionPolicy.observationWindowSeconds` 时，等待截止时间为 **`max(checkpoint时间, 容器StartedAt + initialDelaySeconds) + (periodSeconds + timeoutSeconds) × failureThreshold + 2秒`**。每次失败预留完整调度周期和执行超时，包含首次探测的调度等待。默认附加窗口为 `(3 + 5) × 3 + 2 = 26 秒`；checkpoint 较早时，截止时间为容器启动后 36 秒。
- 可选的 `completionPolicy.observationWindowSeconds` 为正整数，直接覆盖自动窗口，截止时间为 **`max(checkpoint时间, 容器StartedAt) + observationWindowSeconds`**，不再额外叠加初始延迟。用户应为探针初始延迟、连续失败探测及结果写回预留足够时间；允许配置比自动值更短的窗口。引用多个探针时，每个探针分别判断，全部通过后才放行。
- 窗口到期后 Controller 主动重新调谐，即使 PodCondition 没有变化也会重新检查。

启动时间校验只限制提前放行。窗口结束后只要当前全部条件仍匹配，即使 `lastTransitionTime` 一直未刷新也能通过，不要求条件必须先变 False 再变 True。计时基于固定 checkpoint 和容器本次启动时间，不随每次 Reconcile 重置；容器再次重启时按新的启动时间计算。

为避免 Controller 重启后跳过等待，OKG 在首次确认 Pod 更新完成时，把观察起点写入该 Pod 的自有 annotation，并绑定 GSS `updateGeneration`、Pod UID 和目标 Revision；每个 Pod 每轮最多写一次。重启后复用有效检查点；检查点缺失、版本不匹配或信息冲突时重新计时，不立即放行。观察起点只用于计时，**不与 PodCondition 的 `lastProbeTime` 比较**。

这一方案有明确边界：原地升级前已有的 `True/ready` 可能在窗口结束后仍保持不变；自动窗口计入探测周期与单次超时，但固定余量不能保证组件拥塞时的写回或缓存同步时延，也无法证明探针已重新执行。第一期接受这一局限，不要求脚本返回发布标识，也不要求业务条件必须经历“不满足→满足”。若之后探测到条件不满足，OKG 会在全局重算时停止继续放量；未被采到或尚未写回的异常无法立即阻止放量。

## 7. 控制器设计

### 7.1 事实来源

控制器不维护独立 Rollout 状态机，每次 Reconcile 都从以下事实重新计算进度：

| 信息 | 事实来源 |
| --- | --- |
| 目标 PodTemplate、目标 partition、暂停和完成条件 | GSS spec |
| ASTS 已接收的模板与当前放行边界 | ASTS 模板指纹 annotation 与 spec |
| ASTS 是否已识别本次更新及目标 Revision | ASTS status |
| 单个 Pod 当前 Revision、原地升级状态、探针容器启动信息及本轮观察检查点 | Pod label、annotation 和 status |
| 当前业务信号 | 被引用的 ServiceQuality PodCondition |
| 本轮更新时间边界 | GSS status.updateProgress |

GSS status 是必要的更新时间检查点和对外进度，不替代 ASTS、Pod 等事实来源。

### 7.2 调谐流程

每次 Reconcile 按以下顺序执行：

1. 未配置 `completionPolicy` 时，完全使用现有 GSS 更新逻辑；
2. 发现 GSS `spec.gameServerTemplate` 相比 ASTS 当前 PodTemplate 发生变化时，先记录本轮 `updateGeneration` 和 `startedAt`；模板是否变化通过 OKG 写在 ASTS 上的指纹判断，避免 API Server 补默认字段造成误判；
3. 将目标 PodTemplate 与 `partition=replicas` 原子写入 ASTS，先关闭更新窗口；
4. 等待 ASTS `status.observedGeneration` 追上，再从 `ASTS.status.updateRevision` 读取目标 Revision；
5. 按第 6 章规则检查 Pod，统计目标 Revision 数量和当前通过观察的数量；对尚未到期的观察窗口设置定时重新调谐；
6. 用户未暂停时，按 `maxUnavailable` 计算下一步 ASTS partition；
7. 必要时更新 ASTS partition，并同步 GSS `updateProgress` 和 `UpdateProgressing` Condition。

调谐必须幂等。写入 GSS status 后、更新 ASTS 前重启时，Controller 根据本轮状态继续执行，不会提前放量；API 冲突、缓存延迟或无法确认事实时也不继续降低 partition。

升级前已存在且没有模板指纹的 ASTS，先通过 API Server dry-run 比较默认化后的模板；确认未变化时补上指纹，确认变化时仍先关闭更新窗口。dry-run 失败时不放量。

一轮受控更新只有同时满足以下两个条件才算结束：

```text
currentPartition == targetPartition
且 pendingCompletionReplicas == 0
```

到达目标 partition 只表示最后一个窗口已经放行；窗口中的 GameServer 尚未通过业务观察时，`UpdateProgressing` 必须继续保持 `True`。本轮结束后不因日常业务健康波动重新打开 rollout；后续健康状态仍由 ServiceQuality 和 GS 表达。

### 7.3 放量计算

这里使用容量为 `maxUnavailable` 的**滑动窗口**，不是等一整批全部通过后再开放下一批的批次屏障。每次准备降低 ASTS partition 时，OKG 都重新检查本轮全部已放行的名额：未开始更新、Pod 暂时缺失、尚在观察窗口内、业务条件缺失或不匹配的名额均占用窗口；此前已通过但后来业务条件退化的 Pod 也重新占用名额。窗口未满时才继续放量。

设当前已通过业务观察的 GameServer 数量为 `completedReplicas`，则：

```text
累计允许更新数 = completedReplicas + maxUnavailable
```

累计允许更新数不能超过用户通过 GSS partition 指定的更新范围。Controller 据此计算 ASTS partition，并且只会继续降低 partition，不会为了回收名额而撤销已经发生的更新。

例如 `replicas=5`、GSS partition 默认为 0、`maxUnavailable=2`：

- 尚无 GameServer 通过观察时，累计允许更新 2 个，ASTS partition 为 3；
- 通过 1 个后，累计允许更新 3 个，ASTS partition 降为 2；
- 通过 2 个后，累计允许更新 4 个，ASTS partition 降为 1。

`completedReplicas` 在更新期间按当前业务条件重新计算，可以因某个 Pod 后续退化而减少。当前已放行数由 ASTS partition 决定；即使 ASTS 因 Pod 不可用、`minReadySeconds` 或调谐延迟尚未开始更新，这些已放行名额仍占用窗口，Controller 不会因此重复放量。业务失败写回有延迟时，已开放的更新不能撤销；一旦观察到窗口已满或超限，OKG 只停止继续降低 partition。

窗口占用按已放行的名额计算，不按当前存在的 Pod 数计算；名额对应的 Pod 尚未创建或暂时缺失时仍算未通过，不能释放新名额。回退到旧业务版本也按新一轮更新处理，旧轮检查点不能释放新名额。

## 8. 边界规则

边界场景仍使用同一套事实计算，不引入额外 phase：

| 场景 | 处理规则 |
| --- | --- |
| GSS 首次创建或普通扩容 | `completionPolicy` 不限制 Pod 创建并发，继续使用现有 scaleStrategy 和 ASTS 行为 |
| 已有 GSS 单独新增 completionPolicy | 只启用策略，不追溯接管当前版本、不创建 `updateProgress`；若 ASTS 正在执行原有更新，该更新仍按原策略完成，`completionPolicy` 从下一次 PodTemplate 变化开始生效 |
| 新目标版本或回退 | 建立新的更新时间边界，将新模板与 `partition=replicas` 写入 ASTS；旧轮检查点不得复用，再按正常流程放量 |
| 用户修改 partition | 活动门禁更新中禁止任意修改，包括同时更新模板；缩容时旧 partition 超过新副本数，可同步降到新副本数；完成后可调整。未配置 completionPolicy 不增加限制 |
| 用户修改 maxUnavailable | 立即用于后续窗口；减小后如果当前已超限，只停止新增放量 |
| 用户修改 observationWindowSeconds | 允许单独调整为正数或移除自定义值，复用本轮检查点按新窗口判断，不建立新轮进度 |
| 用户修改被引用 ServiceQuality 的动作或 permanent | 允许调整，继续使用原有动作处理机制；这些字段不参与门禁完成判断 |
| 用户设置 paused | 同一目标版本内保持 ASTS 当前 partition，不撤销已经发生的更新；暂停期间出现新 PodTemplate 时，仍先建立新一轮更新并将 partition 重置为 `replicas`，但不开放首个窗口 |
| 活跃更新中扩缩容 | 扩缩容本身不受 `completionPolicy` 节流；先冻结新增更新放量，并按新副本数调整 partition，尽量保持 `replicas-currentPartition` 不变；等待 ASTS 与 Pod 集合收敛后，按实际 Revision 和完成状态重新统计再继续推进 |
| 活跃更新中移除 completionPolicy | 显式取消业务门禁，不要求先暂停；先清理本轮进度及 UpdateProgressing Condition，再恢复 GSS 配置的普通更新策略和目标 partition，不表示业务已 Ready |
| 活跃更新中单独修改完成条件或被引用探针 | 拒绝修改，避免中途改变完成标准 |
| 新 PodTemplate 与完成条件或被引用探针同时变化 | 视为新一轮更新，允许新一轮使用新的完成标准；正在进行的旧目标被新目标取代 |
| Controller 重启 | 使用持久化的 `updateProgress`、Pod 观察检查点以及 GSS、ASTS、Pod 事实继续调谐；检查点无效时重新等待观察窗口，不立即开放名额 |
| 活跃更新的 updateProgress 缺失或矛盾 | 无法确认有效轮次，停止降低 partition，并将 `UpdateProgressing` 设为 `Unknown`；不得通过自动重置状态继续放量 |
| 状态冲突或证据不足 | 不降低 partition，`UpdateProgressing` 设为 Unknown 并说明原因 |
| 用户直接修改 ASTS partition | 不受支持，后续由 OKG 按 GSS 目标和当前事实纠正 |

扩缩容期间，设调整前已经开放的容量为 `oldReplicas-oldCurrentPartition`。Controller 先将新的 current partition 调整为：

```text
clamp(newReplicas - 已开放容量, targetPartition, newReplicas)
```

这一步只用于避免副本数变化意外扩大更新窗口；Pod 集合稳定后，仍以实际 Revision 和 ServiceQuality 完成状态重新计算下一步放量。

当前不提供通过修改 partition 提前结束活动门禁更新的路径。需要暂时停止后续放量时使用 `paused=true`，已放行对象仍可能继续更新，暂停不会将本轮标记为 Completed。需要切换目标版本或回退镜像时可修改 PodTemplate，partition 保持原值，新轮仍按门禁推进。需要取消业务门禁时可直接移除 `completionPolicy`，后续更新按 GSS 的普通更新策略继续，仍遵守 `paused`、目标 partition、`maxUnavailable` 及 ASTS 自身的更新限制；不增加独立的取消状态机。

配置 `completionPolicy` 即启用业务门禁，无需额外启动参数；未配置时行为完全不变。活跃更新期间不得降级到不识别该字段的旧 Controller。

## 9. 状态设计

新增 `status.updateProgress` 和标准 `status.conditions`，现有副本统计继续复用：

现有 `updatedReplicas` 表示处于目标 Revision 的 Pod 数量，**不表示业务切换已经完成**；新增的 `completedReplicas` 表示本轮更新期间**当前通过业务观察**的名额数，不是“已证明探针在本轮重新执行”的数量。更新进行中该值可能因业务条件退化而减少；本轮结束后保留最终快照，不再用于日常业务健康统计。

```yaml
status:
  observedGeneration: 15
  replicas: 5
  readyReplicas: 5
  updatedReplicas: 2
  updatedReadyReplicas: 2
  updateProgress:
    updateGeneration: 12
    completedReplicas: 1
    pendingCompletionReplicas: 1
    startedAt: "2026-10-19T08:00:00Z"
  conditions:
    - type: UpdateProgressing
      status: "True"
      observedGeneration: 15
      reason: WaitingForBusinessObservation
      message: "1 GameServer is waiting for ServiceQuality observation"
```

| 新增字段 | 含义 |
| --- | --- |
| `updateGeneration` | 触发本轮 PodTemplate 更新时的 GSS generation |
| `completedReplicas` | 更新进行中当前通过业务观察的名额数；结束后保留最终快照 |
| `pendingCompletionReplicas` | 已放行但当前未通过观察的名额数，包括尚未开始更新、Pod 暂时缺失、观察未到期或业务条件退化的名额 |
| `startedAt` | 整轮更新开始时间，用于展示与记录；单 Pod 观察窗口使用 checkpoint 时间和容器启动时间 |

最终更新范围读取 `GSS.spec.updateStrategy.rollingUpdate.partition`；当前放行边界读取 `ASTS.spec.updateStrategy.rollingUpdate.partition`。GSS status 不另存 partition 副本。

`UpdateProgressing` 使用标准 `metav1.Condition`：

- `True`：受控更新仍在进行，reason 区分等待 Revision、等待业务观察、用户暂停等；
- `False`：本轮受控更新已结束，即 ASTS 已到达目标 partition，且所有已放行 GameServer 均已通过观察；
- `Unknown`：事实冲突或证据不足，Controller 已停止继续放量。

`updateProgress` 在受控更新开始时创建，完成后保留最后结果，下一轮更新时覆盖；移除 `completionPolicy` 后先清理该进度及 `UpdateProgressing` Condition，保留其他 Condition，再恢复普通更新。清理采用带版本冲突重试和 generation 校验的状态更新，避免旧轮覆盖新轮；即使清理后重启或立即重新添加策略，没有门禁轮次时仍按 GSS 配置同步普通 partition，避免遗留旧门禁边界。清理后普通 status 同步保持原有副本计数 Merge Patch 行为，稳定计数不重复写入。仅更新开始、暂停、恢复、完成及新出现或原因变化的异常产生 Event；正常等待阶段切换和计数变化只更新 Condition，实际放量记录日志，不增加独立 phase、策略哈希或进度超时状态机。

## 10. 实现与验证

### 10.1 主要改造点

- 在 GSS RollingUpdate API 中增加 `completionPolicy.serviceQualities`；
- 在 GSS status 中增加最小 `updateProgress` 和 `conditions`；
- 为完成判定复用现有 Condition 查找工具，并单独实现与现有 Action 一致的结果匹配；原有 Action 执行代码保持不变；
- 将 GSS 到 ASTS 的模板、partition 和 replicas 写入收敛到同一协调路径；
- 在 ASTS annotation 中记录模板指纹，用于区分真正的模板变更和 API Server 默认字段；
- 增加单 Pod 观察判定器和窗口计算器；首次观察到本轮 Pod 更新结束后写入一次 Pod 检查点，不为每次探测写入；
- 复用现有 Pod 与 ASTS Watch；观察窗口尚未到期时设置定时重新调谐，业务条件变化时也重新计算；
- Admission Webhook 只增加 completionPolicy 的静态校验，不附带收紧 reserveGameServerIds 的存量准入行为。活动更新允许取消门禁、调整观察窗口和服务质量动作，以及缩容所需的 partition 调整；其余 partition 变更、单独修改完成条件或被引用探针仍拒绝。partition 校验直接比较新旧 GSS，无需查询 ASTS。

每个 Pod 每轮至少持久化一次观察 checkpoint，单次 Reconcile 最多执行 32 次 checkpoint Patch，写入采用乐观锁；后续调谐复用有效记录。每轮扫描当前 Pod 集合，暂不增加无界并发写入或新的云资源。规模较大时需要评估 API QPS 和 worker 占用，不能将窗口容量等同于已测量的吞吐。

### 10.2 关键测试

**兼容性**

- 未配置 `completionPolicy` 时行为不变；单独新增时不接管已有更新，与 PodTemplate 同时变更时控制本轮更新。
- 整数及百分比 `maxUnavailable` 与 ASTS 解析一致；原地升级、重建、更新优先级及用户 partition 继续生效。

**观察判定**

- 多个条件必须全部匹配；缺失、Unknown 或不匹配时不通过。
- 原地升级前遗留的 `True/ready` 在短窗口内不能释放名额；本轮观察之后、容器本次启动之后的新状态转换可以提前放行。旧 True 不刷新时间戳时窗口到期仍能通过，不以 `lastProbeTime` 证明新探测已执行。
- 重建 Pod 不继承旧结果；仅被探测容器重启时重新等待初始延迟。探针周期、失败阈值与多个探针的独立观察正确，窗口到期能主动重新调谐。
- 探针超时或组件拥塞导致失败结果晚于窗口写回时，后续状态变化仍能停止新增放量；通过 E2E 校验默认窗口与实际时延。

**窗口与恢复**

- 已放行但尚未开始更新、Pod 尚未创建或暂时缺失的名额仍占用窗口；业务条件退化后重新占用名额，窗口满或超限时停止新增放量。
- Controller 在状态写入和 ASTS 更新的各阶段重启，以及 API 冲突、缓存延迟时，都不会重复放量；Pod 检查点无效时重新等待，活动更新状态缺失或矛盾时报告 `Unknown`。
- 连续模板变更和回退不复用旧轮完成结果；扩缩容、暂停及 `maxUnavailable` 动态调整符合第 8 章规则；活动门禁更新期间仅放开缩容所需的 partition 调整，完成后可任意调整合法值。
- ASTS 到达目标 partition 但仍有名额未通过观察时保持更新中；全部通过后结束，后续日常健康波动不重新打开更新。
- 活跃更新中允许移除 `completionPolicy`，恢复普通策略并清理门禁状态；仍保留策略时单独修改完成条件会被拒绝，新 PodTemplate 可以携带新的完成条件建立新一轮更新。
- 移除策略后先清理旧进度及自身 Condition，保留其他 Condition；稳定计数不重复写入，计数变化及降为 0 时仍正常同步；清理后重启或立即重新添加策略不接管旧轮次、不遗留旧 partition。调整观察窗口及动作不重置进度或检查点；缩到较小副本数及 0 时可同步调整目标 partition 并正常收敛。

## 11. 版本依据

本提案原以 OpenKruise v1.8.0 行为为基线；第 6.3 节探针执行与结果写回时序另按本地 OpenKruise v1.8.1 源码核对。实现及部署前仍须以目标集群实际 OpenKruise 版本验证。关键依赖包括：

- `pkg/util/inplaceupdate/inplace_update.go`：每次原地升级写入 revision 和 `updateTimestamp`；
- `pkg/util/inplaceupdate/inplace_update_defaults.go`：PodTemplate metadata 变更可形成原地升级 spec，并由 OpenKruise 判断容器更新完成；
- `pkg/controller/statefulset/stateful_update_utils.go`：UnorderedUpdate 先选择已经处于目标 Revision 的 Pod，再按优先级选择待更新 Pod；
- `pkg/controller/statefulset/stateful_set_control.go`：ASTS 对 `maxUnavailable` 的解析和更新期间 Pod 不可用数量的限制；
- OKG `pkg/controllers/gameserverset/gameserverset_manager.go`：ServiceQuality 的 `initialDelaySeconds`、`periodSeconds` 等默认值与 PodProbeMarker 转换；
- OpenKruise `pkg/daemon/podprobe/worker.go`：探针从被探测容器启动时间计算初始延迟，按周期与成功/失败阈值执行；
- `pkg/daemon/podprobe/results_manager.go` 与 `pkg/controller/nodepodprobe/node_pod_probe_controller.go`：相同探测状态和消息不会持续刷新 PodCondition。

## 12. 启用与升级

使用前先更新与目标 OKG 版本匹配的 GameServerSet CRD，保留原有字段并增加 `completionPolicy`、`status.updateProgress` 和 `status.conditions`；不删除或重建 CRD，不迁移 API 版本。随后升级全部 Controller/Webhook 实例的镜像，最后在业务 GSS 配置 `completionPolicy` 并通过模板更新启动门禁，无需额外启动参数。临时镜像本身不会更新 CRD。

ECI 存量 Pod 的探针配置可能不会随 GSS/PodProbeMarker 修改而更新。启用前应确认所需探针已在 Pod 创建时注入且正常上报；新增或调整探针应通过适当的新建或重建路径生效。升级 OKG 组件本身不要求重建业务 Pod。

退出功能时，在支持取消门禁的新 Controller 仍运行时移除策略，确认 ASTS 已恢复 GSS 配置的普通更新策略且门禁状态已清理，再恢复旧镜像；可保留新增的可选 CRD 字段。`paused=true` 只暂停后续放量，不将活动更新标记为完成；移除策略后仍保留暂停设置，需单独恢复。

升级 OpenKruise 前应验证这些版本依赖。若任一假设变化，应先评估兼容逻辑，通过验证后再对配置了 `completionPolicy` 的 GSS 执行更新。
