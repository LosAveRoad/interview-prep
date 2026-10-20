"""在每份笔记页面顶部注入「下载 PDF」按钮。

按钮写在渲染后的 HTML 里，而不是 Markdown 链接：开启 directory URLs 后，
笔记页位于子目录，正确地址是 ../pdf/<name>.pdf。MkDocs 的链接检查按源文件
位置解析，会把这条相对路径误判为越出 docs/。
"""

from xml.sax.saxutils import escape

PDFS = {
    "redis-mysql.md": "redis-mysql.pdf",
    "agent.md": "agent.pdf",
    "java-go.md": "java-go.pdf",
    "computer-organization.md": "computer-organization.pdf",
}


def on_page_content(html, page, config, files):
    filename = PDFS.get(page.file.src_uri)
    if not filename:
        return html
    prefix = "../" if config.get("use_directory_urls", True) else ""
    href = escape(f"{prefix}pdf/{filename}")
    banner = (
        f'<p><a class="md-button md-button--primary" href="{href}">下载 PDF</a></p>\n'
        "<p>本页 PDF 在每次推送到 <code>main</code> 时由 GitHub Actions 根据 Markdown 重新生成。</p>\n"
    )
    return banner + html
