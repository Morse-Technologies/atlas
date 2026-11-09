<div align="center">

# Atlas

### 面向任何组织的文档助手

**索引。提问。引用。审计。**  
文档留在原处。每个回答都附带出处。

[快速开始](#快速开始) | [架构](#架构) | [开发](#开发)

**阅读语言：** [English](README.md) | [Español](README.es.md) | [中文](README.zh.md)

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?style=for-the-badge&logo=nextdotjs&logoColor=white)](https://nextjs.org)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org)
[![TailwindCSS](https://img.shields.io/badge/Tailwind-4-06B6D4?style=for-the-badge&logo=tailwindcss&logoColor=white)](https://tailwindcss.com)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org)

**Apache-2.0 核心** | Go | Next.js | Postgres | 本地部署

</div>

---

> **Atlas** 就地索引组织的文档（网络文件夹、共享盘、规程、手册、标准、邮件），用自然语言回答问题，并给出精确引用：文档、版本、页码和章节。证据不足时，它会直说。由 [Morse Technologies](https://morsetechnologies.es) 打造，服务于需要可信、可追溯答案的团队。

---

## Atlas 是什么？

Atlas 是一个**本地部署的文档助手**。它读取你的数据源而不移动文件，自动保持索引最新，让人们像问同事一样提问。

每个回答都链接到其出处段落。权限继承自数据源，并在搜索前生效，因此每个人只会看到自己本来就能打开的文档中的答案。每次问答都会记录在案以供审计，符合欧盟 AI 法案的可追溯性要求。

模型在本地运行。任何文档都不会离开你的网络。

```bash
# 前端（模拟数据，无需后端）
cd app && pnpm install && pnpm dev
# 应用地址 http://localhost:3000

# 后端（Postgres、解析器、atlas 角色）
cd server && make up
# API 地址 http://localhost:8080
```

---

## 功能一览

| 功能 | Core | Pro | 说明 |
|--------|------|-----|-------------|
| **提问** | 是 | 是 | 自然语言提问，流式回答并附编号引用。 |
| **文档** | 是 | 是 | 浏览索引，在被引用的页面打开查看器，查看元数据和版本。 |
| **数据源** | 是 | 是 | 连接本地和网络文件夹；Atlas 监视它们并重新索引变更。 |
| **用户与设置** | 是 | 是 | 本地账户、角色、双因素登录、符合 GDPR 的删除。 |
| **仪表盘** | 否 | 是 | 每日活动、覆盖率和未决问题概览。 |
| **质量告警** | 否 | 是 | 过期版本、相互矛盾、文档间失效的引用。 |
| **知识图谱** | 否 | 是 | 文档及其提及实体的关系图。 |
| **审计报告与 SSO** | 否 | 是 | 可导出的审计记录，LDAP 和 Entra ID 登录。 |

*Pro 功能通过离线签名许可证解锁。界面会锁定这些功能，后端同样强制执行。*

---

## 解决的问题

- **答案埋在文件里。** 为找一个数值要打开十个 PDF。Atlas 找到段落并直接展示。
- **答案无法核实。** 通用聊天工具没有出处也说得很笃定。Atlas 从不在没有出处时作答。
- **文档逐渐失准。** 旧版本、相互冲突的规程、丢失的引用。Atlas 会在审计之前把它们找出来。
- **数据不能外流。** 一切运行在你的服务器上，必要时可完全离线。

---

## 架构

按层组织的单仓库：后端、前端、部署。

```
server/           Go 后端：单一二进制 (atlas)，角色 api、worker、watch、migrate
  api/            openapi.yaml，与前端共享的契约
  db/             SQL 迁移与查询 (sqlc)
  internal/       模块：access、catalog、documents、events、jobs、sources、httpapi
app/              Next.js 前端：提问、文档、数据源、设置、登录
  src/lib/api/    类型化的 AtlasApi 接口，以及接入 API 前使用的 mock 适配器
  src/ui/         原子、单元、组件、模板
deploy/compose/   Docker Compose：Postgres 18 + pgvector、docling-serve、Gotenberg、atlas 角色
```

Postgres 存放数据、向量、全文检索、任务队列 (River) 和审计日志。解析 (Docling)、格式转换 (Gotenberg)、嵌入和 LLM 作为独立服务运行，通过 HTTP 调用。

---

## 快速开始

### 1. 环境要求

- **Go 1.27+**
- **Node 20+** 和 **pnpm**
- **Docker** 及 Compose

```bash
git clone <repo-url>
cd atlas
```

### 2. 前端

```bash
cd app
pnpm install
pnpm dev
```

应用地址 **http://localhost:3000**。前端使用类型化的 API 模拟和一个虚构的演示组织，因此无需后端即可运行。

### 3. 后端

```bash
cd deploy/compose
cp .env.example .env          # 设置密码
docker compose up -d --build
```

然后创建组织并添加数据源：

```bash
docker compose exec worker /atlas setup --org "我的组织"
docker compose exec worker /atlas source add --name Docs --path /sources/docs
docker compose exec worker /atlas quarantine list
```

把文档放进 `deploy/compose/sources/`（或设置 `ATLAS_SOURCES_DIR`）。API 地址 **http://localhost:8080**。

---

## 影响与受众

- **质量、合规和 IT 团队**，需要可追溯的答案和审计记录。
- **每天快速提问的人**，需要正确的版本，以及能证明它的那一页。
- **任何文档分散的组织**：工业、律所、医疗、公共部门、服务业、教育。

---

## 开发

```bash
# 后端
cd server
make gen                # 重新生成 sqlc 查询和 OpenAPI 服务端
make lint               # golangci-lint
make test               # 单元测试和架构测试
make test-integration   # 通过 testcontainers 使用真实 Postgres (需要 Docker)

# 前端
cd app
pnpm lint
pnpm build
sh scripts/check-punct.sh   # 项目标点规则
```

代码规则：函数不超过 20 行，参数不超过 3 个，命名清晰，没有魔法数字。

---

## 许可证

核心采用 Apache-2.0。Atlas 名称和标志是 Morse Technologies 的商标，不在代码许可证范围内。Atlas Pro 为商业版本。

---

<div align="center">

**Atlas** 由 Morse Technologies 出品 | Go | Next.js | Postgres | 本地部署

</div>
