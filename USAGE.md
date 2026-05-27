# 📖 Hybrid GPU Scheduler 使用文档

> **版本:** v1.0.0 | **更新:** 2026-05-23 | **作者:** cpufreestyle

---

## 📋 目录

1. [项目简介](#项目简介)
2. [快速开始](#快速开始)
3. [安装部署](#安装部署)
4. [配置说明](#配置说明)
5. [API 参考](#api-参考)
6. [Dashboard 使用](#dashboard-使用)
7. [高级功能](#高级功能)
8. [故障排查](#故障排查)
9. [开发指南](#开发指南)

---

## 项目简介

**Hybrid GPU Scheduler** 是一个支持 **NVIDIA + AMD 异构 GPU** 环境的智能任务调度器，适用于深度学习训练、推理、科学计算等场景。

### 核心特性

- ✅ **异构 GPU 调度** — 同时管理 NVIDIA (CUDA) 和 AMD (ROCm) GPU
- ✅ **多策略调度** — Binpack / Spread / GPU Type 三种策略
- ✅ **优先级抢占** — 高优先级任务可抢占低优先级任务资源
- ✅ **真实进程执行** — 支持超时控制、进程树终止、日志记录
- ✅ **GPU 隔离** — 通过环境变量注入，任务只看到 assigned GPU
- ✅ **Prometheus 监控** — 内置 `/metrics` 端点
- ✅ **实时 Dashboard** — Web 可视化面板 (Chart.js)
- ✅ **REST API** — 完整的 HTTP API

### 系统要求

- **操作系统:** Windows 10+ / Linux (Ubuntu 20.04+)
- **GPU:** NVIDIA (Compute Capability 7.0+) 或 AMD (ROCm 支持)
- **运行环境:**
  - Go 1.22+ (仅编译需要)
  - NVIDIA: nvidia-smi (驱动 520+)
  - AMD: rocm-smi (ROCm 5.0+)
  - Python 3.8+ (可选，用于运行 ML 任务)

---

## 快速开始

### 1. 下载预编译版本

**Windows:**
```powershell
# 从 GitHub Releases 下载
Invoke-WebRequest -Uri "https://github.com/cpufreestyle/hybrid-gpu-scheduler/releases/latest/download/scheduler-windows-amd64.exe" -OutFile "scheduler.exe"
```

**Linux:**
```bash
wget https://github.com/cpufreestyle/hybrid-gpu-scheduler/releases/latest/download/scheduler-linux-amd64 -O scheduler
chmod +x scheduler
```

### 2. 启动调度器

**Windows:**
```powershell
.\scheduler.exe
```

**Linux:**
```bash
./scheduler
```

### 3. 验证运行

```bash
# 健康检查
curl http://localhost:8080/health

# 查看 GPU 状态
curl http://localhost:8080/api/gpus

# 打开 Dashboard
start http://localhost:8080/dashboard   # Windows
xdg-open http://localhost:8080/dashboard  # Linux
```

---

## 安装部署

### 方式一：预编译 Binary (推荐)

#### Windows 部署

1. **下载 binary:**
```powershell
$url = "https://github.com/cpufreestyle/hybrid-gpu-scheduler/releases/download/v1.0.0/scheduler-windows-amd64.exe"
Invoke-WebRequest -Uri $url -OutFile "C:\Program Files\HybridGPU\scheduler.exe"
```

2. **创建配置文件** (`config.json`):
```json
{
  "port": 8080,
  "gpu_check_interval": 5,
  "log_dir": "C:\\Program Files\\HybridGPU\\logs",
  "default_policy": "binpack"
}
```

3. **安装为服务** (可选):
```powershell
# 使用 NSSM (Non-Sucking Service Manager)
nssm install HybridGPUScheduler "C:\Program Files\HybridGPU\scheduler.exe"
nssm start HybridGPUScheduler
```

#### Linux 部署

1. **下载 binary:**
```bash
sudo mkdir -p /opt/hybrid-gpu-scheduler
cd /opt/hybrid-gpu-scheduler
wget https://github.com/cpufreestyle/hybrid-gpu-scheduler/releases/download/v1.0.0/scheduler-linux-amd64 -O scheduler
chmod +x scheduler
```

2. **创建 systemd 服务** (`/etc/systemd/system/hybrid-gpu-scheduler.service`):
```ini
[Unit]
Description=Hybrid GPU Scheduler
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/hybrid-gpu-scheduler
ExecStart=/opt/hybrid-gpu-scheduler/scheduler
Restart=always
RestartSec=5
Environment="GIN_MODE=release"

[Install]
WantedBy=multi-user.target
```

3. **启动服务:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable hybrid-gpu-scheduler
sudo systemctl start hybrid-gpu-scheduler
sudo systemctl status hybrid-gpu-scheduler
```

### 方式二：从源码编译

```bash
# 克隆仓库
git clone https://github.com/cpufreestyle/hybrid-gpu-scheduler.git
cd hybrid-gpu-scheduler

# 安装依赖
go mod download

# 编译 (Windows)
go build -o bin/scheduler.exe ./cmd/server/

# 编译 (Linux)
GOOS=linux GOARCH=amd64 go build -o bin/scheduler ./cmd/server/

# 运行
./bin/scheduler.exe  # Windows
./bin/scheduler      # Linux
```

---

## 配置说明

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-port` | `8080` | HTTP 服务端口 |
| `-policy` | `binpack` | 默认调度策略 |
| `-interval` | `5` | GPU 状态检查间隔 (秒) |
| `-log-dir` | `./logs` | 日志目录 |

**示例:**
```bash
./scheduler -port 9090 -policy spread -interval 10 -log-dir /var/log/gpu-scheduler
```

### 环境变量

| 变量 | 说明 | 示例 |
|------|------|------|
| `GIN_MODE` | Gin 框架模式 (`debug`/`release`) | `GIN_MODE=release` |
| `CUDA_VISIBLE_DEVICES` | NVIDIA GPU 可见性 (由调度器自动设置) | `CUDA_VISIBLE_DEVICES=0` |
| `ROCR_VISIBLE_DEVICES` | AMD GPU 可见性 (由调度器自动设置) | `ROCR_VISIBLE_DEVICES=1` |

### 调度策略详解

#### 1. Binpack (集中打包)
- **策略:** 优先将任务分配到已使用的 GPU，提高 GPU 利用率
- **适用场景:** 需要提高 GPU 利用率，减少空闲 GPU
- **评分公式:** `score = 100 - utilization * 0.5 - (1 - vram_usage_pct) * 20`

#### 2. Spread (分散均衡)
- **策略:** 优先将任务分配到空闲 GPU，均衡负载
- **适用场景:** 需要均衡负载，避免热点 GPU 过热
- **评分公式:** `score = 100 - utilization * 0.3 - vram_usage_pct * 0.3`

#### 3. GPU Type (类型优先)
- **策略:** 根据任务类型偏好分配 (training → NVIDIA, inference → AMD)
- **适用场景:** 混合训练+推理场景，发挥不同 GPU 优势
- **NVIDIA 偏好:** `score += 20` (如果任务类型是 training)
- **AMD 偏好:** `score += 20` (如果任务类型是 inference)

---

## API 参考

### 基础信息

- **Base URL:** `http://localhost:8080`
- **Content-Type:** `application/json`
- **认证:** 暂无 (计划支持 API Key)

---

### 1. 健康检查

**请求:**
```http
GET /health
```

**响应:**
```json
{
  "status": "ok",
  "service": "hybrid-gpu-scheduler",
  "version": "2.0",
  "gpus": 2
}
```

---

### 2. GPU 相关 API

#### 2.1 获取所有 GPU

**请求:**
```http
GET /api/gpus
```

**响应:**
```json
{
  "success": true,
  "gpus": [
    {
      "id": "gpu-0",
      "name": "NVIDIA GeForce RTX 5070 Ti",
      "type": "nvidia",
      "total_vram_mb": 16384,
      "used_vram_mb": 2048,
      "utilization": 45.5,
      "temperature": 65,
      "power_w": 200,
      "status": "ready"
    },
    {
      "id": "gpu-1",
      "name": "AMD Radeon RX 7900 XTX",
      "type": "amd",
      "total_vram_mb": 24576,
      "used_vram_mb": 1024,
      "utilization": 20.0,
      "temperature": 55,
      "power_w": 180,
      "status": "ready"
    }
  ]
}
```

---

### 3. 任务相关 API

#### 3.1 提交任务

**请求:**
```http
POST /api/tasks
Content-Type: application/json

{
  "name": "train-resnet50",
  "type": "training",
  "priority": 8,
  "gpu_req": 1,
  "vram_mb": 8192,
  "command": "python",
  "args": ["train.py", "--epochs", "100"],
  "env": {
    "PYTHONUNBUFFERED": "1"
  },
  "work_dir": "/home/user/project",
  "timeout": 3600
}
```

**字段说明:**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | 任务名称 (唯一) |
| `type` | string | ✅ | 任务类型: `training`/`inference`/`compute` |
| `priority` | int | ❌ | 优先级 1-10 (默认 5) |
| `gpu_req` | int | ✅ | 需要的 GPU 数量 |
| `vram_mb` | int | ✅ | 需要的显存 (MB) |
| `command` | string | ✅ | 执行命令 |
| `args` | string[] | ❌ | 命令参数 |
| `env` | object | ❌ | 环境变量 |
| `work_dir` | string | ❌ | 工作目录 |
| `timeout` | int | ❌ | 超时时间 (秒, 0=无限制) |

**响应:**
```json
{
  "success": true,
  "task_id": "task-1234567890",
  "task": {
    "id": "task-1234567890",
    "name": "train-resnet50",
    "status": "running",
    "gpu_id": "gpu-0",
    "pid": 12345,
    "start_time": "2026-05-23T15:30:00Z"
  }
}
```

#### 3.2 查询所有任务

**请求:**
```http
GET /api/tasks
```

**响应:**
```json
{
  "success": true,
  "tasks": [
    {
      "id": "task-1234567890",
      "name": "train-resnet50",
      "type": "training",
      "status": "running",
      "gpu_id": "gpu-0",
      "priority": 8,
      "vram_mb": 8192,
      "pid": 12345,
      "start_time": "2026-05-23T15:30:00Z",
      "log_file": "logs/task-1234567890.log"
    }
  ]
}
```

#### 3.3 查询单个任务

**请求:**
```http
GET /api/tasks/{task_id}
```

**响应:**
```json
{
  "success": true,
  "task": {
    "id": "task-1234567890",
    "name": "train-resnet50",
    "status": "completed",
    "gpu_id": "gpu-0",
    "exit_code": 0,
    "start_time": "2026-05-23T15:30:00Z",
    "end_time": "2026-05-23T16:15:00Z"
  }
}
```

#### 3.4 停止任务

**请求:**
```http
POST /api/tasks/{task_id}/stop
```

**响应:**
```json
{
  "success": true,
  "message": "Task stopped",
  "task": {
    "id": "task-1234567890",
    "status": "killed"
  }
}
```

⚠️ **注意:** `DELETE /api/tasks/{task_id}` 只处理**排队中**的任务，运行中的任务需用 `POST /api/tasks/{task_id}/stop`。

#### 3.5 删除任务

**请求:**
```http
DELETE /api/tasks/{task_id}
```

**说明:** 只能删除已完成的任务 (`completed`/`failed`/`killed`/`timeout`)

---

### 4. 调度器 API

#### 4.1 获取调度器状态

**请求:**
```http
GET /api/scheduler/status
```

**响应:**
```json
{
  "success": true,
  "policy": "binpack",
  "pending_tasks": 2,
  "running_tasks": 1,
  "gpu_usage": [
    {"gpu_id": "gpu-0", "usage": 0.85},
    {"gpu_id": "gpu-1", "usage": 0.20}
  ]
}
```

#### 4.2 切换调度策略

**请求:**
```http
PUT /api/scheduler/policy
Content-Type: application/json

{
  "policy": "spread"
}
```

**响应:**
```json
{
  "success": true,
  "policy": "spread",
  "message": "Policy updated"
}
```

---

### 5. Prometheus Metrics

**请求:**
```http
GET /metrics
```

**示例输出:**
```
# HELP gpu_scheduler_tasks_total Total number of tasks
# TYPE gpu_scheduler_tasks_total counter
gpu_scheduler_tasks_total{status="completed"} 42
gpu_scheduler_tasks_total{status="failed"} 3
gpu_scheduler_tasks_total{status="killed"} 1

# HELP gpu_scheduler_gpu_utilization GPU utilization percentage
# TYPE gpu_scheduler_gpu_utilization gauge
gpu_scheduler_gpu_utilization{gpu_id="gpu-0"} 45.5
gpu_scheduler_gpu_utilization{gpu_id="gpu-1"} 20.0

# HELP gpu_scheduler_gpu_vram_used GPU VRAM used in MB
# TYPE gpu_scheduler_gpu_vram_used gauge
gpu_scheduler_gpu_vram_used{gpu_id="gpu-0"} 8192
gpu_scheduler_gpu_vram_used{gpu_id="gpu-1"} 2048
```

---

## Dashboard 使用

### 访问 Dashboard

打开浏览器，访问: http://localhost:8080/dashboard

### 功能介绍

#### 1. GPU 监控卡片

- **GPU 名称:** NVIDIA RTX 5070 Ti / AMD RX 7900 XTX
- **显存进度条:** 已用/总量 (MB)
- **利用率:** 实时 GPU 计算利用率 (%)
- **温度:** 当前温度 (°C)
- **功耗:** 当前功耗 (W)

#### 2. 利用率图表 (Chart.js)

- **实时折线图:** 显示所有 GPU 的利用率历史 (最近 60 个数据点)
- **自动刷新:** 每 5 秒更新一次
- **颜色区分:**
  - 🟦 蓝色: NVIDIA GPU
  - 🟥 红色: AMD GPU

#### 3. 任务管理表格

| 列名 | 说明 |
|------|------|
| ID | 任务唯一 ID |
| Name | 任务名称 |
| Type | 任务类型 (training/inference/compute) |
| Status | 状态 (pending/running/completed/failed/killed/timeout) |
| GPU | 分配的 GPU ID |
| Priority | 优先级 (1-10) |
| VRAM | 显存占用 (MB) |
| PID | 进程 ID |
| Start Time | 启动时间 |
| Actions | 操作按钮 (Stop / Delete / Log) |

**操作按钮:**
- 🔴 **Stop:** 停止运行中的任务 (调用 `POST /api/tasks/{id}/stop`)
- 🗑️ **Delete:** 删除已完成的任务 (调用 `DELETE /api/tasks/{id}`)
- 📄 **Log:** 查看任务日志 (弹出模态框，显示日志内容)

#### 4. 策略切换

- **Binpack:** 集中打包 (提高利用率)
- **Spread:** 分散均衡 (均衡负载)
- **GPU Type:** 类型优先 (training→NVIDIA, inference→AMD)

点击按钮即时切换，无需刷新页面。

---

## 高级功能

### 1. GPU 隔离机制

调度器通过环境变量注入实现 GPU 隔离：

- **NVIDIA:** `CUDA_VISIBLE_DEVICES=<gpu_id>`
- **AMD:** `ROCR_VISIBLE_DEVICES=<gpu_id>`

**验证隔离:**
```python
# test_gpu_isolation.py
import os
print("CUDA_VISIBLE_DEVICES:", os.environ.get("CUDA_VISIBLE_DEVICES"))
print("ROCR_VISIBLE_DEVICES:", os.environ.get("ROCR_VISIBLE_DEVICES"))
```

提交为任务后，输出应只显示 assigned GPU。

### 2. 超时控制

任务超时后，调度器会自动：

1. 发送 `SIGTERM` (Linux) 或 `taskkill /T /F` (Windows)
2. 终止整个进程树 (包括子进程)
3. 更新任务状态为 `timeout`
4. 释放 GPU 资源

**示例:**
```json
{
  "name": "long-training",
  "command": "python",
  "args": ["train.py"],
  "timeout": 3600
}
```

### 3. 优先级抢占

当高优先级任务提交时，调度器可以抢占低优先级任务的 GPU 资源。

**抢占条件:**
- 高优先级任务 (`priority >= 8`)
- 没有空闲 GPU
- 低优先级任务 (`priority <= 3`) 正在运行

**配置:**
```go
// 在 scheduler.go 中调整抢占阈值
const (
    HighPriorityThreshold = 8
    LowPriorityThreshold  = 3
)
```

### 4. 日志管理

- **日志目录:** `./logs/` (可通过 `-log-dir` 参数修改)
- **日志文件:** `logs/task-{task_id}.log`
- **查看日志:**
  - 通过 Dashboard (点击 "Log" 按钮)
  - 直接查看文件: `cat logs/task-1234567890.log`

---

## 故障排查

### 1. Dashboard 显示 404

**原因:** `dashboard.html` 文件路径不正确

**解决:**
- 确保 `dashboard.html` 在与 binary 相同的目录下
- 检查 `main.go` 中使用 `os.Executable()` 获取路径 (已修复 in `fcdc449c`)

**验证:**
```bash
curl http://localhost:8080/dashboard
# 应返回 200 OK 和 HTML 内容
```

### 2. 任务提交后状态一直是 `pending`

**原因:** 没有足够的 GPU 资源

**解决:**
- 检查 GPU 状态: `GET /api/gpus`
- 检查运行中的任务: `GET /api/tasks?status=running`
- 停止不必要的任务: `POST /api/tasks/{id}/stop`

### 3. 任务超时后进程未终止 (Windows)

**原因:** `taskkill` 未正确执行

**解决:**
- 确保使用 `taskkill /T /F /PID {pid}` (杀死进程树)
- 检查 `executor.go` 中 `killProcessTree()` 函数

**验证:**
```powershell
Get-Process | Where-Object {$_.Id -eq <PID>}
# 应返回空 (进程已终止)
```

### 4. Git push 失败 (TLS Error)

**原因:** Windows schannel SSL 后端问题

**解决:**
- 使用 GitHub API 直接上传 (参考 `push_via_api.py`)
- 或切换到 SSH: `git remote set-url origin git@github.com:cpufreestyle/hybrid-gpu-scheduler.git`

### 5. GPU 监控数据不更新

**原因:** `nvidia-smi` 或 `rocm-smi` 未安装/路径不正确

**解决:**
- **NVIDIA:** 确保 `nvidia-smi` 在 PATH 中
- **AMD:** 确保 `rocm-smi` 在 PATH 中
- 检查日志: `logs/scheduler.log`

**验证:**
```bash
nvidia-smi --query-gpu=index,name,memory.used,memory.total,utilization.gpu,temperature.gpu --format=csv,noheader,nounits
rocm-smi --showuse --json
```

---

## 开发指南

### 项目结构

```
hybrid-gpu-scheduler/
├── cmd/server/          # HTTP 服务器入口
│   ├── main.go          # 路由、中间件、API 注册
│   └── preempt_api.go   # 抢占式调度 API
├── internal/
│   ├── scheduler/       # 调度核心
│   │   ├── scheduler.go       # 主调度器 (评分算法、策略选择)
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
│   ├── roc/             # AMD ROCm 操作 (跨平台构建)
│   └── types/           # 数据类型定义
├── dashboard.html       # Web 可视化面板
└── README.md           # 项目说明
```

### 添加新调度策略

1. **定义策略函数** (`internal/scheduler/scheduler.go`):
```go
func (s *Scheduler) scoreGPUByNewPolicy(gpu *types.GPU, task *types.Task) int {
    score := 50
    // 自定义评分逻辑
    return score
}
```

2. **注册策略:**
```go
func (s *Scheduler) SelectGPU(task *types.Task) string {
    switch s.defaultPolicy {
    case "binpack":
        return s.selectGPUBinpack(task)
    case "spread":
        return s.selectGPUspread(task)
    case "gpu_type":
        return s.selectGPUByType(task)
    case "new_policy":  // 新增
        return s.selectGPUByNewPolicy(task)
    default:
        return s.selectGPUBinpack(task)
    }
}
```

3. **添加 API 验证** (`cmd/server/main.go`):
```go
validPolicies := []string{"binpack", "spread", "gpu_type", "new_policy"}
```

### 运行测试

```bash
# 单元测试
go test ./...

# 集成测试 (需要真实 GPU)
go test -v ./tests/

# 手动测试
./bin/scheduler.exe &
curl -X POST http://localhost:8080/api/tasks -d @test_task.json
```

### 构建 Release

```bash
# Windows
go build -ldflags="-s -w" -o bin/scheduler-windows-amd64.exe ./cmd/server/

# Linux
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/scheduler-linux-amd64 ./cmd/server/

# 创建 Release
gh release create v1.0.0 bin/scheduler-*
```

---

## 📚 附录

### A. 任务类型说明

| 类型 | 说明 | 推荐 GPU |
|------|------|----------|
| `training` | 模型训练 | NVIDIA (CUDA 生态) |
| `inference` | 模型推理 | AMD (性价比高) |
| `compute` | 通用计算 | 任意 |

### B. 状态码说明

| 状态码 | 说明 |
|--------|------|
| `pending` | 排队中 (等待 GPU 资源) |
| `running` | 运行中 |
| `completed` | 正常完成 |
| `failed` | 失败 (进程退出码非 0) |
| `killed` | 被手动停止 |
| `timeout` | 超时终止 |

### C. 相关项目

- **HAMi:** https://github.com/Project-HAMi/HAMi (Kubernetes GPU 虚拟化)
- **NVIDIA MIG:** https://www.nvidia.com/en-us/technologies/multi-instance-gpu/
- **AMD ROCm:** https://www.amd.com/en/graphics/servers-solutions-rocm

---

## 📞 支持

- **Issues:** https://github.com/cpufreestyle/hybrid-gpu-scheduler/issues
- **Discussions:** https://github.com/cpufreestyle/hybrid-gpu-scheduler/discussions
- **Email:** (待添加)

---

**文档版本:** 1.0 | **最后更新:** 2026-05-23 | **作者:** cpufreestyle
