# AMD 双卡调度器 - 技术架构方案

> **文档版本**: v1.0  
> **创建时间**: 2026-05-18  
> **架构师**: amd-arch  
> **项目**: AMD 双卡 GPU 任务调度系统

---

## 1. 系统架构图

```
┌─────────────────────────────────────────────────────────────┐
│                        API Gateway                          │
│                    (gRPC + REST)                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │   gRPC API   │  │  REST API   │  │   WebSocket  │    │
│  └──────────────┘  └──────────────┘  └──────────────┘    │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                     Scheduler Core                          │
│  ┌──────────────────┐  ┌─────────────────────────────┐    │
│  │   Task Queue     │  │   Task Matcher (调度匹配)    │    │
│  │   (Redis)        │  │   - 优先级排序               │    │
│  │   - 待调度队列    │  │   - 资源匹配                 │    │
│  │   - 运行中队列    │  │   - 策略选择                 │    │
│  │   - 完成队列      │  └─────────────────────────────┘    │
│  └──────────────────┘                                       │
│  ┌─────────────────────────────────────────────────────┐    │
│  │           调度策略引擎                               │    │
│  │  - 负载均衡 (Round Robin / 最少连接)                │    │
│  │  - 优先级抢占                                       │    │
│  │  - 故障自动切换                                     │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                     GPU Manager                             │
│  ┌──────────────────┐  ┌─────────────────────────────┐    │
│  │   GPU-0          │  │   GPU-1                     │    │
│  │   (AMD Radeon)   │  │   (AMD Radeon)              │    │
│  │   - 显存监控      │  │   - 显存监控                │    │
│  │   - 温度监控      │  │   - 温度监控                │    │
│  │   - 利用率监控    │  │   - 利用率监控              │    │
│  │   - 任务执行      │  │   - 任务执行                │    │
│  └──────────────────┘  └─────────────────────────────┘    │
│                                                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │           ROCm/OpenCL 驱动层                        │    │
│  │  - 设备初始化                                       │    │
│  │  - 内存管理                                         │    │
│  │  - 内核执行                                         │    │
│  └─────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                      Data Layer                              │
│  ┌──────────────────┐  ┌─────────────────────────────┐     │
│  │   PostgreSQL      │  │   Redis                    │     │
│  │   - 任务持久化    │  │   - 任务队列                │     │
│  │   - 历史记录      │  │   - 实时状态                │     │
│  │   - 用户配置      │  │   - 缓存                    │     │
│  └──────────────────┘  └─────────────────────────────┘     │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│                   Monitoring & Admin                        │
│  ┌──────────────────┐  ┌─────────────────────────────┐    │
│  │   实时监控面板     │  │   Admin CLI                │    │
│  │   (Web UI)        │  │   - 任务管理                │    │
│  │   - GPU 状态      │  │   - GPU 配置                │    │
│  │   - 任务列表      │  │   - 日志查看                │    │
│  │   - 性能图表      │  │   - 系统诊断                │    │
│  └──────────────────┘  └─────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

---

## 2. 技术选型

| 技术栈 | 选型 | 理由 |
|--------|------|------|
| **编程语言** | Go 1.26.1 | 高性能并发、静态编译、跨平台、丰富的生态 |
| **API 框架** | gRPC + Protobuf | 高性能 RPC、强类型接口、支持流式通信 |
| **辅助 API** | REST (gin) | 便于 Web 前端集成、调试工具支持 |
| **任务队列** | Redis 7.0+ | 高性能内存数据库、原子操作、持久化支持 |
| **数据存储** | PostgreSQL 16+ | 关系型数据、事务支持、JSON 支持 |
| **GPU 驱动** | AMD ROCm 6.0+ / OpenCL | AMD GPU 计算生态、跨平台兼容 |
| **容器化** | Docker + Docker Compose | 环境隔离、快速部署、版本管理 |
| **监控** | Prometheus + Grafana | 指标采集、可视化、告警 |
| **日志** | Zap (Go) | 高性能结构化日志 |
| **配置管理** | Viper (Go) | 多格式配置、热重载 |

---

## 3. 模块设计

### 3.1 模块划分

```
amd-gpu-scheduler/
├── cmd/                    # 入口程序
│   ├── scheduler/          # 调度器主程序
│   ├── gpu-manager/        # GPU 管理器
│   └── cli/                # 命令行工具
├── pkg/                    # 核心包
│   ├── api/                # API 层
│   │   ├── grpc/          # gRPC 服务实现
│   │   └── rest/          # REST 接口实现
│   ├── scheduler/          # 调度核心
│   │   ├── queue/         # 任务队列管理
│   │   ├── matcher/       # 调度匹配算法
│   │   └── strategy/     # 调度策略
│   ├── gpu/                # GPU 管理
│   │   ├── device/        # 设备抽象层
│   │   ├── monitor/       # 监控采集
│   │   └── rocm/         # ROCm 驱动封装
│   ├── storage/            # 存储层
│   │   ├── postgres/      # PostgreSQL 操作
│   │   └── redis/         # Redis 操作
│   └── common/            # 公共组件
│       ├── config/        # 配置管理
│       ├── logger/        # 日志
│       └── metrics/       # 监控指标
├── api/                    # Protobuf 定义
│   └── scheduler.proto    # gRPC 接口定义
├── web/                    # 前端资源
│   └── ui/                # 监控面板
├── deployments/            # 部署配置
│   ├── docker/            # Docker 配置
│   └── k8s/               # Kubernetes 配置
└── docs/                  # 文档
```

### 3.2 核心模块详解

#### 3.2.1 Scheduler Core (调度核心)

**职责**:
- 接收任务提交请求
- 根据调度策略选择合适 GPU
- 管理任务生命周期
- 处理任务优先级和抢占

**关键数据结构**:
```go
type Task struct {
    ID          string
    Type        TaskType  // TRAINING, INFERENCE, COMPUTE
    Priority    int       // 1-10, 10 最高
    GPURequired int       // 需要的 GPU 数量
    Status      TaskStatus // PENDING, RUNNING, COMPLETED, FAILED
    SubmitTime  time.Time
    StartTime   *time.Time
    EndTime     *time.Time
}

