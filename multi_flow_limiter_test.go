package multi_flow_limiter

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewElasticMultiLimiter_Panics(t *testing.T) {
	Convey("测试ElasticMultiLimiter构造函数的参数校验", t, func() {
		Convey("TotalQPS为负数时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     -1,
					BurstSeconds: 1.0,
					Buckets:      []BucketConfig{{Name: "a", Weight: 1}},
				})
			}, ShouldPanic)
		})

		Convey("BurstSeconds为0时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     100,
					BurstSeconds: 0,
					Buckets:      []BucketConfig{{Name: "a", Weight: 1}},
				})
			}, ShouldPanic)
		})

		Convey("BurstSeconds为负数时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     100,
					BurstSeconds: -1,
					Buckets:      []BucketConfig{{Name: "a", Weight: 1}},
				})
			}, ShouldPanic)
		})

		Convey("Buckets为空时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     100,
					BurstSeconds: 1.0,
					Buckets:      []BucketConfig{},
				})
			}, ShouldPanic)
		})

		Convey("Weight为0时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     100,
					BurstSeconds: 1.0,
					Buckets:      []BucketConfig{{Name: "a", Weight: 0}},
				})
			}, ShouldPanic)
		})

		Convey("Weight为负数时panic", func() {
			So(func() {
				NewMultiFlowLimiter(MultiFlowLimiterConfig{
					TotalQPS:     100,
					BurstSeconds: 1.0,
					Buckets:      []BucketConfig{{Name: "a", Weight: -1}},
				})
			}, ShouldPanic)
		})
	})
}

func TestNewElasticMultiLimiter_MinQPS(t *testing.T) {
	Convey("测试minQPS按权重比例分配", t, func() {
		Convey("等权重分配", func() {
			limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
				TotalQPS:     300,
				BurstSeconds: 1.0,
				Buckets: []BucketConfig{
					{Name: "a", Weight: 1},
					{Name: "b", Weight: 1},
					{Name: "c", Weight: 1},
				},
			})

			So(limiter.BucketCount(), ShouldEqual, 3)
			So(limiter.MinQPS(0), ShouldEqual, 100)
			So(limiter.MinQPS(1), ShouldEqual, 100)
			So(limiter.MinQPS(2), ShouldEqual, 100)
		})

		Convey("不等权重分配", func() {
			limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
				TotalQPS:     1000,
				BurstSeconds: 1.0,
				Buckets: []BucketConfig{
					{Name: "critical", Weight: 3},
					{Name: "normal", Weight: 5},
					{Name: "bulk", Weight: 2},
				},
			})

			So(limiter.MinQPS(0), ShouldEqual, 300)
			So(limiter.MinQPS(1), ShouldEqual, 500)
			So(limiter.MinQPS(2), ShouldEqual, 200)
		})

		Convey("权重自动归一化", func() {
			limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
				TotalQPS:     1000,
				BurstSeconds: 1.0,
				Buckets: []BucketConfig{
					{Name: "a", Weight: 0.3},
					{Name: "b", Weight: 0.7},
				},
			})

			So(limiter.MinQPS(0), ShouldEqual, 300)
			So(limiter.MinQPS(1), ShouldEqual, 700)
		})
	})
}

func TestElasticMultiLimiter_BasicAccessors(t *testing.T) {
	Convey("测试基本访问方法", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     1000,
			BurstSeconds: 2.0,
			Buckets: []BucketConfig{
				{Name: "critical", Weight: 3},
				{Name: "normal", Weight: 7},
			},
		})

		Convey("BucketCount", func() {
			So(limiter.BucketCount(), ShouldEqual, 2)
		})

		Convey("BucketName", func() {
			So(limiter.BucketName(0), ShouldEqual, "critical")
			So(limiter.BucketName(1), ShouldEqual, "normal")
			So(limiter.BucketName(-1), ShouldEqual, "")
			So(limiter.BucketName(2), ShouldEqual, "")
		})

		Convey("LimitRate", func() {
			So(limiter.LimitRate(), ShouldEqual, 1000)
		})

		Convey("MinQPS越界返回0", func() {
			So(limiter.MinQPS(-1), ShouldEqual, 0)
			So(limiter.MinQPS(2), ShouldEqual, 0)
		})

		Convey("初始令牌为0", func() {
			tokens := limiter.Tokens()
			So(len(tokens), ShouldEqual, 2)
			So(tokens[0], ShouldEqual, 0)
			So(tokens[1], ShouldEqual, 0)
		})
	})
}

