#!/usr/bin/env bash
# scripts/release.sh — Local-first release automation for jue (SemVer)
# All build, validation, cross-compilation, and packaging run locally.
set -euo pipefail

REPO_DIR=$(cd -P "$(dirname "$0")/.." && pwd -P)
VERSION=$(tr -d '[:space:]' < "${REPO_DIR}/VERSION")
DIST_DIR="${REPO_DIR}/dist"

echo "==> 1. 本地前置质量门控 (Lint & Unit Tests)..."
(cd "$REPO_DIR" && go vet ./... && go test -v ./...)

echo "==> 2. 检查 SemVer 规范..."
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
    echo "ERROR: VERSION '$VERSION' 不符合 SemVer 格式" >&2
    exit 1
fi
echo "    ✔ SemVer 版本: v${VERSION}"

echo "==> 3. 本地多架构纯静态编译 (CGO_ENABLED=0)..."
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

targets=(
    "darwin arm64 jue-darwin-arm64"
    "darwin amd64 jue-darwin-amd64"
    "linux amd64 jue-linux-amd64"
    "linux arm64 jue-linux-arm64"
)

for target in "${targets[@]}"; do
    read -r os arch out <<<"$target"
    echo "    📦 编译 $os/$arch -> dist/$out"
    (cd "$REPO_DIR" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "${DIST_DIR}/${out}" ./cmd/jue)
done

echo "==> 4. 生成 SHA256 校验和..."
(cd "$DIST_DIR" && shasum -a 256 jue-* > SHA256SUMS.txt)
cat "${DIST_DIR}/SHA256SUMS.txt"

echo ""
echo "🎉 本地发布制品构建完成！全部产物位于 dist/:"
ls -lh "$DIST_DIR"

if [[ "${1:-}" == "--publish" ]]; then
    echo "==> 5. 发布到 GitHub Release (使用 local gh CLI)..."
    if ! command -v gh >/dev/null 2>&1; then
        echo "ERROR: 未安装 GitHub CLI (gh)，请先安装或手动发布" >&2
        exit 1
    fi
    TAG="v${VERSION}"
    if ! git rev-parse "$TAG" >/dev/null 2>&1; then
        git tag -a "$TAG" -m "jue ${TAG}"
        echo "    ✔ 创建本地标签: $TAG"
    fi
    gh release create "$TAG" "${DIST_DIR}"/* --title "jue ${TAG}" --generate-notes
    echo "    ✔ GitHub Release 发布成功: $TAG"
fi
