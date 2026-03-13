# Multi-Flow Limiter

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Test Coverage](https://img.shields.io/badge/coverage-98.6%25-brightgreen.svg)](#测试覆盖率)

[English](README.md) | 中文

一个高性能、基于优先级的弹性限流器，支持容量共享的 Go 语言实现。

## 概述

Multi-Flow Limiter 是一个灵活的限流库，实现了基于优先级的多桶令牌桶算法。它允许你定义多个具有保障最小 QPS 的流量类别，同时支持弹性容量共享 - 高优先级桶可以自动利用低优先级桶的未使用容量。

### 核心特性

- **多优先级桶**：定义具有不同优先级的多个流量类别
- **保障最小 QPS**：每个桶基于权重分配获得保障的最小 QPS
- **弹性容量共享**：未使用的容量自动流向高优先级桶
- **令牌桶算法**：懒填充机制，可配置突发容量
- **线程安全**：通过互斥锁保护并发访问
- **零依赖**：仅使用 Go 标准库和测试框架
- **高测试覆盖率**：98.6% 代码覆盖率，测试套件完善

## 安装

```bash
go get github.com/szd-dev/multi_flow_limiter
```

## 快速开始

```go
package main

import (
    "fmt"
    "time"
    
    limiter "github.com/szd-dev/multi_flow_limiter"
)

func main() {
    // 创建多桶限流器
    config := limiter.MultiFlowLimiterConfig{
        TotalQPS:     1000, // 系统总 QPS 上限
        BurstSeconds: 1.0,  // 突发窗口（秒），容量 = TotalQPS * BurstSeconds
        Buckets: []limiter.BucketConfig{
            {Name: "critical", Weight: 3}, // 高优先级 - 保障 30%
            {Name: "normal",   Weight: 5}, // 中优先级 - 保障 50%
            {Name: "bulk",     Weight: 2}, // 低优先级 - 保障 20%
        },
    }
    
    rateLimiter := limiter.NewElasticMultiLimiter(config)
    
    // 检查特定桶的请求是否被允许
    if rateLimiter.Allow(0) { // 桶索引 0 = "critical"
        fmt.Println("关键请求被允许")
    }
    
    // 监控桶状态
    fmt.Printf("桶数量: %d\n", rateLimiter.BucketCount())
    fmt.Printf("关键桶名称: %s\n", rateLimiter.BucketName(0))
    fmt.Printf("关键桶最小 QPS: %.2f\n", rateLimiter.MinQPS(0))
    fmt.Printf("系统总 QPS: %.2f\n", rateLimiter.LimitRate())
}
```

## 架构设计

### 核心概念

#### 1. 懒填充令牌桶

限流器使用懒填充的令牌桶算法。令牌根据自上次填充以来经过的时间进行补充，计算公式为：

```
新增令牌 = 经过的秒数 * totalQPS
```

#### 2. 两阶段填充算法

当令牌补充时，算法分两个阶段操作：

**阶段一 - 保障分配**：
- 每个桶按权重比例接收令牌
- 确保每个桶获得其最小保障 QPS
- 达到容量的桶溢出部分返回共享池

**阶段二 - 优先级分配**：
- 剩余令牌（来自溢出或未使用的容量）按优先级分配
- 高优先级桶（索引较小）优先获得剩余令牌
- 允许桶突发超过其保障最小值

#### 3. 容量共享

所有桶共享相同的最大容量（`cap = totalQPS * burstSeconds`）：

- **示例**：如果 `totalQPS=1000` 且 `burstSeconds=2`，每个桶最多可容纳 2000 个令牌
- 当桶不使用其令牌时，它们对所有桶保持可用
- 如果其他桶空闲，高优先级桶可以消耗整个系统容量

### 基于权重的 QPS 分配

桶权重被归一化以计算最小保障 QPS：

```
桶的最小QPS = totalQPS * (桶权重 / 所有权重之和)
```

**示例**：
```
总 QPS: 1000
桶: [critical=3, normal=5, bulk=2]
权重总和: 10

最小 QPS:
- critical: 1000 * (3/10) = 300 QPS 保障
- normal:   1000 * (5/10) = 500 QPS 保障
- bulk:     1000 * (2/10) = 200 QPS 保障
```

### 优先级顺序

桶按其在配置数组中的位置排序：
- **索引 0**：最高优先级（首先接收剩余令牌）
- **索引 n-1**：最低优先级（最后接收剩余令牌）

## API 参考

### 类型定义

```go
type BucketConfig struct {
    Name   string  // 桶名称（标识符）
    Weight float64 // 权重比例（用于计算 minQPS，自动归一化）
}

type MultiFlowLimiterConfig struct {
    TotalQPS     int64          // 系统总 QPS 上限
    BurstSeconds float64        // 突发窗口（秒），容量 = TotalQPS * BurstSeconds
    Buckets      []BucketConfig // 桶配置，顺序 = 优先级（索引 0 最高）
}
```

### 构造函数

```go
func NewElasticMultiLimiter(config MultiFlowLimiterConfig) *ElasticMultiLimiter
```

创建新的弹性多桶限流器。

**配置校验规则**：
- `TotalQPS` 必须 >= 0（负数会 panic）
- `BurstSeconds` 必须 > 0（零或负数会 panic）
- `Buckets` 不能为空（空会 panic）
- 所有 `Weight` 必须 > 0（零或负数会 panic）

### 方法

```go
func (eml *ElasticMultiLimiter) Allow(bucketIdx int) bool
```
尝试从指定桶消耗 1 个令牌。成功返回 `true`，拒绝或桶索引越界返回 `false`。

```go
func (eml *ElasticMultiLimiter) BucketCount() int
```
返回桶数量。

```go
func (eml *ElasticMultiLimiter) BucketName(bucketIdx int) string
```
返回指定桶的名称。索引越界返回空字符串。

```go
func (eml *ElasticMultiLimiter) Tokens() []float64
```
返回所有桶的当前令牌数。用于监控和调试。

```go
func (eml *ElasticMultiLimiter) MinQPS(bucketIdx int) float64
```
返回指定桶的最小保障 QPS。索引越界返回 0。

```go
func (eml *ElasticMultiLimiter) LimitRate() float64
```
返回系统总 QPS 上限。

## 使用场景

### 1. API 限流（多优先级层级）

```go
config := limiter.MultiFlowLimiterConfig{
    TotalQPS:     10000,
    BurstSeconds: 2.0,
    Buckets: []limiter.BucketConfig{
        {Name: "premium",  Weight: 5}, // 高级用户 - 保障 50%
        {Name: "standard", Weight: 3}, // 标准用户 - 保障 30%
        {Name: "free",     Weight: 2}, // 免费用户 - 保障 20%
    },
}
```

## 测试覆盖率

该库拥有全面的测试覆盖，**98.6%** 的语句被覆盖。

### 各函数覆盖率

| 函数 | 覆盖率 |
|------|--------|
| `NewElasticMultiLimiter` | 100.0% |
| `refillLocked` | 96.7% |
| `Allow` | 100.0% |
| `BucketCount` | 100.0% |
| `BucketName` | 100.0% |
| `Tokens` | 100.0% |
| `MinQPS` | 100.0% |
| `LimitRate` | 100.0% |
| **总计** | **98.6%** |

### 测试类别

测试套件包括：

1. **构造函数校验**：测试所有 panic 条件（负 QPS、无效权重等）
2. **MinQPS 分配**：验证基于权重的 QPS 分配和归一化
3. **基本访问器**：测试所有 getter 方法和边界条件
4. **边界检查**：验证桶索引范围校验
5. **边界情况**：零 QPS、空桶、并发访问
6. **令牌填充**：验证懒填充和令牌消耗
7. **保障最小 QPS**：确保满载时每个桶获得其最小值
8. **优先级顺序**：验证高优先级桶优先获得剩余令牌
9. **容量共享**：测试所有桶共享相同容量限制
10. **并发安全**：30 个 goroutine 同时访问
11. **手动沙盒**：可视化限流器行为的交互式测试

### 运行测试

```bash
# 运行所有测试并生成覆盖率
go test -cover -coverprofile=coverage.out

# 查看各函数覆盖率
go tool cover -func=coverage.out

# 生成 HTML 覆盖率报告
go tool cover -html=coverage.out -o coverage.html

# 运行详细测试
go test -v

# 运行特定测试
go test -v -run TestElasticMultiLimiter_ManualPlayground -count=1
```

## 性能特征

- **时间复杂度**：令牌填充为 O(n)，其中 n 为桶数量
- **空间复杂度**：桶存储为 O(n)
- **并发性**：互斥锁保护，并发安全
- **内存**：最小开销，仅存储桶状态和时间戳

## 许可证

本项目采用 MIT 许可证 - 详情请参见 [LICENSE](LICENSE) 文件。

## 致谢

- 灵感来源于令牌桶算法和优先队列概念
- 使用 Go 优秀的标准库和测试工具构建
- 使用 [goconvey](https://github.com/smartystreets/goconvey) 进行 BDD 风格测试

---

**由 [szd-dev](https://github.com/szd-dev) 用 ❤️ 制作**
