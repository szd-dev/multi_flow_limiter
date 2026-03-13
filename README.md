# Multi-Flow Limiter

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Test Coverage](https://img.shields.io/badge/coverage-98.6%25-brightgreen.svg)](#test-coverage)

English | [中文](README_CN.md)

A high-performance, priority-based rate limiter with elastic capacity sharing for Go applications.

## Overview

Multi-Flow Limiter is a flexible rate limiting library that implements a multi-bucket token bucket algorithm with priority levels. It allows you to define multiple traffic classes with guaranteed minimum QPS while enabling elastic capacity sharing - higher priority buckets can utilize unused capacity from lower priority buckets.

### Key Features

- **Multi-Priority Buckets**: Define multiple traffic classes with different priority levels
- **Guaranteed Minimum QPS**: Each bucket gets a guaranteed minimum QPS based on weight allocation
- **Elastic Capacity Sharing**: Unused capacity flows to higher priority buckets automatically
- **Token Bucket Algorithm**: Lazy refill with configurable burst capacity
- **Thread-Safe**: Concurrent access safe with mutex protection
- **Zero Dependencies**: Only uses Go standard library and test framework
- **High Test Coverage**: 98.6% code coverage with comprehensive test suite

## Installation

```bash
go get github.com/szd-dev/multi_flow_limiter
```

## Quick Start

```go
package main

import (
    "fmt"
    "time"
    
    limiter "github.com/szd-dev/multi_flow_limiter"
)

func main() {
    // Create a multi-bucket limiter
    config := limiter.MultiFlowLimiterConfig{
        TotalQPS:     1000, // Total system QPS limit
        BurstSeconds: 1.0,  // Burst window in seconds (capacity = TotalQPS * BurstSeconds)
        Buckets: []limiter.BucketConfig{
            {Name: "critical", Weight: 3}, // High priority - 30% guaranteed
            {Name: "normal",   Weight: 5}, // Medium priority - 50% guaranteed
            {Name: "bulk",     Weight: 2}, // Low priority - 20% guaranteed
        },
    }
    
    rateLimiter := limiter.NewElasticMultiLimiter(config)
    
    // Check if request is allowed for a specific bucket
    if rateLimiter.Allow(0) { // bucket index 0 = "critical"
        fmt.Println("Critical request allowed")
    }
    
    // Monitor bucket stats
    fmt.Printf("Bucket count: %d\n", rateLimiter.BucketCount())
    fmt.Printf("Critical bucket name: %s\n", rateLimiter.BucketName(0))
    fmt.Printf("Critical bucket min QPS: %.2f\n", rateLimiter.MinQPS(0))
    fmt.Printf("System total QPS: %.2f\n", rateLimiter.LimitRate())
}
```

## Architecture

### Core Concepts

#### 1. Token Bucket with Lazy Refill

The limiter uses a token bucket algorithm with lazy refill. Tokens are replenished based on time elapsed since the last refill, calculated as:

```
tokens_to_add = elapsed_seconds * totalQPS
```

#### 2. Two-Phase Refill Algorithm

When tokens are replenished, the algorithm operates in two phases:

**Phase 1 - Guaranteed Allocation**:
- Each bucket receives tokens proportional to its weight
- Ensures every bucket gets its minimum guaranteed QPS
- Overflow from buckets reaching capacity returns to shared pool

**Phase 2 - Priority-Based Distribution**:
- Remaining tokens (from overflow or unused capacity) are distributed by priority
- Higher priority buckets (lower index) get first access to surplus tokens
- Allows buckets to burst beyond their guaranteed minimum

#### 3. Capacity Sharing

All buckets share the same maximum capacity (`cap = totalQPS * burstSeconds`):

- **Example**: If `totalQPS=1000` and `burstSeconds=2`, each bucket can hold up to 2000 tokens
- When a bucket doesn't use its tokens, they remain available for all buckets
- High priority buckets can consume the entire system capacity if others are idle

### Weight-Based QPS Allocation

Bucket weights are normalized to calculate minimum guaranteed QPS:

```
bucket_minQPS = totalQPS * (bucket_weight / sum_of_all_weights)
```

**Example**:
```
Total QPS: 1000
Buckets: [critical=3, normal=5, bulk=2]
Sum of weights: 10

Min QPS:
- critical: 1000 * (3/10) = 300 QPS guaranteed
- normal:   1000 * (5/10) = 500 QPS guaranteed
- bulk:     1000 * (2/10) = 200 QPS guaranteed
```

### Priority Order

Buckets are ordered by their position in the configuration array:
- **Index 0**: Highest priority (first to receive surplus tokens)
- **Index n-1**: Lowest priority (last to receive surplus tokens)

## API Reference

### Types

```go
type BucketConfig struct {
    Name   string  // Bucket name (identifier)
    Weight float64 // Weight ratio (used to calculate minQPS, auto-normalized)
}

type MultiFlowLimiterConfig struct {
    TotalQPS     int64          // Total system QPS limit
    BurstSeconds float64        // Burst window in seconds, capacity = TotalQPS * BurstSeconds
    Buckets      []BucketConfig // Bucket configuration, order = priority (index 0 highest)
}
```

### Constructor

```go
func NewElasticMultiLimiter(config MultiFlowLimiterConfig) *ElasticMultiLimiter
```

Creates a new elastic multi-bucket rate limiter.

**Configuration Validation**:
- `TotalQPS` must be >= 0 (panics if negative)
- `BurstSeconds` must be > 0 (panics if zero or negative)
- `Buckets` must not be empty (panics if empty)
- All `Weight` values must be > 0 (panics if zero or negative)

### Methods

```go
func (eml *ElasticMultiLimiter) Allow(bucketIdx int) bool
```
Attempts to consume 1 token from the specified bucket. Returns `true` if successful, `false` if denied or bucket index is out of bounds.

```go
func (eml *ElasticMultiLimiter) BucketCount() int
```
Returns the number of buckets.

```go
func (eml *ElasticMultiLimiter) BucketName(bucketIdx int) string
```
Returns the name of the specified bucket. Returns empty string if index is out of bounds.

```go
func (eml *ElasticMultiLimiter) Tokens() []float64
```
Returns current token count for all buckets. Useful for monitoring and debugging.

```go
func (eml *ElasticMultiLimiter) MinQPS(bucketIdx int) float64
```
Returns the minimum guaranteed QPS for the specified bucket. Returns 0 if index is out of bounds.

```go
func (eml *ElasticMultiLimiter) LimitRate() float64
```
Returns the system total QPS limit.

## Use Cases

### 1. API Rate Limiting with Priority Tiers

```go
config := limiter.MultiFlowLimiterConfig{
    TotalQPS:     10000,
    BurstSeconds: 2.0,
    Buckets: []limiter.BucketConfig{
        {Name: "premium",  Weight: 5}, // Premium users - 50% guaranteed
        {Name: "standard", Weight: 3}, // Standard users - 30% guaranteed
        {Name: "free",     Weight: 2}, // Free users - 20% guaranteed
    },
}
```

### 2. Microservice Traffic Shaping

```go
config := limiter.MultiFlowLimiterConfig{
    TotalQPS:     5000,
    BurstSeconds: 1.5,
    Buckets: []limiter.BucketConfig{
        {Name: "health-check", Weight: 1},  // Health checks - 10% guaranteed
        {Name: "read",         Weight: 6},  // Read operations - 60% guaranteed
        {Name: "write",        Weight: 3},  // Write operations - 30% guaranteed
    },
}
```

### 3. Background Job Processing

```go
config := limiter.MultiFlowLimiterConfig{
    TotalQPS:     500,
    BurstSeconds: 5.0, // Allow burst for batch processing
    Buckets: []limiter.BucketConfig{
        {Name: "realtime", Weight: 4}, // Real-time jobs - 40% guaranteed
        {Name: "scheduled", Weight: 3}, // Scheduled jobs - 30% guaranteed
        {Name: "batch",    Weight: 3}, // Batch jobs - 30% guaranteed
    },
}
```

## Test Coverage

The library has comprehensive test coverage with **98.6%** of statements covered.

### Coverage by Function

| Function | Coverage |
|----------|----------|
| `NewElasticMultiLimiter` | 100.0% |
| `refillLocked` | 96.7% |
| `Allow` | 100.0% |
| `BucketCount` | 100.0% |
| `BucketName` | 100.0% |
| `Tokens` | 100.0% |
| `MinQPS` | 100.0% |
| `LimitRate` | 100.0% |
| **Total** | **98.6%** |

### Test Categories

The test suite includes:

1. **Constructor Validation**: Tests for all panic conditions (negative QPS, invalid weights, etc.)
2. **MinQPS Allocation**: Verifies weight-based QPS distribution and normalization
3. **Basic Accessors**: Tests for all getter methods and boundary conditions
4. **Bounds Checking**: Validates bucket index range validation
5. **Edge Cases**: Zero QPS, empty buckets, concurrent access
6. **Token Refill**: Validates lazy refill and token consumption
7. **Guaranteed Minimum QPS**: Ensures each bucket gets its minimum under full load
8. **Priority Order**: Verifies high-priority buckets get surplus first
9. **Capacity Sharing**: Tests that all buckets share the same capacity limit
10. **Concurrency Safety**: 30 goroutines accessing simultaneously
11. **Manual Playground**: Interactive test for visualizing limiter behavior

### Running Tests

```bash
# Run all tests with coverage
go test -cover -coverprofile=coverage.out

# View coverage by function
go tool cover -func=coverage.out

# Generate HTML coverage report
go tool cover -html=coverage.out -o coverage.html

# Run verbose tests
go test -v

# Run specific test
go test -v -run TestElasticMultiLimiter_ManualPlayground -count=1
```

## Performance Characteristics

- **Time Complexity**: O(n) for token refill, where n is the number of buckets
- **Space Complexity**: O(n) for bucket storage
- **Concurrency**: Mutex-protected, safe for concurrent use
- **Memory**: Minimal overhead, only stores bucket state and timestamps

## Design Decisions

### Why Lazy Refill?

Lazy refill (calculating tokens on `Allow()` call) reduces CPU overhead compared to periodic background refill. This is especially efficient for:
- Low-frequency traffic patterns
- Burst-heavy workloads
- Systems with many idle buckets

### Why Same Capacity for All Buckets?

Using a shared capacity for all buckets enables:
- Elastic capacity sharing between buckets
- Simpler mental model (total capacity = totalQPS * burstSeconds)
- Better resource utilization (no wasted capacity in low-priority buckets)

### Why Panic on Invalid Config?

The limiter panics on invalid configuration because:
- Configuration errors are programmer mistakes, not runtime conditions
- Failing fast prevents silent misconfiguration in production
- Rate limiter misconfiguration can have severe system-wide impacts

## Benchmarks

Run benchmarks to measure performance:

```bash
go test -bench=. -benchmem
```

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Write tests for new functionality
4. Ensure all tests pass (`go test -v`)
5. Maintain test coverage above 95%
6. Commit changes (`git commit -m 'Add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Inspired by the token bucket algorithm and priority queue concepts
- Built with Go's excellent standard library and testing tools
- Uses [goconvey](https://github.com/smartystreets/goconvey) for BDD-style testing

## Status

This project is actively maintained. For production use:
- ✅ Comprehensive test coverage (98.6%)
- ✅ Thread-safe implementation
- ✅ Well-documented API
- ✅ Zero external runtime dependencies

---

**Made with ❤️ by [szd-dev](https://github.com/szd-dev)**
