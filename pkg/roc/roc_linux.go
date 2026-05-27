// +build !windows

package roc

/*
#cgo LDFLAGS: -lrocm_smi64
#include <rocm_smi/rocm_smi.h>
*/
import "C"

type ROCmDevice struct {
    index int
}

func InitROCm() error {
    // 初始化 ROCm
    return nil
}

func GetDeviceCount() (int, error) {
    // 获取 GPU 设备数量
    return 0, nil
}

func GetDeviceInfo(index int) (map[string]interface{}, error) {
    // 获取设备详细信息
    return map[string]interface{}{}, nil
}
