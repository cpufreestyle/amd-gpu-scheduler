package gpu_manager

import (
    "github.com/amd-gpu-scheduler/pkg/gpu"
    "github.com/amd-gpu-scheduler/pkg/roc"
)

type Manager struct {
    gpuMgr *gpu.GPUManager
}

func NewManager() (*Manager, error) {
    // 初始化 ROCm
    if err := roc.InitROCm(); err != nil {
        return nil, err
    }
    
    return &Manager{
        gpuMgr: gpu.NewGPUManager(),
    }, nil
}