type Scheduler interface {
    SubmitTask(task *Task) error
    CancelTask(taskID string) error
    GetTaskStatus(taskID string) (*TaskStatus, error)
    ListTasks(filter *TaskFilter) ([]*Task, error)
}
```

#### 3.2.2 Queue (任务队列)

**职责**:
- 管理待调度任务队列
- 优先级排序
- 任务超时处理

**实现**:
- 使用 Redis Sorted Set 实现优先级队列
- Key: `tasks:pending`, Score: 优先级 + 提交时间
- 原子操作保证并发安全

#### 3.2.3 GPU Manager (GPU 管理)

**职责**:
- GPU 设备发现和初始化
- 显存、温度、利用率监控
- 任务分配和执行
- 故障检测和自动切换

**关键数据结构**:
```go
type GPUDevice struct {
    ID           int
    Name         string
    TotalMemory  uint64
    UsedMemory   uint64
    Temperature  int
    Utilization  float64
    Status       GPUStatus // IDLE, BUSY, ERROR
    CurrentTask  *Task
}

type GPUManager interface {
    ListGPUs() ([]*GPUDevice, error)
    GetGPUStatus(gpuID int) (*GPUDevice, error)
    AllocateTask(gpuID int, task *Task) error
    ReleaseTask(gpuID int, taskID string) error
}
```

#### 3.2.4 API Layer (接口层)

**职责**:
- 提供 gRPC 接口供程序调用
- 提供 REST 接口供 Web 前端调用
- 请求验证和鉴权
- 协议转换

**接口定义**: 详见第 4 节

---

## 4. API 接口设计 (gRPC)

### 4.1 Protobuf 定义

```protobuf
syntax = "proto3";

