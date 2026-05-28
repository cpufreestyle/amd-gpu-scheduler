# 🎮 AMD GPU Scheduler

混合 GPU 智能调度器 — 支持 NVIDIA + AMD 异构 GPU 环境的任务调度与管理。

## ✨ Features

- **异构 GPU 调度** — 同时管理 NVIDIA 和 AMD GPU，智能分配任务
- **多策略调度** — Binpack（集中）、Spread（均衡）、GPU Type（类型优先）
- **优先级抢占** — 高优先级任务可抢占低优先级任务的 GPU 资源
- **任务执行器** — 真实进程管理、超时控制、日志记录
- **Prometheus Metrics** — 内置 `/metrics` 端点，对接 Prometheus 监控
- **实时 Dashboard** — Web 可视化面板，实时展示 GPU 状态和任务队列
- **REST API** — 完整的 HTTP API，支持任务提交、GPU 查询、策略切换

## 🏗️ Architecture

```
hybrid-gpu-scheduler/
├── cmd/server/          # HTTP 服务器入口
│   ├── main.go          # 路由、中间件、API 注册
│   └── preempt_api.go   # 抢占式调度 API
├── internal/
│   ├── scheduler/       # 调度核心
│   │   ├── scheduler.go       # 主调度器（评分算法、策略选择）
│   │   ├── hybrid_scheduler.go # 混合 GPU 调度逻辑
│   │   ├── preempt.go         # 优先级抢占
│   │   └── gpu_update.go      # GPU 使用量更新
│   ├── executor/        # 任务执行器
│   │   └── executor.go  # 进程管理、超时控制、进程树终止
│   ├── gpumonitor/      # GPU 监控
│   │   └── monitor.go   # nvidia-smi / rocm-smi 集成
│   ├── gpu_manager/     # GPU 资源管理
│   │   └── manager.go   # GPU 初始化、状态维护
│   └── metrics/         # Prometheus 指标
│       └── metrics.go   # 五类监控指标定义
├── pkg/
│   ├── gpu/gpu.go       # NVIDIA GPU 操作
│   ├── roc/             # AMD ROCm 操作（跨平台构建）
│   └── types/           # 数据类型定义
├── dashboard.html       # Web 可视化面板
└── dashboard-520.html   # 520 浪漫主题面板（彩蛋）
```

## 🚀 Quick Start

### 编译

```bash
# 需要 Go 1.22+
go build -o scheduler ./cmd/server/
```

### 运行

```bash
./scheduler
```

默认监听 `http://localhost:8080`。

### 下载 Release

