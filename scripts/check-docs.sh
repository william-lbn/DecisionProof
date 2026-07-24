#!/usr/bin/env sh
set -eu

required_files="
README.md
docs/IMPLEMENTATION_STATUS.md
docs/00_产品架构与研发总纲.md
docs/06_研发计划_Codex任务.md
docs/07_验证计划与生产验收.md
docs/11_全面审计与端到端实施计划.md
docs/12_实验部署与使用手册.md
docs/13_身份与GitLab交付使用手册.md
validation/STAGE_C_VALIDATION_REPORT.md
"

for file in $required_files; do
  if [ ! -s "$file" ]; then
    echo "缺少或为空的关键文档: $file" >&2
    exit 1
  fi
done

# 防止在实现状态文档中误写成全部生产完成。
if grep -Eq '完整生产平台[：:]?[[:space:]]*(已完成|完成)' docs/IMPLEMENTATION_STATUS.md; then
  echo "实现状态文档包含未经允许的完整生产完成声明" >&2
  exit 1
fi

if ! grep -q 'PROVIDER_NOT_RUN' validation/STAGE_C_VALIDATION_REPORT.md; then
  echo "Stage C 报告必须明确真实 Provider 未执行状态" >&2
  exit 1
fi

echo "关键文档检查通过"
