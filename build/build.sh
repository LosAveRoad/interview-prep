#!/usr/bin/env bash
# 用 pandoc + headless Chrome 把笔记 Markdown 打成 PDF。
# 在仓库根目录执行：
#   ./build/build.sh
#   ./build/build.sh docs/agent.md
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CHROME="${CHROME:-}"
if [[ -z "$CHROME" ]]; then
  for candidate in google-chrome google-chrome-stable chromium chromium-browser; do
    if command -v "$candidate" >/dev/null 2>&1; then
      CHROME="$candidate"
      break
    fi
  done
fi
if [[ -z "$CHROME" ]]; then
  echo "未找到 Chrome/Chromium，请安装后重试，或设置 CHROME=/path/to/chrome" >&2
  exit 1
fi

if ! command -v pandoc >/dev/null 2>&1; then
  echo "未找到 pandoc" >&2
  exit 1
fi

mkdir -p build docs/pdf

# headless Chrome 写完 PDF 后经常不退出。文件大小连续两次不变后结束整个进程组。
print_pdf() {
  local html="$1" pdf="$2"
  local tmp log pid i size prev
  tmp="$(mktemp -d)"
  log="${tmp}/chrome.log"
  rm -f "$pdf"
  setsid "$CHROME" \
    --headless=new \
    --no-sandbox \
    --disable-gpu \
    --disable-dev-shm-usage \
    --disable-extensions \
    --disable-background-networking \
    --disable-sync \
    --no-first-run \
    --no-default-browser-check \
    --user-data-dir="$tmp/profile" \
    --no-pdf-header-footer \
    --print-to-pdf="$pdf" \
    "file://${html}" \
    >"$log" 2>&1 &
  pid=$!
  prev=0
  for i in $(seq 1 120); do
    if [[ -s "$pdf" ]]; then
      size="$(stat -c%s "$pdf")"
      if [[ "$size" -gt 20000 && "$size" == "$prev" ]]; then
        break
      fi
      prev="$size"
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM -- "-$pid" 2>/dev/null || true
    sleep 0.5
    kill -KILL -- "-$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
  if [[ ! -s "$pdf" ]]; then
    echo "PDF 生成失败或为空: $pdf" >&2
    cat "$log" >&2 || true
    rm -rf "$tmp"
    exit 1
  fi
  rm -rf "$tmp"
}

# 顶层笔记沿用文件名；子目录笔记加上目录前缀，避免 index.md 等重名。
pdf_basename() {
  local src="$1"
  local rel="${src#docs/}"
  local base dir
  base="$(basename "${rel%.md}")"
  dir="$(dirname "$rel")"
  if [[ "$dir" == "." ]]; then
    printf '%s' "$base"
  else
    printf '%s-%s' "${dir//\//-}" "$base"
  fi
}

notes=("$@")
if [[ ${#notes[@]} -eq 0 ]]; then
  notes=(
    docs/redis.md
    docs/mysql.md
    docs/seckill.md
    docs/agent.md
    docs/java.md
    docs/go.md
    docs/os.md
    docs/network.md
    docs/terminal-agent/index.md
    docs/terminal-agent/core.md
    docs/terminal-agent/compaction-resume.md
    docs/terminal-agent/memory-skill.md
    docs/terminal-agent/git.md
    docs/terminal-agent/mcp.md
    docs/terminal-agent/gateway.md
    docs/terminal-agent/tui.md
  )
fi

for src in "${notes[@]}"; do
  if [[ ! -f "$src" ]]; then
    echo "找不到笔记: $src" >&2
    exit 1
  fi
  base="$(pdf_basename "$src")"
  html="build/${base}.html"
  pdf="docs/pdf/${base}.pdf"
  extra_style=()
  if [[ "$src" == docs/terminal-agent/* ]]; then
    extra_style=(-H build/terminal-agent-style.html)
  fi
  echo "pandoc $src -> $html"
  pandoc "$src" \
    -f markdown \
    -t html5 \
    -s \
    --toc \
    --toc-depth=3 \
    -H build/style.html \
    "${extra_style[@]}" \
    --metadata lang=zh \
    -o "$html"
  echo "chrome $html -> $pdf"
  print_pdf "${ROOT}/${html}" "${ROOT}/${pdf}"
  echo "built $pdf ($(wc -c < "$pdf") bytes)"
done
