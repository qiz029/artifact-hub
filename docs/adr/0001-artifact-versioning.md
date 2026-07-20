# ADR 0001: Artifact 版本化(Series)

- 状态:已接受
- 日期:2026-07-20

## 上下文

Artifact Hub 的核心约束是 artifact 不可变:API 没有 update 路由,Postgres trigger(`artifacts_are_immutable`)拒绝任何 `UPDATE artifacts`。同时 `(collection_id, slug)` 上有唯一约束,同 collection 同 slug 重传会直接 409。

这导致"更新文档"这个最自然的用户动作没有落地方式:用户改完文档想再传一次,只能换 slug 或先删除——两种做法都会让旧公开链接失效或指向错误内容。

同时我们计划支持 artifact 之间的双向链接(从文档内容中自动提取对 `/a/{id}` 公开地址的引用,形成独立的 linkbase)。没有版本概念时,链接会钉死在某个具体版本上:目标文档一"更新"(实为新 artifact),链接和反向链接就全部指向过时版本,用户感知为"链接坏了"。

因此版本化是双向链接的前置:链接需要指向"逻辑文档"而不是某个版本快照。

## 决策

引入 series 概念:一个逻辑文档 = 一个 series,每次上传 = series 下的一个不可变版本。

### Schema(migration `004_artifact_versions.sql`)

- `artifacts` 增加 `series_id uuid NOT NULL`(逻辑文档 id,series 内所有版本共享)和 `version int NOT NULL DEFAULT 1`。
- 存量数据迁移:`series_id = id`,`version = 1`,即每个现有 artifact 自成一个 series。
- 唯一约束从 `(collection_id, slug)` 改为 `(collection_id, slug, version)`;另建 `series_id` 索引。
- 不可变 trigger 保持不变——每个版本仍然 immutable。

### 上传语义

同 collection 同 slug 重传,不再返回 409,而是自动创建新版本:`version = max(version) + 1`,继承已有 `series_id`。新 slug 则开启新 series(`series_id = 新 artifact 的 id`)。用户无需理解 version 概念,"改完再传一次"即可。

### URL 与读取语义

- `/a/{id}/{slug}` 语义不变:**id 永久钉住某个版本**,已分享的链接永远指向当时的内容,可安全引用。
- 列表接口默认只返回每个 series 的最新版本。
- artifact 详情响应增加 `seriesId`、`version` 字段。
- 新增 `GET /api/artifacts/{artifactID}/versions`,返回该 series 的全部版本(供前端版本切换)。
- 删除仍按单个版本删除,行为不变。

### 为双向链接预留的接口

后续 linkbase 表(`artifact_links`)的目标列将存 `target_series_id` 而非 artifact id:目标文档出新版本时,引用自动解析到最新版,链接不过时。本 ADR 不包含链接表本身,将另立 ADR / 实现。

## 后果

正面:

- "更新文档"成为一等操作,且零用户学习成本。
- 不可变约束与永久链接承诺都得到保留,反而被强化(旧版本永远可访问)。
- 为双向链接、以及更远的 span 级链接 / transclusion 类能力打了地基。

负面 / 代价:

- 同 slug 重传从报错变为成功,属于行为变化(视为改进,但需要在 changelog / 文档中说明)。
- 存储随版本数线性增长;当前内容为自托管小团队场景,可接受,后续可加版本清理策略。
- 列表查询从"全表扫描"变为"按 series 取最新",SQL 复杂度略增(`DISTINCT ON` 或窗口函数)。
