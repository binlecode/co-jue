#!/usr/bin/env bash
# install.sh — ting-gen-2 原子安装与卸载脚本 (CLI 二进制 + 全局 Agent Skill)
set -euo pipefail

REPO_DIR=$(cd -P "$(dirname "$0")" && pwd -P)
BIN_DIR="${HOME}/bin"
CO_BRAIN_DIR="${HOME}/workspace_genai/co-brain"
CO_BRAIN_SKILLS="${CO_BRAIN_DIR}/skills/ting"
GLOBAL_SKILLS_PARENT="${HOME}/env-config/.agents/skills"
GLOBAL_SKILLS="${GLOBAL_SKILLS_PARENT}/ting"

# -----------------------------------------------------------------------------
# 卸载逻辑 (--uninstall)
# -----------------------------------------------------------------------------
if [[ "${1:-}" == "--uninstall" ]]; then
    echo "==> 正在卸载 ting-gen-2..."
    
    if [[ -f "${BIN_DIR}/ting" ]]; then
        rm -f "${BIN_DIR}/ting"
        echo "    ✔ 已删除二进制: ${BIN_DIR}/ting"
    fi

    if [[ -L "${GLOBAL_SKILLS}" ]]; then
        rm -f "${GLOBAL_SKILLS}"
        echo "    ✔ 已移除全局技能软链: ${GLOBAL_SKILLS}"
    fi

    if [[ -d "${CO_BRAIN_SKILLS}" ]]; then
        rm -rf "${CO_BRAIN_SKILLS}"
        echo "    ✔ 已清理 co-brain 技能目录: ${CO_BRAIN_SKILLS}"
    fi

    echo "🎉 卸载完成！ting 已从系统 PATH 与全局 Agent 技能中移除。"
    exit 0
fi

# -----------------------------------------------------------------------------
# 安装逻辑 (默认)
# -----------------------------------------------------------------------------
echo "==> 1. 检查运行时依赖..."
missing=0
for dep in go mpv yt-dlp; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo "    ❌ 缺少必要工具: $dep (建议通过 brew install $dep 安装)"
        missing=1
    fi
done
if [[ $missing -eq 1 ]]; then
    exit 1
fi
echo "    ✔ 依赖完整 (go, mpv, yt-dlp)"

echo "==> 2. 编译并安装 ting CLI..."
mkdir -p "$BIN_DIR"
(cd "$REPO_DIR" && go build -trimpath -ldflags "-s -w" -o "${BIN_DIR}/ting" ./cmd/ting)
echo "    ✔ 已安装至 ${BIN_DIR}/ting ($("${BIN_DIR}/ting" -V))"

echo "==> 3. 部署并软链全局 Agent Skill..."
mkdir -p "$CO_BRAIN_SKILLS"
cp "$REPO_DIR/SKILL.md" "$CO_BRAIN_SKILLS/SKILL.md"
echo "    ✔ 已同步至 co-brain 技能库: ${CO_BRAIN_SKILLS}/SKILL.md"

if [[ -d "$GLOBAL_SKILLS_PARENT" ]]; then
    ln -sfn "$CO_BRAIN_SKILLS" "$GLOBAL_SKILLS"
    echo "    ✔ 已建立全局技能软链: ${GLOBAL_SKILLS}"
fi

echo "==> 4. 自检与健康验证..."
status_out=$("${BIN_DIR}/ting" status)
if [[ $? -eq 0 ]] && grep -q '"state":"idle"' <<<"$status_out"; then
    echo "    ✔ ting 微外设自检通过 (状态: idle)"
else
    echo "    ⚠️ 自检返回异常: $status_out"
fi

if command -v brain-doctor >/dev/null 2>&1; then
    echo "==> 5. 运行 brain-doctor 全局健康验证..."
    brain-doctor --treat >/dev/null 2>&1 || true
    echo "    ✔ 全脑基础设施核验完成"
fi

echo ""
echo "🎉 安装完成！ting-gen-2 已在所有终端环境与 Agent 会话中全局生效。"