前往 [Releases](https://gitee.com/cpufreestyle/hybrid-gpu-scheduler/releases) 下载编译好的可执行文件。

## 📡 API

### 基础端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/metrics` | GET | Prometheus 格式指标 |
| `/dashboard` | GET | Web 可视化面板 |

### GPU 管理

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/gpus` | GET | 获取所有 GPU 状态 |
| `/api/gpus/:id` | GET | 获取单个 GPU 详情 |

### 任务管理

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/tasks` | GET | 获取所有任务列表 |
| `/api/tasks` | POST | 提交新任务 |
| `/api/tasks/:id` | GET | 获取任务详情 |
| `/api/tasks/:id/stop` | POST | 停止运行中的任务 |
| `/api/tasks/:id/logs` | GET | 获取任务日志 |

### 调度策略

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/scheduler/policy` | GET | 获取当前策略 |
| `/api/scheduler/policy` | POST | 切换调度策略 |

### 抢占式调度

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/scheduler/preempt/history` | GET | 抢占历史 |
| `/api/scheduler/preempt/config` | GET | 抢占配置 |
| `/api/scheduler/preempt/config` | POST | 更新配置 |
| `/api/scheduler/preempt` | POST | 手动触发抢占 |

### 提交任务示例

```bash
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "training-job",
    "type": "training",
    "priority": 8,
    "vram_required_mb": 4096,
    "command": "python train.py",
    "timeout_seconds": 3600
  }'
```

## 🧠 Scheduling Algorithm

调度器采用**多因素打分机制**：

| 因素 | 权重 | 说明 |
|------|------|------|
| VRAM 剩余 | 30% | 可用显存越多分数越高 |
| GPU 利用率 | 25% | 利用率越低分数越高 |
| GPU 类型匹配 | 20% | training→NVIDIA, inference→AMD |
| 任务优先级 | 15% | 高优先级优先调度 |
| 负载均衡 | 10% | 避免单 GPU 过载 |

### 调度策略

| 策略 | 说明 |
|------|------|
| **Binpack** | 集中调度，尽量用少的 GPU 承载多的任务 |
| **Spread** | 分散调度，负载均衡，避免单点过载 |
| **GPU Type** | 类型优先，根据任务类型自动匹配 GPU |

### 抢占规则

- 触发条件：高优先级任务（Priority ≥ 7）且所有 GPU 资源不足
- 抢占目标：低优先级任务（Priority ≤ 5）
- 策略：`kill`（直接终止）、`wait`（等待完成）、`checkpoint`（保存状态）

## 📊 Prometheus Metrics

访问 `/metrics` 获取以下指标：

| 指标 | 类型 | 说明 |
|------|------|------|
| `gpu_vram_used_mb` | Gauge | GPU 已用显存 |
| `gpu_vram_total_mb` | Gauge | GPU 总显存 |
| `gpu_utilization_percent` | Gauge | GPU 利用率 |
| `gpu_running_tasks` | Gauge | 运行中任务数 |
| `task_submitted_total` | Counter | 已提交任务总数 |
| `task_duration_seconds` | Histogram | 任务执行时长 |
| `scheduler_decisions_total` | Counter | 调度决策次数 |
| `http_request_duration_seconds` | Histogram | HTTP 请求延迟 |

## 🔧 配置

调度器默认配置（在 `cmd/server/main.go` 中）：

```go
// GPU 初始化
GPU 0: NVIDIA GeForce RTX 5070 Ti (16GB)
GPU 1: AMD Radeon RX 7900 XTX (24GB)

// 抢占配置
MinPriorityToPreempt:    7
MaxPriorityToBePreempted: 5
PreemptPolicy:           kill
```

## 💻 Supported Platforms

| 平台 | 架构 | 状态 |
|------|------|------|
| Windows | amd64 | ✅ 完全支持 |
| Linux | amd64 | ✅ 支持（含 ROCm） |

> Windows 下 ROCm 功能不可用，AMD GPU 使用模拟数据。安装 ROCm 驱动后 Linux 版本可读取真实 AMD GPU 数据。

## 🛠️ Tech Stack

- **语言**: Go 1.22+
- **Web 框架**: Gin
- **监控**: Prometheus client_golang
- **前端**: HTML + CSS + Chart.js
- **GPU 监控**: nvidia-smi / rocm-smi

## 📊 Performance Benchmark

> Tested on: Intel i7-6700 @ 3.40GHz | RTX 5070 Ti 16GB + RX 7900 XTX 24GB | Go 1.23 | Windows x64

### Scheduling Algorithm (pure compute, no I/O)

| Benchmark | Time/op | Throughput |
|-----------|---------|------------|
| `Binpack` policy score calc | **8.2 ns/op** | ~121 M ops/sec |
| `Spread` policy score calc | **7.4 ns/op** | ~135 M ops/sec |
| `GPUType` policy score calc | **14.7 ns/op** | ~68 M ops/sec |
| `Scheduler_CalculateScore` (with mutex) | **~15 ns/op** | ~67 M ops/sec |

**结论：** 调度算法本身极快（纳秒级），调度开销可忽略不计。

### Full SubmitTask Cycle (with Windows process spawn)

| Benchmark | Time/op | Memory/op | Allocations |
|-----------|---------|-----------|------------|
| `SubmitTask` (echo ok) | **~16.7 ms/op** | ~41 KB/op | 382 allocs/op |

**结论：** 真实任务提交的瓶颈是 Windows 进程创建，而非调度算法。

运行命令：
```bash
go test ./internal/scheduler/ -bench=. -benchmem -benchtime=3s
```

---

## 📄 License

MIT License

---

<p align="center">
  Made with ❤️ by <a href="https://gitee.com/cpufreestyle">cpufreestyle</a>
</p>

