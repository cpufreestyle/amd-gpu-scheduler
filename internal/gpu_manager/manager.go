package gpu_manager

import (
    "github.com/hybrid-gpu-scheduler/pkg/gpu"
    "github.com/hybrid-gpu-scheduler/pkg/roc"
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

