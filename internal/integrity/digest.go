package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CanonicalJSON 使用 Go 标准库生成稳定 JSON。
// 标准库会对 map 的字符串键排序；平台同时要求证据 Payload 使用结构化 JSON，禁止 NaN 等非 JSON 值。
func CanonicalJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("规范化 JSON 失败: %w", err)
	}
	return data, nil
}

// SHA256Bytes 计算带算法前缀的摘要，避免调用方混淆不同哈希算法。
func SHA256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SHA256Object 对结构化对象进行规范化后计算摘要。
func SHA256Object(v any) (string, error) {
	data, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(data), nil
}