func TestElasticMultiLimiter_AllowBoundsCheck(t *testing.T) {
	Convey("测试Allow的越界检查", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     100,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "a", Weight: 1},
			},
		})

		So(limiter.Allow(-1), ShouldBeFalse)
		So(limiter.Allow(1), ShouldBeFalse)
		So(limiter.Allow(100), ShouldBeFalse)
	})
}

func TestElasticMultiLimiter_TotalQPSZero(t *testing.T) {
	Convey("TotalQPS为0时所有请求被拒绝", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     0,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "a", Weight: 1},
			},
		})

		time.Sleep(100 * time.Millisecond)
		So(limiter.Allow(0), ShouldBeFalse)
	})
}

func TestElasticMultiLimiter_RefillAndConsume(t *testing.T) {
	Convey("测试令牌填充和消耗", t, func() {
		Convey("等待后可以消耗令牌", func() {
			limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
				TotalQPS:     100,
				BurstSeconds: 1.0,
				Buckets: []BucketConfig{
					{Name: "a", Weight: 1},
				},
			})

			// 等待 500ms，预期填充约 50 个令牌
			time.Sleep(500 * time.Millisecond)

			consumed := 0
			for limiter.Allow(0) {
				consumed++
				if consumed > 200 {
					break
				}
			}

			// 应消耗约 50 个令牌（允许 20% 误差）
			So(consumed, ShouldBeGreaterThanOrEqualTo, 40)
			So(consumed, ShouldBeLessThanOrEqualTo, 60)
		})

		Convey("消耗后等待可以再次消耗", func() {
			limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
				TotalQPS:     100,
				BurstSeconds: 5.0,
				Buckets: []BucketConfig{
					{Name: "a", Weight: 1},
				},
			})

			// 等待 1 秒后耗尽
			time.Sleep(1 * time.Second)
			for limiter.Allow(0) {
			}

			// 再等 500ms
			time.Sleep(500 * time.Millisecond)

			consumed := 0
			for limiter.Allow(0) {
				consumed++
				if consumed > 200 {
					break
				}
			}

			// 约 50 个令牌
			So(consumed, ShouldBeGreaterThanOrEqualTo, 40)
			So(consumed, ShouldBeLessThanOrEqualTo, 60)
		})
	})
}

func TestElasticMultiLimiter_MinQPSGuarantee(t *testing.T) {
	Convey("测试minQPS保障：满载时每个桶至少获得minQPS", t, func() {
		// totalQPS=100, 3个桶 weight 3:5:2
		// minQPS: 30, 50, 20
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     100,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "high", Weight: 3},
				{Name: "mid", Weight: 5},
				{Name: "low", Weight: 2},
			},
		})

		var allowed [3]int64
		duration := 5 * time.Second
		interval := 1 * time.Millisecond
		done := make(chan struct{})

		// 所有桶同时满负荷请求
		for idx := 0; idx < 3; idx++ {
			go func(i int) {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						if limiter.Allow(i) {
							atomic.AddInt64(&allowed[i], 1)
						}
					}
				}
			}(idx)
		}

		time.Sleep(duration)
		close(done)
		time.Sleep(50 * time.Millisecond)

		a0 := atomic.LoadInt64(&allowed[0])
		a1 := atomic.LoadInt64(&allowed[1])
		a2 := atomic.LoadInt64(&allowed[2])
		total := a0 + a1 + a2

		t.Logf("high: %d, mid: %d, low: %d, total: %d", a0, a1, a2, total)

		// 总量应接近 100 * 5 = 500（允许 20% 误差）
		expectedTotal := int64(100 * 5)
		So(total, ShouldBeGreaterThanOrEqualTo, expectedTotal*8/10)
		So(total, ShouldBeLessThanOrEqualTo, expectedTotal*12/10)

		// 每个桶至少获得 minQPS * duration 的 70%（考虑调度误差）
		So(a0, ShouldBeGreaterThanOrEqualTo, int64(30*5*7/10)) // >= 105
		So(a1, ShouldBeGreaterThanOrEqualTo, int64(50*5*7/10)) // >= 175
		So(a2, ShouldBeGreaterThanOrEqualTo, int64(20*5*7/10)) // >= 70
	})
}

