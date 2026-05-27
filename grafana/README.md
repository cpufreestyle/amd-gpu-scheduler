# Grafana + Prometheus 监控

本目录包含 Hybrid GPU Scheduler 的 Grafana Dashboard 和 Prometheus 配置。

## 快速部署

### 1. Prometheus 配置

在 `prometheus.yml` 中添加 scrape target：

```yaml
scrape_configs:
  - job_name: 'hybrid-gpu-scheduler'
    static_configs:
      - targets: ['localhost:8080']   # 改为你的调度器地址
    metrics_path: '/metrics'
    scrape_interval: 5s
```

### 2. 导入 Dashboard

**方式 A — Grafana UI（推荐）**
1. 打开 Grafana → Dashboards → Import
2. 上传 `grafana/dashboard.json` 或粘贴其内容
3. 选择 Prometheus 数据源
4. 点击 Import

**方式 B — API**
```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -d @grafana/dashboard.json \
  http://admin:admin@localhost:3000/api/dashboards/db
```

### 3. Docker Compose 一键启动

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - ./grafana/dashboard.json:/var/lib/grafana/dashboards/hgs.json
      - ./grafana/provisioning:/etc/grafana/provisioning/dashboards
    depends_on:
      - prometheus
```

## Dashboard 面板说明

| 面板 | 指标 | 说明 |
|------|------|------|
| GPU Utilization | `hgs_gpu_utilization` | NVIDIA/AMD 利用率 (%) |
| GPU VRAM Usage | `hgs_gpu_vram_used/total` | 显存使用比例 (%) |
| NVIDIA/AMD Temperature | `hgs_gpu_temperature_c` | GPU 温度 (°C) |
| GPU Power Draw | `hgs_gpu_power_watts` | 功耗 (W) |
| Running / Pending Tasks | `hgs_tasks_running/pending` | 实时任务数 |
| Total Completed / Failed | `hgs_tasks_completed/failed_total` | 累计任务数 |
| Task Throughput | `rate(hgs_tasks_submitted_total[5m])` | 任务提交/完成速率 |
| Tasks by Type | `sum by (task_type)` | 按类型分组任务数 |
| Tasks by GPU Type | `sum by (gpu_type)` | NVIDIA/AMD 分组 |
| Scheduling Latency | `histogram_quantile(p50/p95/p99)` | 调度延迟分布 |
| Memory & CPU | `process_resident_memory_bytes/cpu_seconds` | 进程资源占用 |
| HTTP Request Rate | `rate(hgs_http_requests_total[5m])` | API 请求频率 |

## 数据源变量

Dashboard 使用 `${DS_PROMETHEUS}` 变量引用数据源，导入时选择你的 Prometheus 数据源即可。

## 告警规则（可选）

```yaml
groups:
  - name: hgs-alerts
    rules:
      - alert: GPUOverheating
        expr: hgs_gpu_temperature_c > 85
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "GPU {{ $labels.gpu_id }} temperature {{ $value }}°C"

      - alert: HighVRAMUsage
        expr: (hgs_gpu_vram_used_mb / hgs_gpu_vram_total_mb) > 0.95
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "GPU {{ $labels.gpu_id }} VRAM {{ $value | humanizePercentage }}"

      - alert: TaskQueueBacklog
        expr: hgs_tasks_pending > 50
        for: 5m
        labels:
          severity: info
        annotations:
          summary: "{{ $value }} tasks pending in queue"
```
