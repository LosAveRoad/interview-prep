"""在每份笔记页面顶部注入「下载 PDF」按钮。

按钮写在渲染后的 HTML 里，而不是 Markdown 链接：开启 directory URLs 后，
笔记页位于子目录，正确地址相对当前页向上回到 docs 再进 pdf/。
MkDocs 的链接检查按源文件位置解析，会把这条相对路径误判为越出 docs/。
"""

from pathlib import PurePosixPath
from xml.sax.saxutils import escape

PDFS = {
    "redis.md": "redis.pdf",
    "mysql.md": "mysql.pdf",
    "seckill.md": "seckill.pdf",
    "agent.md": "agent.pdf",
    "java.md": "java.pdf",
    "go.md": "go.pdf",
    "os.md": "os.pdf",
    "network.md": "network.pdf",
    "pi-agent/index.md": "pi-agent-index.pdf",
    "pi-agent/core.md": "pi-agent-core.pdf",
    "pi-agent/compaction-resume.md": "pi-agent-compaction-resume.pdf",
    "pi-agent/memory-skill.md": "pi-agent-memory-skill.pdf",
    "pi-agent/git.md": "pi-agent-git.pdf",
    "pi-agent/mcp.md": "pi-agent-mcp.pdf",
    "pi-agent/gateway.md": "pi-agent-gateway.pdf",
    "pi-agent/tui.md": "pi-agent-tui.pdf",
}


def _pdf_href_prefix(src_uri: str, use_directory_urls: bool) -> str:
    """从页面 URL 回到站点根下的 pdf/。

    directory URLs 把 docs/pi-agent/core.md 放在 /pi-agent/core/，需要 ../../pdf/。
    顶层 docs/redis.md 放在 /redis/，仍然是 ../pdf/。
    """
    parent = PurePosixPath(src_uri).parent
    depth = 0 if str(parent) == "." else len(parent.parts)
    if use_directory_urls:
        depth += 1
    return "../" * depth


def on_page_content(html, page, config, files):
    filename = PDFS.get(page.file.src_uri)
    if not filename:
        return html
    prefix = _pdf_href_prefix(page.file.src_uri, config.get("use_directory_urls", True))
    href = escape(f"{prefix}pdf/{filename}")
    banner = (
        f'<p><a class="md-button md-button--primary" href="{href}">下载 PDF</a></p>\n'
        "<p>本页 PDF 在每次推送到 <code>main</code> 时由 GitHub Actions 根据 Markdown 重新生成。</p>\n"
    )
    return banner + html