func TestElasticMultiLimiter_SingleBucketFullCapacity(t *testing.T) {
	Convey("测试单桶独占：其他桶空闲时单桶可用总QPS", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     100,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "high", Weight: 3},
				{Name: "mid", Weight: 5},
				{Name: "low", Weight: 2},
			},
		})

		// 只有 "high" 桶在请求
		var allowedHigh int64
		duration := 5 * time.Second
		interval := 1 * time.Millisecond
		done := make(chan struct{})

		// 提前让系统跑一段时间，让 mid 和 low 桶蓄满水，从而开始溢出到共享池
		// mid 攒满 100 需要 100/50 = 2 秒
		// low 攒满 100 需要 100/20 = 5 秒
		// 我们这里先休眠 5 秒，让所有桶都满了
		time.Sleep(5 * time.Second)

		// 清空刚才蓄水期间可能由 tick 带来的细微误差
		atomic.StoreInt64(&allowedHigh, 0)

		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if limiter.Allow(0) {
						atomic.AddInt64(&allowedHigh, 1)
					}
				}
			}
		}()

		time.Sleep(duration)
		close(done)
		time.Sleep(50 * time.Millisecond)

		high := atomic.LoadInt64(&allowedHigh)
		// 前面蓄满水后，mid 和 low 每秒产生的 70 qps 全部溢出进入共享池
		// high 在 5 秒内应该拿到:
		// 1. 开始时自己桶内已经蓄满的 cap = 100
		// 2. 5 秒内自己产生的 minQPS = 30 * 5 = 150
		// 3. 5 秒内从共享池抢到的 = 70 * 5 = 350
		// 总计 = 100 + 150 + 350 = 600
		t.Logf("high bucket alone after fully idle: %d (expected ~%d)", high, 600)

		expectedTotal := int64(600)
		So(high, ShouldBeGreaterThanOrEqualTo, expectedTotal*8/10)
		So(high, ShouldBeLessThanOrEqualTo, expectedTotal*12/10)
	})
}
func TestElasticMultiLimiter_PriorityOrder(t *testing.T) {
	Convey("测试优先级顺序：高优先级桶在剩余令牌分配中占优", t, func() {
		// 3个桶等权重 → minQPS 各 100
		// totalQPS = 300，没有剩余令牌
		// 改为 totalQPS = 500，minQPS 各约 166，剩余 ~168/s
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     500,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "high", Weight: 1},
				{Name: "mid", Weight: 1},
				{Name: "low", Weight: 1},
			},
		})

		// minQPS 各 ~166.67，总 ~500
		// 在满载下没有剩余，每个桶各得 ~166.67
		// 但如果 high 桶请求频率低于其 minQPS，剩余令牌会优先给 high（阶段二）
		// 这里验证：high 桶大量请求时能获得更多（因为阶段二按优先级填充）

		var allowed [3]int64
		duration := 5 * time.Second
		done := make(chan struct{})

		// high: 高频请求
		go func() {
			ticker := time.NewTicker(200 * time.Microsecond) // 5000/s 远超 minQPS
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if limiter.Allow(0) {
						atomic.AddInt64(&allowed[0], 1)
					}
				}
			}
		}()

		// mid/low: 低频请求（只用了 minQPS 的一半）
		for idx := 1; idx <= 2; idx++ {
			go func(i int) {
				ticker := time.NewTicker(12 * time.Millisecond) // ~83/s < minQPS ~166
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						if limiter.Allow(i) {
							atomic.AddInt64(&allowed[i], 1)
						}
					}
				}
			}(idx)
		}

		time.Sleep(duration)
		close(done)
		time.Sleep(50 * time.Millisecond)

		a0 := atomic.LoadInt64(&allowed[0])
		a1 := atomic.LoadInt64(&allowed[1])
		a2 := atomic.LoadInt64(&allowed[2])

		t.Logf("high: %d, mid: %d, low: %d", a0, a1, a2)

		// high 桶应获得显著多于 minQPS（因为 mid/low 没用完，剩余优先给 high）
		So(a0, ShouldBeGreaterThan, a1)
		So(a0, ShouldBeGreaterThan, a2)
	})
}

