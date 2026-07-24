package telemetry

import (
	"encoding/json"
	"log"
	"time"
)

// Log 以结构化 JSON 输出审计友好的运行日志。
func Log(level, message string, fields map[string]any) {
	record := map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level,
		"message": message,
	}
	for key, value := range fields {
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err != nil {
		log.Printf("日志序列化失败: %v", err)
		return
	}
	log.Print(string(data))
}
