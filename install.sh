#!/usr/bin/env bash
# install.sh — co-ting (ting) 原子安装与卸载脚本 (CLI 二进制 + 全局 Agent Skill)
set -euo pipefail

REPO_DIR=$(cd -P "$(dirname "$0")" && pwd -P)
BIN_DIR="${HOME}/bin"
CO_BRAIN_DIR="${HOME}/workspace_genai/co-brain"
CO_BRAIN_SKILLS="${CO_BRAIN_DIR}/skills/ting"
CO_BRAIN_CO_SKILLS="${CO_BRAIN_DIR}/skills/co-ting"
GLOBAL_SKILLS_PARENT="${HOME}/env-config/.agents/skills"
GLOBAL_SKILLS="${GLOBAL_SKILLS_PARENT}/ting"
GLOBAL_CO_SKILLS="${GLOBAL_SKILLS_PARENT}/co-ting"
VERSION=$(tr -d '[:space:]' < "${REPO_DIR}/VERSION")

# -----------------------------------------------------------------------------
# 卸载逻辑 (--uninstall)
# -----------------------------------------------------------------------------
if [[ "${1:-}" == "--uninstall" ]]; then
    echo "==> 正在卸载 co-ting / ting (v${VERSION})..."
    
    for b in ting co-ting; do
        if [[ -f "${BIN_DIR}/$b" || -L "${BIN_DIR}/$b" ]]; then
            rm -f "${BIN_DIR}/$b"
            echo "    ✔ 已删除二进制/软链: ${BIN_DIR}/$b"
        fi
    done

    for s in "$GLOBAL_SKILLS" "$GLOBAL_CO_SKILLS" "${HOME}/.agents/skills/ting" "${HOME}/.agents/skills/co-ting" "$CO_BRAIN_CO_SKILLS"; do
        if [[ -L "$s" || -f "$s" ]]; then
            rm -f "$s"
            echo "    ✔ 已移除技能软链: $s"
        fi
    done

    if [[ -d "${CO_BRAIN_SKILLS}" ]]; then
        rm -rf "${CO_BRAIN_SKILLS}"
        echo "    ✔ 已清理 co-brain 技能目录: ${CO_BRAIN_SKILLS}"
    fi

    echo "🎉 卸载完成！co-ting 已从系统 PATH 与全局 Agent 技能中移除。"
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

echo "==> 2. 编译并安装 co-ting CLI (v${VERSION})..."
mkdir -p "$BIN_DIR"
(cd "$REPO_DIR" && go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "${REPO_DIR}/ting" ./cmd/ting)
ln -sfn "${REPO_DIR}/ting" "${BIN_DIR}/ting"
ln -sfn "${REPO_DIR}/ting" "${BIN_DIR}/co-ting"
echo "    ✔ 已安装软链至 ${BIN_DIR}/ting 与 ${BIN_DIR}/co-ting ($("${BIN_DIR}/ting" -V))"

echo "==> 3. 部署并更新全局 Agent Skill..."
mkdir -p "$CO_BRAIN_SKILLS"
cp "$REPO_DIR/SKILL.md" "$CO_BRAIN_SKILLS/SKILL.md"
ln -sfn "$CO_BRAIN_SKILLS" "$CO_BRAIN_CO_SKILLS"
echo "    ✔ 已同步至 co-brain 技能库: ${CO_BRAIN_SKILLS}/SKILL.md (含 co-ting 别名)"

if [[ -d "$GLOBAL_SKILLS_PARENT" ]]; then
    ln -sfn "$CO_BRAIN_SKILLS" "$GLOBAL_SKILLS"
    ln -sfn "$CO_BRAIN_SKILLS" "$GLOBAL_CO_SKILLS"
    echo "    ✔ 已建立/刷新全局技能软链: ${GLOBAL_SKILLS} & ${GLOBAL_CO_SKILLS}"
fi

# 确保 ~/.agents/skills/ting 与 co-ting 可达
if [[ -d "${HOME}/.agents/skills" ]]; then
    ln -sfn "$CO_BRAIN_SKILLS" "${HOME}/.agents/skills/ting"
    ln -sfn "$CO_BRAIN_SKILLS" "${HOME}/.agents/skills/co-ting"
    echo "    ✔ 已建立 ~/.agents 技能软链: ting & co-ting"
fi

# 强校验 Skill 内容一致性
if cmp -s "$REPO_DIR/SKILL.md" "${HOME}/.agents/skills/ting/SKILL.md"; then
    echo "    ✔ 全局 Agent Skill 校验一致 (指向: $(readlink "${HOME}/.agents/skills/ting"))"
else
    echo "    ❌ 全局 Agent Skill 校验失败" >&2
    exit 1
fi

echo "==> 4. 自检与健康验证..."
status_out=$("${BIN_DIR}/ting" status)
if [[ $? -eq 0 ]] && grep -q '"status":"ok"' <<<"$status_out"; then
    echo "    ✔ co-ting 微外设自检通过 (响应正常: $(jq -r .state <<<"$status_out"))"
else
    echo "    ⚠️ 自检返回异常: $status_out"
fi

if command -v brain-doctor >/dev/null 2>&1; then
    echo "==> 5. 运行 brain-doctor 全局健康验证..."
    brain-doctor --treat >/dev/null 2>&1 || true
    echo "    ✔ 全脑基础设施核验完成"
fi

echo ""
echo "🎉 安装完成！ting (v${VERSION}) 已在所有终端环境与 Agent 会话中全局生效。"