package scheduler;

service GPUScheduler {
  // 提交任务
  rpc SubmitTask(SubmitTaskRequest) returns (SubmitTaskResponse);
  
  // 取消任务
  rpc CancelTask(CancelTaskRequest) returns (CancelTaskResponse);
  
  // 获取任务状态
  rpc GetTaskStatus(GetTaskStatusRequest) returns (GetTaskStatusResponse);
  
  // 列出所有任务
  rpc ListTasks(ListTasksRequest) returns (ListTasksResponse);
  
  // 列出所有 GPU
  rpc ListGPUs(ListGPUsRequest) returns (ListGPUsResponse);
  
  // 获取 GPU 状态
  rpc GetGPUStatus(GetGPUStatusRequest) returns (GetGPUStatusResponse);
  
  // 流式监控 (服务端推送)
  rpc StreamGPUStatus(StreamGPUStatusRequest) returns (stream GPUStatusUpdate);
}

message SubmitTaskRequest {
  TaskType type = 1;
  int32 priority = 2;
  int32 gpu_required = 3;
  map<string, string> env = 4;
  repeated string command = 5;
}

message SubmitTaskResponse {
  string task_id = 1;
  TaskStatus status = 2;
}

message CancelTaskRequest {
  string task_id = 1;
}

message CancelTaskResponse {
  bool success = 1;
  string message = 2;
}

message GetTaskStatusRequest {
  string task_id = 1;
}

message GetTaskStatusResponse {
  Task task = 1;
}

message ListTasksRequest {
  TaskStatus filter_status = 1;
  TaskType filter_type = 2;
}

message ListTasksResponse {
  repeated Task tasks = 1;
}

message ListGPUsRequest {}

message ListGPUsResponse {
  repeated GPUDevice gpus = 1;
}

message GetGPUStatusRequest {
  int32 gpu_id = 1;
}

message GetGPUStatusResponse {
  GPUDevice gpu = 1;
}

message StreamGPUStatusRequest {}

message GPUStatusUpdate {
  int32 gpu_id = 1;
  GPUStatus status = 2;
  float utilization = 3;
  uint64 used_memory = 4;
  int32 temperature = 5;
  int64 timestamp = 6;
}

message Task {
  string id = 1;
  TaskType type = 2;
  int32 priority = 3;
  TaskStatus status = 4;
  int64 submit_time = 5;
  int64 start_time = 6;
  int64 end_time = 7;
}

message GPUDevice {
  int32 id = 1;
  string name = 2;
  uint64 total_memory = 3;
  uint64 used_memory = 4;
  int32 temperature = 5;
  float utilization = 6;
  GPUStatus status = 7;
  string current_task_id = 8;
}

enum TaskType {
  TASK_TYPE_UNSPECIFIED = 0;
  TASK_TYPE_TRAINING = 1;
  TASK_TYPE_INFERENCE = 2;
  TASK_TYPE_COMPUTE = 3;
}

enum TaskStatus {
  TASK_STATUS_UNSPECIFIED = 0;
  TASK_STATUS_PENDING = 1;
  TASK_STATUS_RUNNING = 2;
  TASK_STATUS_COMPLETED = 3;
  TASK_STATUS_FAILED = 4;
  TASK_STATUS_CANCELLED = 5;
}

