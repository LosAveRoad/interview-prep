# 面试八股笔记

用 [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) 托管的中文面试笔记，发布在 GitHub Pages：

<https://losaveroad.github.io/interview-prep/>

包含四份笔记，每份页面都有「下载 PDF」：

| 笔记 | 源文件 | PDF |
|---|---|---|
| Redis & MySQL | [`docs/redis-mysql.md`](docs/redis-mysql.md) | [`docs/pdf/redis-mysql.pdf`](docs/pdf/redis-mysql.pdf) |
| AI Agent | [`docs/agent.md`](docs/agent.md) | [`docs/pdf/agent.pdf`](docs/pdf/agent.pdf) |
| Java & Go | [`docs/java-go.md`](docs/java-go.md) | [`docs/pdf/java-go.pdf`](docs/pdf/java-go.pdf) |
| 计算机组成原理 | [`docs/computer-organization.md`](docs/computer-organization.md) | [`docs/pdf/computer-organization.pdf`](docs/pdf/computer-organization.pdf) |

[`seckill/`](seckill) 是 Redis & MySQL 笔记第三部分对应的 Go 秒杀示例，只供对照阅读。本地编译：

```bash
cd seckill && go build ./...
```

## 更新笔记

1. 直接改 `docs/` 下对应的 Markdown。首页入口在 [`docs/index.md`](docs/index.md)，导航在 [`mkdocs.yml`](mkdocs.yml)。
2. 本地预览：

   ```bash
   python3 -m venv .venv
   source .venv/bin/activate
   pip install -r requirements.txt
   mkdocs serve
   ```

   浏览器打开 <http://127.0.0.1:8000/>。搜索依赖 `jieba`（已写在 `requirements.txt`）。
3. 提交并推送到 `main`。GitHub Actions（[`.github/workflows/pages.yml`](.github/workflows/pages.yml)）会：
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
