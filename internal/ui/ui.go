package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var assets embed.FS

// Handler 返回内嵌的零构建依赖中文产品工作台。
// 生产版可以替换为 React 微前端，但 API 与领域模型保持不变。
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/app/", http.FileServer(http.FS(sub)))
}