func TestElasticMultiLimiter_CapSharedAcrossBuckets(t *testing.T) {
	Convey("测试所有桶共享同一个cap", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     100,
			BurstSeconds: 2.0,
			Buckets: []BucketConfig{
				{Name: "a", Weight: 1},
				{Name: "b", Weight: 1},
			},
		})

		// cap = 100 * 2 = 200，所有桶共享
		// 桶 a 的 minQPS = 50。要攒满 200，需要 4 秒。
		// 我们等待 4.5 秒，确保它一定能攒满 200。
		time.Sleep(4500 * time.Millisecond)

		consumed := 0
		for limiter.Allow(0) {
			consumed++
			if consumed > 500 {
				break
			}
		}

		// 桶 a 应该最多消耗 cap=200 个令牌（不超过 cap 上限）
		t.Logf("consumed from bucket a after 4.5s idle: %d (cap=200)", consumed)
		So(consumed, ShouldBeGreaterThanOrEqualTo, 180)
		So(consumed, ShouldBeLessThanOrEqualTo, 220)
	})
}
func TestElasticMultiLimiter_ManyBuckets(t *testing.T) {
	Convey("测试支持任意多桶（10个桶）", t, func() {
		buckets := make([]BucketConfig, 10)
		for i := 0; i < 10; i++ {
			buckets[i] = BucketConfig{
				Name:   string(rune('A' + i)),
				Weight: float64(10 - i), // A=10, B=9, ... J=1
			}
		}

		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     1000,
			BurstSeconds: 1.0,
			Buckets:      buckets,
		})

		So(limiter.BucketCount(), ShouldEqual, 10)

		// sumWeight = 10+9+...+1 = 55
		// 桶 A 的 minQPS = 1000 * 10/55 ≈ 181.8
		// 桶 J 的 minQPS = 1000 * 1/55 ≈ 18.2
		So(math.Abs(limiter.MinQPS(0)-1000*10.0/55) < 0.01, ShouldBeTrue)
		So(math.Abs(limiter.MinQPS(9)-1000*1.0/55) < 0.01, ShouldBeTrue)

		// 等待后所有桶都能消耗令牌
		time.Sleep(500 * time.Millisecond)
		for i := 0; i < 10; i++ {
			So(limiter.Allow(i), ShouldBeTrue)
		}
	})
}

func TestElasticMultiLimiter_ConcurrentSafety(t *testing.T) {
	Convey("测试并发安全性", t, func() {
		limiter := NewMultiFlowLimiter(MultiFlowLimiterConfig{
			TotalQPS:     10000,
			BurstSeconds: 1.0,
			Buckets: []BucketConfig{
				{Name: "a", Weight: 1},
				{Name: "b", Weight: 1},
				{Name: "c", Weight: 1},
			},
		})

		done := make(chan struct{})
		var total int64

		// 30 个并发 goroutine 同时请求
		for g := 0; g < 30; g++ {
			go func(idx int) {
				bucketIdx := idx % 3
				for {
					select {
					case <-done:
						return
					default:
						if limiter.Allow(bucketIdx) {
							atomic.AddInt64(&total, 1)
						}
					}
				}
			}(g)
		}

		time.Sleep(2 * time.Second)
		close(done)
		time.Sleep(50 * time.Millisecond)

		finalTotal := atomic.LoadInt64(&total)
		t.Logf("total allowed in 2s: %d (expected ~%d)", finalTotal, 10000*2)

		// 总量应接近 10000 * 2 = 20000（允许 20% 误差）
		So(finalTotal, ShouldBeGreaterThanOrEqualTo, 20000*8/10)
		So(finalTotal, ShouldBeLessThanOrEqualTo, 20000*12/10)
	})
}