enum GPUStatus {
  GPU_STATUS_UNSPECIFIED = 0;
  GPU_STATUS_IDLE = 1;
  GPU_STATUS_BUSY = 2;
  GPU_STATUS_ERROR = 3;
}
```

### 4.2 接口说明

#### SubmitTask
- **功能**: 提交新任务到调度队列
- **输入**: 任务类型、优先级、GPU 需求、环境变量、执行命令
- **输出**: 任务 ID、初始状态
- **错误码**:
  - `INVALID_ARGUMENT`: 参数错误
  - `RESOURCE_EXHAUSTED`: GPU 资源不足
  - `INTERNAL`: 内部错误

#### CancelTask
- **功能**: 取消正在运行或等待中的任务
- **输入**: 任务 ID
- **输出**: 是否成功、消息
- **错误码**:
  - `NOT_FOUND`: 任务不存在
  - `FAILED_PRECONDITION`: 任务已完成或取消

#### GetTaskStatus
- **功能**: 查询任务状态
- **输入**: 任务 ID
- **输出**: 任务详细信息
- **错误码**:
  - `NOT_FOUND`: 任务不存在

#### ListGPUs
- **功能**: 列出所有 GPU 设备
- **输入**: 无
- **输出**: GPU 设备列表

#### GetGPUStatus
- **功能**: 查询指定 GPU 状态
- **输入**: GPU ID
- **输出**: GPU 详细信息
- **错误码**:
  - `NOT_FOUND`: GPU 不存在

#### StreamGPUStatus
- **功能**: 流式推送 GPU 状态更新（实时监控）
- **输入**: 无
- **输出**: GPU 状态更新流

---

## 5. 调度策略设计

### 5.1 负载均衡策略

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| Round Robin | 轮询分配 | 任务负载均匀 |
| Least Connection | 最少连接数 | 任务执行时间差异大 |
| GPU Utilization | 基于利用率 | 需要精细调度 |

### 5.2 优先级策略

- **优先级抢占**: 高优先级任务可以抢占低优先级任务
- **优先级继承**: 长时间运行的低优先级任务不会被无限抢占
- **老化机制**: 等待时间过长的低优先级任务逐渐提升优先级

### 5.3 故障处理

- **心跳检测**: 定期检查 GPU 状态
- **自动切换**: 检测到 GPU 故障时自动迁移任务
- **任务重试**: 失败任务可配置自动重试次数

---

## 6. 数据库设计

### 6.1 PostgreSQL 表结构

```sql
-- 任务表
CREATE TABLE tasks (
    id VARCHAR(64) PRIMARY KEY,
    type VARCHAR(20) NOT NULL,
    priority INT NOT NULL,
    gpu_required INT NOT NULL,
    status VARCHAR(20) NOT NULL,
    env JSONB,
    command TEXT[],
    submit_time TIMESTAMP NOT NULL,
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- GPU 设备表
CREATE TABLE gpus (
    id INT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    total_memory BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- GPU 状态历史表
CREATE TABLE gpu_status_history (
    id SERIAL PRIMARY KEY,
    gpu_id INT REFERENCES gpus(id),
    used_memory BIGINT,
    temperature INT,
    utilization FLOAT,
    recorded_at TIMESTAMP DEFAULT NOW()
);

-- 任务-GPU 分配表
CREATE TABLE task_gpu_assignments (
    task_id VARCHAR(64) REFERENCES tasks(id),
    gpu_id INT REFERENCES gpus(id),
    assigned_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (task_id, gpu_id)
);
```

### 6.2 Redis 数据结构

```
# 待调度任务队列 (Sorted Set)
# Score = 优先级 * 10000000000 + (9999999999 - 提交时间戳)  # 高优先级优先，同优先级早提交优先
ZADD tasks:pending <score> <task_id>

# 运行中任务集合 (Set)
SADD tasks:running <task_id>

# 已完成任务集合 (Set)
SADD tasks:completed <task_id>

# GPU 状态缓存 (Hash)
HSET gpu:<gpu_id> status <status> used_memory <mem> temperature <temp> utilization <util>

# 任务信息缓存 (Hash)
HSET task:<task_id> type <type> priority <priority> status <status> submit_time <time>
```

---

## 7. 部署方案

### 7.1 Docker Compose 部署

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: scheduler
      POSTGRES_USER: admin
      POSTGRES_<SECRET_REDACTED>
    volumes:
      - postgres-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
    ports:
      - "6379:6379"

  scheduler:
    build: .
    depends_on:
      - postgres
      - redis
    environment:
      DB_HOST: postgres
      REDIS_HOST: redis
    ports:
      - "8080:8080"   # REST API
      - "9090:9090"   # gRPC
    volumes:
      - ./config:/app/config

  web:
    image: nginx:alpine
    depends_on:
      - scheduler
    ports:
      - "80:80"
    volumes:
      - ./web/ui:/usr/share/nginx/html:ro
      - ./deployments/nginx.conf:/etc/nginx/nginx.conf:ro

volumes:
  postgres-data:
  redis-data:
```

### 7.2 运行要求

- **操作系统**: Windows 10/11, Linux (Ubuntu 20.04+), macOS 12+
- **GPU 驱动**: AMD ROCm 6.0+ 或 OpenCL 2.0+
- **内存**: 8GB+ RAM
- **存储**: 50GB+ 可用空间
- **网络**: 局域网或互联网连接

---

## 8. 性能优化

### 8.1 Go 层面

- 使用 Goroutine 池避免频繁创建协程
- 使用 Channel 进行协程间通信
- 避免内存分配，使用对象池 (sync.Pool)
- 使用 pprof 进行性能分析

### 8.2 Redis 层面

- 使用 Pipeline 批量执行命令
- 使用 Lua 脚本保证原子性
- 合理设置 Key 过期时间

### 8.3 数据库层面

- 创建合适的索引
- 使用连接池
- 批量插入/更新

---

## 9. 监控和告警

### 9.1 监控指标

| 指标 | 说明 | 告警阈值 |
|------|------|----------|
| GPU 利用率 | GPU 计算单元使用率 | < 20% 持续 10 分钟 |
| GPU 温度 | GPU 核心温度 | > 85°C |
| GPU 显存使用率 | 显存占用比例 | > 90% |
| 任务队列长度 | 待调度任务数量 | > 100 |
| 任务失败率 | 失败任务占比 | > 5% |

### 9.2 告警方式

- 邮件通知
- Webhook (Slack/钉钉)
- 监控系统仪表盘

---

## 10. 开发路线图

| 阶段 | 时间 | 交付物 |
|------|------|--------|
| **Phase 1: 核心框架** | Week 1-2 | 项目结构、API 定义、数据库设计 |
| **Phase 2: 调度核心** | Week 3-4 | 任务队列、调度算法、GPU 管理器 |
| **Phase 3: ROCm 集成** | Week 5-6 | ROCm 驱动封装、GPU 监控 |
| **Phase 4: API 实现** | Week 7-8 | gRPC/REST 接口、鉴权 |
| **Phase 5: 前端面板** | Week 9-10 | Web UI、实时监控 |
| **Phase 6: 测试部署** | Week 11-12 | 单元测试、集成测试、Docker 部署 |

---

## 11. 风险与挑战

| 风险 | 影响 | 应对措施 |
|------|------|----------|
| ROCm 驱动兼容性 | 高 | 提供 OpenCL  fallback，详细文档 |
| GPU 故障处理 | 中 | 实现自动切换、任务重试 |
| 性能瓶颈 | 中 | 性能测试、优化关键路径 |
| 并发安全 | 高 | 代码审查、压力测试 |

---

## 12. 附录

### 12.1 参考文档

- [Go 官方文档](https://golang.org/doc/)
- [gRPC 官方文档](https://grpc.io/docs/)
- [AMD ROCm 文档](https://docs.amd.com/projects/ROCm/)
- [Redis 官方文档](https://redis.io/docs/)
- [PostgreSQL 官方文档](https://www.postgresql.org/docs/)

### 12.2 相关工具

- **开发工具**: GoLand / VSCode
- **API 测试**: Postman / grpcurl
- **数据库工具**: pgAdmin / RedisInsight
- **性能分析**: pprof / Grafana

---

**架构师签名**: amd-arch  
**日期**: 2026-05-18
