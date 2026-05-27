package gpu

import "time"

type GPUStatus struct {
    DeviceID     int
    Name         string
    MemoryUsed   uint64
    MemoryTotal  uint64
    Utilization  float32
    Temperature  float32
    PowerUsage   float32
    UpdatedAt    time.Time
}

type GPUManager struct {
    gpus []GPUStatus
}

func NewGPUManager() *GPUManager {
    return &GPUManager{
        gpus: make([]GPUStatus, 0),
    }
}

func (m *GPUManager) GetGPUStatus(id int) (*GPUStatus, error) {
    // 通过 ROCm API 获取 GPU 状态
    return &GPUStatus{}, nil
}

func (m *GPUManager) ListGPUs() []GPUStatus {
    // 列出所有可用 GPU
    return m.gpus
}

func (m *GPUManager) GetLoad(id int) (float32, error) {
    // 获取 GPU 负载
    return 0.0, nil
}
