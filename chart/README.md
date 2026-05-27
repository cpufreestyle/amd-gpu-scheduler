# Helm Values Reference

## Quick Start

```bash
# All-in-one (single node)
helm install hgs ./chart -n hgs --create-namespace

# With custom settings
helm install hgs ./chart \
  --set mode=allInOne \
  --set allInOne.config.policy=spread \
  --set allInOne.nvidia.deviceIds="0,1" \
  --set allInOne.amd.deviceIds="0"

# Master + Agents (multi-node)
# 1. Install master
helm install hgs-master ./chart \
  --set mode=master -n hgs --create-namespace

# 2. Install agent on each GPU node
helm install hgs-agent ./chart \
  --set mode=agent \
  --set agent.master.url=http://hgs-master:8080 \
  -n hgs --create-namespace
```

## Mode

| Value | Description |
|-------|-------------|
| `allInOne` | Scheduler + agent combined (single-node) |
| `master` | Central scheduler for multi-node cluster |
| `agent` | Worker node that connects to master |

## NVIDIA GPU

```yaml
allInOne:
  nvidia:
    enabled: true
    deviceIds: "0,1"       # Specific GPUs; empty = auto-detect all
    migStrategy: "none"     # none | single | mixed
```

## AMD GPU

```yaml
allInOne:
  amd:
    enabled: true
    deviceIds: "0"         # Specific GPUs; empty = auto-detect all
```

## Scheduling Policy

```yaml
allInOne:
  config:
    policy: binpack         # binpack | spread | gpu_type
```

- **binpack**: Fill up one GPU before using the next (maximizes utilization)
- **spread**: Distribute tasks evenly across GPUs (maximizes availability)
- **gpu_type**: Match training→NVIDIA, inference→AMD

## Ingress (with cert-manager)

```yaml
allInOne:
  ingress:
    enabled: true
    className: nginx
    annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
    hosts:
      - host: scheduler.example.com
        paths:
          - path: /
    tls:
      - hosts:
          - scheduler.example.com
        secretName: hgs-tls
```

## Node Affinity (GPU nodes)

```yaml
allInOne:
  nodeSelector:
    gpu: "true"

  tolerations:
    - key: nvidia.com/gpu
      operator: Exists
      effect: NoSchedule
    - key: amd.com/gpu
      operator: Exists
      effect: NoSchedule
```

## Resources

```yaml
allInOne:
  resources:
    limits:
      nvidia.com/gpu: 2     # Request 2 NVIDIA GPUs
      memory: "4Gi"
      cpu: "2"
    requests:
      memory: "512Mi"
      cpu: "250m"
```
