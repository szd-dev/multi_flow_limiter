package multi_flow_limiter

import (
	"math"
	"sync"
	"time"
)

// BucketConfig 定义一个优先级桶的配置。
// Buckets 在列表中的顺序即为优先级顺序，index 0 为最高优先级。
type BucketConfig struct {
	Name   string  // 桶名称（标识用）
	Weight float64 // 权重比例（用于计算 minQPS，自动归一化）
}

// MultiFlowLimiterConfig 弹性共享多桶限流器的配置。
type MultiFlowLimiterConfig struct {
	TotalQPS     int64          // 系统总 QPS 上限
	BurstSeconds float64        // 突发窗口（秒），cap = TotalQPS * BurstSeconds
	Buckets      []BucketConfig // 桶配置列表，顺序 = 优先级（index 0 最高）
}

type elasticBucket struct {
	name   string
	minQPS float64 // 最低保障 QPS
	tokens float64 // 当前可用令牌
	cap    float64 // 容量上限（所有桶相同 = totalQPS * burstSeconds）
}

// MultiFlowLimiter: 弹性共享多桶限流器。
//
// 核心机制：
//   - 每个桶有 minQPS 保障（按权重比例分配自总 QPS）
//   - 所有桶共享同一个 cap（= totalQPS * burstSeconds）
//   - 两阶段填充：先保障所有桶的 minQPS，再按优先级分配剩余令牌
//   - 单桶可突破自身 minQPS，最高使用系统总 QPS
//
// Refill 采用懒填充，在 Allow() 调用时基于时间差计算。
type MultiFlowLimiter struct {
	mu sync.Mutex

	totalQPS float64         // 系统总 QPS
	buckets  []elasticBucket // 桶列表（index 0 = 最高优先级）

	lastNs int64 // 上次填充时间（unix nano）
}

// NewMultiFlowLimiter 创建弹性共享多桶限流器。
//
// 配置校验规则：
//   - TotalQPS >= 0
//   - BurstSeconds > 0
//   - Buckets 不能为空
//   - 所有 Weight > 0
func NewMultiFlowLimiter(config MultiFlowLimiterConfig) *MultiFlowLimiter {
	if config.TotalQPS < 0 {
		panic("ElasticMultiLimiter: TotalQPS must be >= 0")
	}
	if config.BurstSeconds <= 0 {
		panic("ElasticMultiLimiter: BurstSeconds must be > 0")
	}
	if len(config.Buckets) == 0 {
		panic("ElasticMultiLimiter: Buckets must not be empty")
	}

	// 计算权重总和
	sumWeight := 0.0
	for _, b := range config.Buckets {
		if b.Weight <= 0 {
			panic("ElasticMultiLimiter: Weight must be > 0 for bucket: " + b.Name)
		}
		sumWeight += b.Weight
	}

	totalQPS := float64(config.TotalQPS)
	cap := totalQPS * config.BurstSeconds

	buckets := make([]elasticBucket, len(config.Buckets))
	for i, bc := range config.Buckets {
		buckets[i] = elasticBucket{
			name:   bc.Name,
			minQPS: totalQPS * (bc.Weight / sumWeight),
			tokens: 0,
			cap:    cap,
		}
	}

	return &MultiFlowLimiter{
		totalQPS: totalQPS,
		buckets:  buckets,
		lastNs:   time.Now().UnixNano(),
	}
}

// refillLocked 两阶段填充算法（调用方需持有 mu 锁）。
//
// 阶段一：按优先级顺序，将每个桶填充到 minQPS 对应的令牌数（保障最低额度）。
// 阶段二：按优先级顺序，将剩余令牌填充到各桶 cap（高优先级优先获得额外容量）。
func (eml *MultiFlowLimiter) refillLocked(nowNs int64) {
	if eml.totalQPS == 0 {
		eml.lastNs = nowNs
		return
	}

	delta := nowNs - eml.lastNs
	if delta <= 0 {
		return
	}

	add := float64(delta) * eml.totalQPS / 1e9
	eml.lastNs = nowNs

	if add <= 0 {
		return
	}

	deltaSec := float64(delta) / 1e9

	// ===== 阶段一：保障所有桶的 minQPS =====
	for i := range eml.buckets {
		b := &eml.buckets[i]
		fill := b.minQPS * deltaSec
		// 从总 add 中预先扣除分配的保障额度
		add -= fill
		b.tokens += fill
		// 如果超出 cap，超出部分加回共享池 (add)
		if b.tokens > b.cap {
			overflow := b.tokens - b.cap
			b.tokens = b.cap
			add += overflow
		}
	}

	// ===== 阶段二：按优先级分配剩余令牌 =====
	// 此时 add 包含：计算零头误差 + 其他桶溢出的空闲额度
	if add > 0 {
		for i := range eml.buckets {
			if add <= 0 {
				break
			}
			b := &eml.buckets[i]

			space := b.cap - b.tokens
			if space > 0 {
				fill := math.Min(space, add)
				b.tokens += fill
				add -= fill
			}
		}
	}
	// 剩余令牌丢弃（所有桶已满）
}

// Allow 尝试从指定桶消耗 1 个令牌。
// bucketIdx 为桶在配置中的索引（0 = 最高优先级）。
// 如果 bucketIdx 越界，返回 false。
func (eml *MultiFlowLimiter) Allow(bucketIdx int) bool {
	now := time.Now().UnixNano()

	eml.mu.Lock()
	defer eml.mu.Unlock()
	eml.refillLocked(now)

	if bucketIdx < 0 || bucketIdx >= len(eml.buckets) {
		return false
	}

	b := &eml.buckets[bucketIdx]
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// BucketCount 返回桶数量。
func (eml *MultiFlowLimiter) BucketCount() int {
	return len(eml.buckets)
}

// BucketName 返回指定桶的名称。
func (eml *MultiFlowLimiter) BucketName(bucketIdx int) string {
	if bucketIdx < 0 || bucketIdx >= len(eml.buckets) {
		return ""
	}
	return eml.buckets[bucketIdx].name
}

// Tokens 返回各桶当前令牌数（用于监控/调试）。
func (eml *MultiFlowLimiter) Tokens() []float64 {
	eml.mu.Lock()
	defer eml.mu.Unlock()

	result := make([]float64, len(eml.buckets))
	for i := range eml.buckets {
		result[i] = eml.buckets[i].tokens
	}
	return result
}

// MinQPS 返回指定桶的最低保障 QPS。
func (eml *MultiFlowLimiter) MinQPS(bucketIdx int) float64 {
	if bucketIdx < 0 || bucketIdx >= len(eml.buckets) {
		return 0
	}
	return eml.buckets[bucketIdx].minQPS
}

// LimitRate 返回系统总 QPS 上限。
func (eml *MultiFlowLimiter) LimitRate() float64 {
	return eml.totalQPS
}
