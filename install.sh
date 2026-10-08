#!/usr/bin/env bash
# install.sh — co-jue (jue) 原子安装与卸载脚本 (CLI 二进制 + 全局 Agent Skill)
set -euo pipefail

REPO_DIR=$(cd -P "$(dirname "$0")" && pwd -P)
BIN_DIR="${HOME}/bin"
CO_BRAIN_DIR="${HOME}/workspace_genai/co-brain"
CO_BRAIN_SKILLS="${CO_BRAIN_DIR}/skills/jue"
GLOBAL_SKILLS_PARENT="${HOME}/env-config/.agents/skills"
GLOBAL_SKILLS="${GLOBAL_SKILLS_PARENT}/jue"
VERSION=$(tr -d '[:space:]' < "${REPO_DIR}/VERSION")

# stop_daemon <名称> <二进制>: 优雅停止 ${TMPDIR}/<名称>-<uid>/mpv.sock 背后的守护并清理沙箱。
# socket 在 stop 后仍在、却已无任何进程以它为 --input-ipc-server 时（stop 报 not_playing 或
# 无人应答），即崩溃残留的死 socket：移除并继续；仍有活进程持有才中止，保留现场。
stop_daemon() {
    local name="$1" bin="$2"
    local base="${TMPDIR:-/tmp}"
    base="${base%/}"
    local dir="${base}/${name}-$(id -u)"
    local sock="${dir}/mpv.sock"
    [[ -S "$sock" ]] || return 0
    echo "==> 检测到 ${name} 守护套接字，正在优雅停止..."
    local stop_err=""
    if [[ -x "${BIN_DIR}/${bin}" ]]; then
        stop_err=$("${BIN_DIR}/${bin}" control stop 2>&1) || true
    elif command -v "$bin" >/dev/null 2>&1; then
        stop_err=$("$bin" control stop 2>&1) || true
    fi
    for _ in {1..20}; do
        [[ ! -S "$sock" ]] && break
        sleep 0.1
    done
    if [[ -S "$sock" ]]; then
        local rc=0
        pgrep -u "$(id -u)" -f -- "--input-ipc-server=${sock}" >/dev/null 2>&1 || rc=$?
        if [[ $rc -eq 0 ]]; then
            echo "    ❌ 错误: ${name} 守护未能正常退出 (socket 仍存活: ${sock}，诊断: ${stop_err:-无输出})，保留现场以防破坏正在运行的播放器" >&2
            return 1
        elif [[ $rc -ne 1 ]]; then
            echo "    ❌ 错误: pgrep 进程探测失败 (exit ${rc})，无法确认 ${sock} 是否仍被持有，保留现场" >&2
            return 1
        fi
        echo "    ⚠ 无进程持有 $sock (诊断: ${stop_err:-无输出})，判定为残留死 socket，予以移除"
    fi
    rm -rf "$dir"
    echo "    ✔ ${name} 守护已停止并清理沙箱"
}

stop_legacy_daemon() { stop_daemon ting ting; }
stop_jue_daemon() { stop_daemon jue jue; }

# -----------------------------------------------------------------------------
# 卸载逻辑 (--uninstall)
# -----------------------------------------------------------------------------
if [[ "${1:-}" == "--uninstall" ]]; then
    echo "==> 正在卸载 co-jue (v${VERSION}) 及旧版残留..."
    stop_legacy_daemon
    stop_jue_daemon

    for b in jue ting co-ting; do
        if [[ -f "${BIN_DIR}/$b" || -L "${BIN_DIR}/$b" ]]; then
            rm -f "${BIN_DIR}/$b"
            echo "    ✔ 已删除二进制/软链: ${BIN_DIR}/$b"
        fi
        if [[ -f "${REPO_DIR}/$b" ]]; then
            rm -f "${REPO_DIR}/$b"
            echo "    ✔ 已删除编译产物: ${REPO_DIR}/$b"
        fi
    done

    for s in \
        "${GLOBAL_SKILLS}" \
        "${GLOBAL_SKILLS_PARENT}/ting" \
        "${GLOBAL_SKILLS_PARENT}/co-ting" \
        "${HOME}/.agents/skills/jue" \
        "${HOME}/.agents/skills/ting" \
        "${HOME}/.agents/skills/co-ting" \
        "${CO_BRAIN_DIR}/skills/co-ting"; do
        if [[ -L "$s" || -f "$s" ]]; then
            rm -f "$s"
            echo "    ✔ 已移除技能软链: $s"
        fi
    done

    for d in "${CO_BRAIN_SKILLS}" "${CO_BRAIN_DIR}/skills/ting"; do
        if [[ -d "$d" ]]; then
            rm -rf "$d"
            echo "    ✔ 已清理技能目录: $d"
        fi
    done

    if command -v brain-doctor >/dev/null 2>&1; then
        echo "==> 运行 brain-doctor 全局健康自愈..."
        brain-doctor --treat
    fi

    echo "🎉 卸载完成！co-jue 及旧残留已从系统 PATH 与全局 Agent 技能中移除。"
    exit 0
