//go:build ignore

// generate-manifest.go 机械生成仓库交付清单；PROJECT_MANIFEST.json 自身不参与摘要。
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fileRecord struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func main() {
	var files []fileRecord
	var goFiles, goLines, markdownFiles, markdownLines int
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		clean := filepath.ToSlash(strings.TrimPrefix(path, "./"))
		if entry.IsDir() {
			switch clean {
			case ".git", ".idea", ".vscode", "bin", "data", "node_modules", "objects", "tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !isDeliverable(clean) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		files = append(files, fileRecord{Path: clean, Bytes: len(body), SHA256: hex.EncodeToString(sum[:])})
		lines := countLines(body)
		switch filepath.Ext(clean) {
		case ".go":
			goFiles++
			goLines += lines
		case ".md":
			markdownFiles++
			markdownLines += lines
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	coverage := readCoverage()
	manifest := map[string]any{
		"product":     "DecisionProof V2",
		"description": "产品决策到生产结果的可信闭环平台工程底座",
		"version":     "2.2.0-stage-c-contract",
		"date":        "2026-07-24",
		"truthfulness_status": map[string]string{
			"local_engineering_foundation":  "VERIFIED",
			"postgres_runtime":              "VERIFIED_SINGLE_NODE",
			"kubernetes_experimental":       "VERIFIED_DOCKER_DESKTOP",
			"oidc_local_crypto_integration": "VERIFIED",
			"gitlab_delivery_contract":      "VERIFIED",
			"webhook_reconciler_postgres":   "VERIFIED",
			"enterprise_oidc_provider":      "NOT_RUN",
			"real_gitlab_provider":          "NOT_RUN",
			"temporal_workflow":             "NOT_IMPLEMENTED",
			"kubernetes_agent_runtime":      "NOT_IMPLEMENTED",
			"argo_delivery":                 "NOT_IMPLEMENTED",
			"production_ready":              "NO",
		},
		"statistics": map[string]any{
			"files_excluding_bin": len(files),
			"go_files":            goFiles,
			"go_lines":            goLines,
			"markdown_files":      markdownFiles,
			"markdown_lines":      markdownLines,
			"local_test_coverage": coverage,
		},
		"files": files,
	}
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile("PROJECT_MANIFEST.json", out, 0o644); err != nil {
		log.Fatal(err)
	}
}

// isDeliverable 排除本机产物，只让清单描述实际准备提交和分发的工程文件。
func isDeliverable(path string) bool {
	if path == "PROJECT_MANIFEST.json" || strings.HasPrefix(path, ".DS_Store") || strings.Contains(path, "/.DS_Store") {
		return false
	}
	base := filepath.Base(path)
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return base == ".env.example"
	}
	switch filepath.Ext(path) {
	case ".log", ".out", ".prof", ".test", ".tmp":
		return false
	}
	if strings.HasPrefix(path, "validation/") {
		switch filepath.Ext(path) {
		case ".json", ".html", ".txt", ".out", ".log", ".tmp":
			return false
		}
	}
	return true
}

func readCoverage() string {
	body, err := os.ReadFile("validation/coverage-final-summary.txt")
	if err != nil {
		return "UNKNOWN"
	}
	fields := strings.Fields(string(body))
	for i := len(fields) - 1; i >= 0; i-- {
		if strings.HasSuffix(fields[i], "%") {
			return fields[i]
		}
	}
	return "UNKNOWN"
}

func countLines(body []byte) int {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	lines := 0
	for scanner.Scan() {
		lines++
	}
	return lines
}
