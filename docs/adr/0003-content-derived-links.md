# ADR 0003: 内容派生的双向链接(Linkbase)

- 状态:已接受
- 日期:2026-07-20
- 前置:ADR 0001(版本化)、ADR 0002(幂等上传)

## 上下文

我们希望支持 artifact 之间的双向链接:A 引用 B 时,B 能自动看到"被 A 引用"。

第一直觉是提供手动建链的 API + UI(详情页选择目标 artifact)。但这与现有用户动线不符:用户的习惯是"写文档 → 上传 → 拿稳定链接 → 分享",中间没有任何管理动作;要求用户上传后再去详情页点选关联,是给 curator 而非现有用户设计的功能。

而用户的文档里天然就有链接——markdown/html 中引用另一个 artifact 的公开地址(`/a/{id}/{slug}`),这本身就是用户已经在做的动作。链接应当从内容里自动长出来。

设计上受 Project Xanadu 启发:链接是独立于内容存储的一等数据(linkbase),正因如此才能双向可见、可独立演化。本方案中,内容提取只是"收割"用户已写下的链接进入 linkbase,表结构本身与内容解耦,将来支持手动/typed 链接不需要推翻设计。

## 决策

### Schema(migration `005_artifact_links.sql`)

```sql
artifact_links (
    source_artifact_id uuid REFERENCES artifacts(id) ON DELETE CASCADE,
    target_series_id   uuid,          -- 无 FK,见下
    kind               text DEFAULT 'content',
    created_at         timestamptz,
    PRIMARY KEY (source_artifact_id, target_series_id)
)
```

- **链接目标指向 series,不指向具体版本**(ADR 0001 的核心动机):目标文档出新版本时,引用自动解析到最新版,链接不过时。`target_series_id` 无法建外键(series_id 在 artifacts 中不唯一),孤儿目标由删除时清理兜底(见下)。
- **来源指向具体版本**,级联删除免费获得。
- `kind` 列预留给未来的手动/typed 链接,当前恒为 `'content'`。
- 一行存一条边,"双向"在读取时通过两个方向的查询体现,不维护冗余反向行。

### 提取与替换策略

- 上传成功(新 series 或新版本)后,在同一事务内扫描内容字节,正则 `/a/([0-9a-fA-F-]{36})` 匹配相对与绝对公开地址,对所有 artifact 类型一刀切(markdown/html 为主,json/csv 误报成本低)。
- 不存在的 UUID、指向自身 series 的链接,在 SQL 层过滤;去重由主键 + `ON CONFLICT DO NOTHING` 完成。
- **替换策略**:新版本上传时,先删除该 series 所有旧版本作为来源的链接行,再插入新提取的集合。因此表中任何时刻只有各 series 最新版本的链接,读取侧无需版本判断;"旧版本提过、新版本删了"的链接不会残留。
- 幂等重放(ADR 0002)在建行之前返回,天然不触碰链接状态。

### 读取与展示

- `GET /api/artifacts/{id}` 响应增加 `links`(该 series 引用的目标,解析到目标 series 的最新版本)与 `backlinks`(引用者,按来源 series 去重、解析到其最新版本)。列表接口不携带,保持轻量。
- 前端详情面板展示"引用了 / 被引用于"两组,点击可跨 collection 跳转。
- 删除 artifact 时:作为来源的链接由 `ON DELETE CASCADE` 清理;若删除的是某 series 的最后一个版本,同时清理指向该 series 的入站链接。
- 公开渲染页(`/a/{id}` 的服务端渲染 HTML)**暂不注入**反向链接:四条渲染路径无公共模板,注入需要在缓存内容路径上加查询,收益不成比例。后续如需要,先做公共页面包装再统一注入。

## 后果

正面:

- 用户零操作获得双向导航:在 A 里贴 B 的链接、上传 A,B 即显示"被 A 引用"。
- 链接随文档演进自动指向最新版本,不会腐烂(Xanadu 的"链接永不失效"性质)。
- 替换策略保证链接表始终反映各文档最新版本的内容,无残留、读取简单。

负面 / 代价:

- 链接是整文档粒度,不是 Xanadu 的 span 级;真需要细粒度时需另设锚点机制。
- 内容中引用被删除后(传新版本),反向链接即消失——这是"链接反映当前事实"的取舍,不保留引用历史。
- 提取只在上传时发生:若 A 引用的目标在 A 上传之后才创建(先引用后上传),该链接不会被补建。可接受,后续可加重建工具。
