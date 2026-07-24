package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewID 生成不依赖中心服务的随机标识。
// 前缀用于提升日志和数据库中的可读性，随机部分使用 128 位熵。
func NewID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机标识失败: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}
