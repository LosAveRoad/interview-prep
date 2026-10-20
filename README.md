# 面试八股笔记

用 [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) 托管的中文面试笔记，发布在 GitHub Pages：

<https://losaveroad.github.io/interview-prep/>

每份笔记单独成页，页面顶部有「下载 PDF」。每页只有一个一级标题，右侧目录从二级标题展开。

| 笔记 | 源文件 | PDF |
|---|---|---|
| Redis | [`docs/redis.md`](docs/redis.md) | [`docs/pdf/redis.pdf`](docs/pdf/redis.pdf) |
| MySQL | [`docs/mysql.md`](docs/mysql.md) | [`docs/pdf/mysql.pdf`](docs/pdf/mysql.pdf) |
| Go 秒杀 | [`docs/seckill.md`](docs/seckill.md) | [`docs/pdf/seckill.pdf`](docs/pdf/seckill.pdf) |
| AI Agent | [`docs/agent.md`](docs/agent.md) | [`docs/pdf/agent.pdf`](docs/pdf/agent.pdf) |
| Java | [`docs/java.md`](docs/java.md) | [`docs/pdf/java.pdf`](docs/pdf/java.pdf) |
| Go | [`docs/go.md`](docs/go.md) | [`docs/pdf/go.pdf`](docs/pdf/go.pdf) |
| 操作系统 | [`docs/os.md`](docs/os.md) | [`docs/pdf/os.pdf`](docs/pdf/os.pdf) |
| 计算机网络 | [`docs/network.md`](docs/network.md) | [`docs/pdf/network.pdf`](docs/pdf/network.pdf) |

[`seckill/`](seckill) 是 Go 秒杀笔记对应的可运行示例。笔记里的【R-N】链到 Redis 第 N 个知识点，【M-N】链到 MySQL 第 N 个知识点。本地编译：

```bash
cd seckill && go build ./...
```

## 更新笔记

1. 直接改 `docs/` 下对应的 Markdown。首页入口在 [`docs/index.md`](docs/index.md)，导航在 [`mkdocs.yml`](mkdocs.yml)。新增笔记时，同时改导航、首页卡片和 `hooks/pdf_link.py`、`build/build.sh` 里的文件列表。
2. 每份笔记只保留一个 `#` 一级标题，主题用 `##`，知识点用 `###`。多个一级标题会让 Material 右侧目录只显示第一个一级标题下面的内容。
3. 本地预览：

   ```bash
   python3 -m venv .venv
   source .venv/bin/activate
   pip install -r requirements.txt
   mkdocs serve
   ```

   浏览器打开 <http://127.0.0.1:8000/>。搜索依赖 `jieba`（已写在 `requirements.txt`）。
4. 提交并推送到 `main`。GitHub Actions（[`.github/workflows/pages.yml`](.github/workflows/pages.yml)）会：
   - 用 `build/build.sh`（pandoc 生成 HTML，headless Chrome 打印 PDF，字体为 Noto Sans CJK SC）按 Markdown **重新生成** `docs/pdf/*.pdf`；
   - `mkdocs build` 后通过 `actions/deploy-pages` 部署到 GitHub Pages。

仓库里已提交的 PDF 是初始产物。线上站点使用的是当次推送重新生成的 PDF，工作流不会把新 PDF 再提交回仓库。若希望仓库内 PDF 与线上一致，可在本地跑 `./build/build.sh` 后把 `docs/pdf/` 一并提交。

本地生成 PDF 需要 pandoc、Google Chrome / Chromium，以及中文字体（Ubuntu 安装 `fonts-noto-cjk`）：

```bash
sudo apt-get install -y pandoc fonts-noto-cjk
./build/build.sh
```

## 开启 GitHub Pages

仓库 **Settings → Pages → Build and deployment → Source** 需要选 **GitHub Actions**。选好之后，推送到 `main` 就会触发「Build and deploy Pages」。
