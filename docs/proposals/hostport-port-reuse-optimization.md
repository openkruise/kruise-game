 HostPort 端口分配优化方案

## 1. 背景

OpenKruiseGame (OKG) 的 Kubernetes-HostPort 网络插件在 Pod 创建时（通过 MutatingWebhook）为游戏服务器分配 hostPort 端口号。原始版本存在一个设计缺陷：将 HostPort 端口视为全局唯一资源，导致端口数量直接等于可支撑的 Pod 上限。

### 1.1 问题现象

集群配置：
- 端口范围：`min_port=8000, max_port=8500`（共 501 个端口）
- 节点数量：~300 个
- 目标：扩容 1000 个 Pod

实际结果：**仅能创建 501 个 Pod，随后报错 "insufficient ports available"**。

### 1.2 问题本质

HostPort 在 Kubernetes 中是 **per-node 资源**——同一端口号可在不同节点上同时使用，Kubernetes 调度器内置了 per-node 的 hostPort 冲突检测。因此：

```
理论上限 = 端口数量 × 节点数 = 501 × 300 = 150,300 个 Pod
```

但原始版本的实现将端口当作全局资源，每个端口号只分配一次，使得上限退化为：

```
实际上限 = 端口数量 = 501 个 Pod
```

**容量损失了 300 倍**（节点数）。

## 2. 原始版本分析

### 2.1 数据结构

```go
type HostPortPlugin struct {
    maxPort      int32
    minPort      int32
    podAllocated map[string]string   // podKey -> "port1,port2,..."（字符串存储）
    portAmount   map[int32]int       // port -> 使用次数
    amountStat   []int               // index=使用次数, value=有多少端口处于该使用次数
    mutex        sync.RWMutex
}
```

使用了 `portAmount` 和 `amountStat` 两个数据结构联动来追踪端口使用情况。

### 2.2 端口选择算法

```go
func selectPorts(amountStat []int, portAmount map[int32]int, num int) ([]int32, int) {
    // 1. 通过 amountStat 找到最低使用层级（有足够可用端口的层级）
    var index int
    for i, total := range amountStat {
        if total >= num {
            index = i
            break
        }
    }
    // 2. 从 portAmount 中选取处于该层级的端口
    hostPorts := make([]int32, 0)
    for hostPort, amount := range portAmount {
        if amount == index {
            hostPorts = append(hostPorts, hostPort)
            num--
        }
        if num == 0 { break }
    }
    return hostPorts, index
}
```

### 2.3 问题分析

这个算法 **设计上具备端口复用能力**——`portAmount` 记录了每个端口的使用次数，`amountStat` 通过层级索引实现 O(1) 查找最少使用层级。理论上，当所有端口都被使用 1 次后（`amountStat[0]=0, amountStat[1]=501`），应该可以从 `amountStat[1]` 层级继续分配，实现端口复用。

**但实际运行中并未如预期工作**。在大规模扩容场景下（如 1000 Pod），当 501 个端口全部被分配一次后，系统报错 "insufficient ports available"，没有进入端口复用阶段。

核心问题在于：

1. **`selectPorts` 的 `amountStat` 层级查找存在边界缺陷**：当 `amountStat[0]` 变为 0 后（所有端口至少用了 1 次），算法查找 `total >= num` 的层级时，如果请求的端口数 `num` 大于当前层级的端口总数，可能找不到满足条件的层级
2. **双数据结构联动的脆弱性**：`portAmount` 和 `amountStat` 需要严格同步更新，在并发 webhook 场景下容易出现不一致
3. **map 遍历的不确定性**：Go 的 map 遍历顺序是随机的，`for hostPort, amount := range portAmount` 选出的端口集合不稳定，可能导致分配不均匀

## 3. 优化方案

### 3.1 核心思路

**正确对齐 Kubernetes 的 HostPort 语义**：端口是 per-node 资源，允许同一端口号被多个 Pod 使用，由 Kubernetes 调度器保证同一节点上不会冲突。

### 3.2 新数据结构

```go
type HostPortPlugin struct {
    maxPort int32
    minPort int32

    mu        sync.Mutex
    portUsage []int32            // index: port-minPort, value: 使用该端口的 Pod 数量
    podPorts  map[string][]int32 // podKey -> 已分配的端口列表
}
```

改动：
- `portAmount map[int32]int` + `amountStat []int` → **`portUsage []int32`** 单一计数器数组
- `podAllocated map[string]string` → **`podPorts map[string][]int32`**，从逗号分隔字符串改为原生 int32 切片
- `sync.RWMutex` → **`sync.Mutex`**，简化锁类型

### 3.3 最少使用分配算法

```go
func (hpp *HostPortPlugin) allocatePorts(num int, podKey string) ([]int32, error) {
    hpp.mu.Lock()
    defer hpp.mu.Unlock()

    // 幂等性：如果已有分配记录，直接返回
    if existing, ok := hpp.podPorts[podKey]; ok {
        return existing, nil
    }

    // 选择使用次数最少的端口（least-used strategy）
    result := make([]int32, 0, num)
    selected := make(map[int32]bool, num)
    for len(result) < num {
        minUsage := int32(math.MaxInt32)
        bestPort := hpp.minPort
        for p := hpp.minPort; p <= hpp.maxPort; p++ {
            usage := hpp.portUsage[p-hpp.minPort]
            if !selected[p] && usage < minUsage {
                minUsage = usage
                bestPort = p
            }
        }
        result = append(result, bestPort)
        selected[bestPort] = true
        hpp.portUsage[bestPort-hpp.minPort]++
    }
    hpp.podPorts[podKey] = result
    return result, nil
}
```