fi

# -----------------------------------------------------------------------------
# 安装逻辑 (默认)
# -----------------------------------------------------------------------------
# 卸载或安装前若检测到旧 ting 守护，显式优雅停止
stop_legacy_daemon

echo "==> 1. 检查运行时依赖..."
missing=0
for dep in go mpv yt-dlp jq; do
    if ! command -v "$dep" >/dev/null 2>&1; then
        echo "    ❌ 缺少必要工具: $dep (建议通过 brew install $dep 安装)"
        missing=1
    fi
done
if [[ $missing -eq 1 ]]; then
    exit 1
fi
echo "    ✔ 依赖完整 (go, mpv, yt-dlp, jq)"

echo "==> 2. 编译并安装 co-jue CLI (v${VERSION})..."
mkdir -p "$BIN_DIR"
(cd "$REPO_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "${REPO_DIR}/jue" ./cmd/jue)
ln -sfn "${REPO_DIR}/jue" "${BIN_DIR}/jue"
# 彻底清理可能残留的旧 ting / co-ting 软链
rm -f "${BIN_DIR}/ting" "${BIN_DIR}/co-ting"
echo "    ✔ 已安装软链至 ${BIN_DIR}/jue ($("${BIN_DIR}/jue" -V))"

echo "==> 3. 部署并更新全局 Agent Skill..."
mkdir -p "$CO_BRAIN_SKILLS"
cp "$REPO_DIR/SKILL.md" "$CO_BRAIN_SKILLS/SKILL.md"
# 清理 co-brain 中的旧别名软链/目录
rm -f "${CO_BRAIN_DIR}/skills/co-ting"
rm -rf "${CO_BRAIN_DIR}/skills/ting"
echo "    ✔ 已同步至 co-brain 技能库: ${CO_BRAIN_SKILLS}/SKILL.md"

if [[ -d "$GLOBAL_SKILLS_PARENT" ]]; then
    ln -sfn "$CO_BRAIN_SKILLS" "$GLOBAL_SKILLS"
    rm -f "${GLOBAL_SKILLS_PARENT}/ting" "${GLOBAL_SKILLS_PARENT}/co-ting"
    echo "    ✔ 已建立/刷新全局技能软链: ${GLOBAL_SKILLS}"
fi

# 确保 ~/.agents/skills/jue 可达，并清理旧软链
if [[ -d "${HOME}/.agents/skills" ]]; then
    ln -sfn "$CO_BRAIN_SKILLS" "${HOME}/.agents/skills/jue"
    rm -f "${HOME}/.agents/skills/ting" "${HOME}/.agents/skills/co-ting"
    echo "    ✔ 已建立 ~/.agents 技能软链: jue"
fi

# 强校验 Skill 内容一致性
if cmp -s "$REPO_DIR/SKILL.md" "${HOME}/.agents/skills/jue/SKILL.md"; then
    echo "    ✔ 全局 Agent Skill 校验一致 (指向: $(readlink "${HOME}/.agents/skills/jue"))"
else
    echo "    ❌ 全局 Agent Skill 校验失败" >&2
    exit 1
fi

echo "==> 4. 自检与健康验证..."
status_out=$("${BIN_DIR}/jue" status)
status_val=$(jq -r '.status // empty' <<<"$status_out")
state_val=$(jq -r '.state // empty' <<<"$status_out")
if [[ "$status_val" == "ok" ]]; then
    echo "    ✔ co-jue 微外设自检通过 (响应正常: ${state_val})"
else
    echo "    ❌ 自检返回异常: $status_out" >&2
    exit 1
fi

if command -v brain-doctor >/dev/null 2>&1; then
    echo "==> 5. 运行 brain-doctor 全局健康验证..."
    brain-doctor --treat >/dev/null
    echo "    ✔ 全脑基础设施核验完成"
fi

echo ""
echo "🎉 安装完成！jue (v${VERSION}) 已在所有终端环境与 Agent 会话中全局生效。"
