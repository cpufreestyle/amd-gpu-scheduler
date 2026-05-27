// +build windows

package roc

type ROCmDevice struct {
    index int
}

func InitROCm() error {
    // Windows 不支持 ROCm，返回空实现
    return nil
}

func GetDeviceCount() (int, error) {
    // Windows 上返回 0 个 ROCm 设备
    return 0, nil
}

func GetDeviceInfo(index int) (map[string]interface{}, error) {
    // Windows 上返回空信息
    return map[string]interface{}{}, nil
}