// TestElasticMultiLimiter_ManualPlayground 是一个用于人工验证限流器效果的沙盒测试。
// 可以通过命令行指定运行:
// go test -v ./pkg/ratelimiter -run TestElasticMultiLimiter_ManualPlayground -count=1
func TestElasticMultiLimiter_ManualPlayground(t *testing.T) {
	// =============== 调整这里的配置进行实验 ===============
	config := MultiFlowLimiterConfig{
		TotalQPS:     1000,
		BurstSeconds: 1.0,
		Buckets: []BucketConfig{
			{Name: "L0(High)", Weight: 40},
			{Name: "L1(Mid)", Weight: 30},
			{Name: "L2(Low)", Weight: 30},
		},
	}

	// 调整各个桶的实际请求频率 (请求间隔越短，频率越高)
	// 例如: 1ms = 1000 QPS, 5ms = 200 QPS, 10ms = 100 QPS
	requestIntervals := []time.Duration{
		3 * time.Millisecond, // L0 请求频率: 1000 QPS (满载)
		5 * time.Millisecond, // L1 请求频率: 200 QPS
		1 * time.Millisecond, // L2 请求频率: 100 QPS
	}

	// 测试持续时间
	testDuration := 10 * time.Second
	// ========================================================

	if len(requestIntervals) != len(config.Buckets) {
		t.Fatalf("requestIntervals 长度 (%d) 必须与 Buckets 数量 (%d) 一致",
			len(requestIntervals), len(config.Buckets))
	}

	limiter := NewMultiFlowLimiter(config)
	t.Logf("=== 限流器配置 ===")
	t.Logf("总 QPS: %d, Burst: %.1fs, Cap: %.0f",
		config.TotalQPS, config.BurstSeconds, float64(config.TotalQPS)*config.BurstSeconds)
	for i := 0; i < limiter.BucketCount(); i++ {
		t.Logf("桶[%d] %s: 保障 MinQPS=%.1f, 请求间隔=%v (理论请求QPS=%.0f)",
			i, limiter.BucketName(i), limiter.MinQPS(i),
			requestIntervals[i], float64(time.Second)/float64(requestIntervals[i]))
	}
	// 等待填充
	// 为了防止空闲额度被长久缓存，在测试开始前我们先预先填充所有的桶到上限，
	// 这样任何 L0 和 L1 没吃完的增量，都会立刻瞬间溢出给 L2！
	t.Logf("等待蓄水（5秒），使得所有桶达到满载溢出状态...")
	time.Sleep(5 * time.Second)
	t.Logf("===================")

	allowedCount := make([]int64, limiter.BucketCount())
	requestCount := make([]int64, limiter.BucketCount())
	done := make(chan struct{})

	// 为每个桶启动请求 goroutine
	for i := 0; i < limiter.BucketCount(); i++ {
		go func(bucketIdx int, interval time.Duration) {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					atomic.AddInt64(&requestCount[bucketIdx], 1)
					if limiter.Allow(bucketIdx) {
						atomic.AddInt64(&allowedCount[bucketIdx], 1)
					}
				}
			}
		}(i, requestIntervals[i])
	}

	t.Logf("开始执行，将运行 %v...", testDuration)
	time.Sleep(testDuration)
	close(done)
	time.Sleep(100 * time.Millisecond)

	t.Logf("")
	t.Logf("=== 执行结果统计 (运行 %v) ===", testDuration)
	var totalAllowed int64
	for i := 0; i < limiter.BucketCount(); i++ {
		reqs := atomic.LoadInt64(&requestCount[i])
		allowed := atomic.LoadInt64(&allowedCount[i])
		totalAllowed += allowed

		qps := float64(allowed) / testDuration.Seconds()
		passRate := float64(0)
		if reqs > 0 {
			passRate = float64(allowed) / float64(reqs) * 100
		}

		t.Logf("桶[%d] %-10s -> 请求数: %-6d | 通过数: %-6d | 实际QPS: %-7.1f | 通过率: %.1f%%",
			i, limiter.BucketName(i), reqs, allowed, qps, passRate)
	}
	t.Logf("=============================")
	t.Logf("系统总计通过: %d (实际总 QPS: %.1f, 理论上限: %d)",
		totalAllowed, float64(totalAllowed)/testDuration.Seconds(), config.TotalQPS)
}
