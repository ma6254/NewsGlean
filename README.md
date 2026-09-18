# NewsGlean

> 拾取、清洗、归档你关心的内容。把 RSS、网页、聊天机器人里的信息统一成同一个阅读流。

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue)](./LICENSE)
[![Status](https://img.shields.io/badge/status-M2进行中-orange)](#项目状态)

---

## 项目状态

**当前进度：M1（v0.1）已完成，M2 部分功能已提前落地 —— 采集链路跑通，跨渠道去重、游标持久化、稍后再阅、渠道管理已就绪。**

| 能力        | 现状                                                                 |
| ----------- | -------------------------------------------------------------------- |
| 采集契约    | `internal/source`：`Connector` / `Item` / `Cursor` / `State` + 可选 `Prober` 已定稿 |
| `feed` 渠道 | RSS 2.0 / Atom / JSON Feed 解析 + 单元测试；支持 `http` / `chromedp` / `chromedp_headed` 抓取 |
| 数据层      | GORM + SQLite，`sources` / `entries` / `source_cursors` / `entry_state` 四表，`Install()` 自动建表 + 一次性迁移 |
| 去重        | 三层去重（GUID → URL → 内容指纹），含跨渠道指纹去重                  |
| 配置        | `internal/config` + `docs/default.yml`                              |
| HTTP API    | `/api/source` CRUD、`/api/source/probe`、`/api/refresh`、`/api/entry/list`、`/api/entry/read-later`，Swagger UI `/swagger/index.html` |
| 命令        | `news-glean`（启动服务）+ `version` 子命令                           |

M1 验收（DoD）已达成：`go build` / `go vet` / `go test` 全绿，`source add feed → 手动采集 → GET /api/entry/list` 链路可用。M2 的跨渠道去重、游标持久化、「稍后再阅」、Swagger UI 已提前落地；其余 M2–M4 尚未开始，完整设计见 **[PLAN.md](./PLAN.md)**。

---

## 这是什么

RSS 只给你「条目」：标题、链接、一小段摘要，正文留在原站，读起来还得一条条点开。而很多你想看的内容根本没有 feed —— 一个只发在公众号里的号、一个没有订阅入口的新闻列表页、一个只在群里转发的消息流。

爬虫能抓到这些，但抓完就散：没有统一的已读状态、没有去重、没有归档、没有搜索。

NewsGlean 的思路是**把「内容怎么来」和「内容怎么读」彻底分开**：来源做成可插拔的适配器，下游只认统一的条目结构。于是不管内容原本来自 RSS、网页还是聊天机器人，都能进同一个阅读流，共享同一套去重、过滤、搜索、导出。

设计上的一条硬约束：**本地优先**。默认单进程、单文件数据库，不依赖任何外部服务。

---

## 计划中的能力

### 内容来源（可插拔）
- **`feed`** —— RSS 2.0 / Atom / JSON Feed，兼容 OPML 导入导出
- **`webpage`** —— 网页列表页爬取，用 CSS 选择器配置，无需改代码
- **`webhook`** —— 入站 HTTP 端点，任何脚本都能往里推内容
- **`telegram`** —— 频道/群消息采集
- **`import`** —— 从本地 Markdown / JSON 批量导入
- `qq_bot` 等更多渠道待评估，见 [PLAN.md](./PLAN.md)

### 统一的阅读体验
- **净** —— 抓取原文正文，剥离广告与导航
- **筛** —— 关键词/正则规则过滤，跨渠道去重
- **读** —— 浏览器界面 + REST API，读同一份数据
- **存** —— 导出 Markdown / JSON / EPUB
- **增强（可选）** —— LLM 摘要与分类，不配置也完整可用

服务端提供 `/api` 与 Web 界面，`news-glean list` / `read` / `search` 等一次性子命令是这套 API 的轻客户端 —— 状态只有服务一处权威，不会出现「命令行改的」和「网页看到的」不一致。

---

## 快速开始

需要 Go **1.23** 或更高版本（Windows / Linux / macOS 均可）。

```bash
git clone https://github.com/ma6254/news-glean.git
cd news-glean
go mod download
go build -o news-glean .
```

Windows 上得到 `news-glean.exe`。直接运行即启动常驻服务（首次自动建库并预置默认源），Web 界面与 Swagger 地址见启动日志：

```bash
go run . -d ./release   # Web: http://127.0.0.1:28080  Swagger: /swagger/index.html
```

<details>
<summary>期望的首次使用流程（蓝图，尚未实现）</summary>

```bash
news-glean -c ./config.yml -d ./release   # 启动服务，首次运行自动建库
news-glean source add feed --url https://example.com/atom.xml
news-glean refresh
news-glean list --unread
# Web 界面：http://127.0.0.1:28080
```

</details>

---

## 文档

| 文档                     | 内容                                                               |
| ------------------------ | ------------------------------------------------------------------ |
| **README.md**            | 项目简介（本文件）                                                 |
| **[PLAN.md](./PLAN.md)** | 设计方案与实现规格：采集抽象、数据模型、命令设计、路线图、技术选型 |

技术栈与项目结构约定参照同组织的 `bookcocoon-server`：Go + Cobra + GORM(sqlite/mysql)、标准库 `net/http` 与 swaggo、`internal/` 顶层包布局、PowerShell 构建脚本注入版本号。详见 [PLAN.md 的项目结构](./PLAN.md#项目结构)。

---

## 贡献

1. 开工前先在 Issue 里对齐范围，避免大改动返工。
2. 设计层面的讨论（尤其是 [PLAN.md](./PLAN.md) 中标注为「契约」的接口）请在动手前提出。
3. Fork → 分支 → 提交 → PR，描述写清动机与验证方式。

---

## License

[MIT](./LICENSE) © 2026 ma6254

选择 MIT 的原因：希望工具能被尽可能多的人使用与改造，不介意他人闭源使用，也不需要专利授权条款。代价是**别人可以拿这份代码闭源做商业产品而不回报**——如果将来要开托管服务或防止竞品直接套壳，需要重新考虑（GPL-3.0 / AGPL-3.0 或 open-core 边界）。