**算法特点**：
- **永远能分配成功**：不存在"端口耗尽"——只要还有节点能调度，就能分配端口号
- **均匀分布**：每次选择使用次数最少的端口，确保所有端口的使用频率趋于一致
- **顺序遍历**：使用 `for p := minPort; p <= maxPort` 而非 map 遍历，分配结果确定性强

### 3.4 端口释放

```go
func (hpp *HostPortPlugin) deallocatePorts(podKey string) []int32 {
    hpp.mu.Lock()
    defer hpp.mu.Unlock()

    ports, ok := hpp.podPorts[podKey]
    if !ok { return nil }
    for _, port := range ports {
        idx := port - hpp.minPort
        if idx >= 0 && idx < int32(len(hpp.portUsage)) && hpp.portUsage[idx] > 0 {
            hpp.portUsage[idx]--
        }
    }
    delete(hpp.podPorts, podKey)
    return ports
}
```

相比原始版本的 `deAllocate()` 需要同步更新 `portAmount` 和 `amountStat` 两个数据结构，优化版本只需递减计数器。

## 4. Kubernetes 调度器如何保证安全

Kubernetes 调度器内置 `NodePorts` 插件（`pkg/scheduler/framework/plugins/nodeports/`），在 Filter 阶段执行：

1. 获取目标节点上所有已调度 Pod 的 hostPort 列表
2. 与待调度 Pod 的 hostPort 对比
3. 若存在冲突（相同端口 + 相同协议 + 相同 IP），排除该节点

**职责分离**：
- OKG 负责"选端口号"——通过最少使用策略均匀分配
- Kubernetes 调度器负责"选节点"——自动避开 hostPort 冲突的节点

两者无需任何额外配合，这是 Kubernetes 的标准行为。

### 调度失败的边界条件

当某个端口的使用次数 ≥ 节点数时，使用该端口的下一个 Pod 将找不到可用节点（所有节点都已占用该端口）。但在最少使用策略下，这意味着**所有端口**都已达到接近节点数的使用次数，此时总 Pod 数接近理论上限 `端口数 × 节点数`。

## 5. 对比总结

| 维度 | 原始版本 | 优化版本 |
|------|---------|--------|
| **端口模型** | 全局资源（分配后不再复用） | Per-node 资源（允许跨节点复用） |
| **容量上限** | `端口数量`（501 Pod） | `端口数量 × 节点数`（150,300 Pod） |
| **分配策略** | amountStat 层级选择 | 最少使用（least-used） |
| **数据结构** | `map[int32]int` + `[]int` 双结构联动 | `[]int32` 单一数组 |
| **Pod 端口记录** | `map[string]string`（逗号分隔字符串） | `map[string][]int32`（原生切片） |
| **锁** | `sync.RWMutex` | `sync.Mutex` |
| **遍历确定性** | map 随机遍历 | 数组顺序遍历 |
| **释放复杂度** | 两个数据结构联动更新 | 单计数器递减 |
| **端口耗尽风险** | 端口用完即停止 | 无耗尽风险（上限由节点数决定） |
| **配置** | `min_port`, `max_port` | `min_port`, `max_port`（不变） |

## 6. 锁选型说明

原始版本使用 `sync.RWMutex`，但 `allocate()` 和 `deAllocate()` 均需要写锁，读锁几乎无使用场景。在 webhook 调用路径上，每次都是写操作。

优化版本使用 `sync.Mutex`：
- 临界区操作：遍历 ~501 个 int32 计数器 + 更新计数器 + 写 map
- 临界区耗时：~3μs
- 1000 并发请求串行通过：~3ms 总等待
- 远小于 Kubernetes API 调用延迟（10-50ms），不构成瓶颈

## 7. Init 状态恢复

`Init()` 在 controller 启动时从集群现有 Pod 恢复状态：

```go
for _, pod := range podList.Items {
    // 遍历 Pod 的 container ports，恢复使用计数
    hpp.portUsage[port.HostPort - hpp.minPort]++
    hpp.podPorts[podKey] = hostPorts
}
```

与原始版本的关键区别：**同一端口号被多个 Pod 使用时，计数器会正确累加**，准确反映当前集群状态。原始版本虽然也有 `portAmount` 计数，但后续分配不会利用这些已用端口。

## 8. 部署与容量规划

### 配置

```toml
[kubernetes.hostPort]
min_port = 8000
max_port = 8500
```

无需其他配置，无需与调度器配合。

### 容量公式

```
最大 Pod 数 = min(端口数量 × 节点数, 节点数 × 单节点资源上限)

示例：
  501 端口 × 300 节点 = 150,300 Pod
  实际受限于节点 CPU/内存，通常远小于此值
```

### 升级方式

- **热升级**：Init() 自动从集群现有 Pod 恢复端口使用状态，无需停服
- **建议验证**：升级后观察端口分配日志，确认 `port-reuse support` 初始化信息
- **监控**：通过 OpenTelemetry span 属性 `ports_allocated_count` / `released_ports_count` 观察分配释放情况
