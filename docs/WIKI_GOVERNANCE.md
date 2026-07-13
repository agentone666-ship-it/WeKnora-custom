# 场景驱动 Wiki 卡片与审核

## 数据流

Wiki ingest 保留原有 Summary / Entity / Concept 抽取，并在同一份资料上追加场景卡片抽取：

1. LLM 识别资料性质、业务线、场景、岗位和指标。
2. LLM 提出原子卡片及卡片关系，不直接发布。
3. 代码校验卡片类型、资料性质、Chunk 证据、不确定用语、风险边界和关系置信度。
4. 每个候选页生成 ChangeSet，根据风险进入 L0 / L1 / L2。
5. 批准时使用页面版本号做乐观锁，事务内更新页面、证据、审核记录和前后快照。
6. 批准后同步场景、全局/业务线/场景知识包和 Wiki 链接；拒绝与冲突不会修改正式页。

卡片关系只允许 `supports` / `contradicts` / `tests` / `answers` / `causes` / `depends_on` / `applies_to` / `measures` / `example_of` / `mitigates`，目标必须是本次抽取中实际存在的卡片，置信度必须不低于 0.7。关系原因和置信度同时保存在 `page_metadata.relationships`。

## 审核 API

- `GET /api/v1/knowledgebase/:kb_id/wiki/change-sets?status=pending&review_level=L1`
- `GET /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id`
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id/review`
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/batch-review`（仅 L1）
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id/rollback`（生成新的 L2 回滚候选，不直接覆盖）
- `GET /api/v1/knowledgebase/:kb_id/wiki/governance/stats`
- `POST /api/v1/knowledgebase/:kb_id/wiki/governance/reclassify?page=1&page_size=200`（分批扫描旧页来源，候选强制进入人工审核）
- `POST /api/v1/knowledgebase/:kb_id/wiki/card-candidates`（编辑者提交人工候选）
- `GET /api/v1/knowledgebase/:kb_id/wiki/card-history?slug=card/...`
- `GET /api/v1/knowledgebase/:kb_id/wiki/packages` / `GET .../scenarios`

单条审核请求：

```json
{
  "decision": "approved",
  "comment": "证据与适用范围已确认",
  "item_overrides": {
    "change-item-id": {
      "knowledge_type": "experience",
      "maturity_status": "partially_verified"
    }
  }
}
```

ChangeSet 详情包含旧值、新值、字段差异、证据 Chunk、资料性质、模型 ID、Prompt 版本和触发原因。审核列表与详情仅 KB 所有者或管理员可访问。

对于疑似重复的新卡，可在批准请求中传 `merge_into_slug`。系统仅允许同知识类型卡片合并，保留已有正式结论，只合并来源、证据、场景、岗位、指标和标题别名。

旧 Wiki 渐进迁移接口返回 `has_more` 和 `next_page`。调用方逐批推进即可；它只把旧页面的来源文档重新送入卡片抽取队列，`force_review` 会确保结果至少为 L1，原有 Summary / Entity / Concept 页面不会被自动改型或覆盖。

## 问答可见性

普通 Wiki 列表、索引、图谱、搜索、直接读页和 Agent Wiki Tool 均只返回：

- `review_status = approved`
- `maturity_status in (verified, partially_verified)`
- 已到 `effective_from` 且未超过 `effective_to`
- 非归档页

Agent 读页结果会同时带上知识类型、成熟度、解法强度、适用边界和禁止承诺。Question 不得伪造答案，Hypothesis 必须标注待验证，Experiment 必须区分预期和实际结果。

## 运行指标

`governance/stats` 返回卡片类型/成熟度分布、L0/L1/L2 与状态分布、自动发布率、审核通过/修改率、类型修正率、平均审核时长和待审积压。若问答层发现“把假设答成事实”，可通过 `POST governance/metrics` 上报 `hypothesis_answered_as_fact`，用于持续验收提示词与检索约束。

## 数据库升级

PostgreSQL 启动时会执行 `migrations/versioned/000066_wiki_governance.up.sql`。升级前建议备份数据库。该迁移不重建旧 Wiki 页；新资料立即走新流水线，旧资料重新解析时才生成卡片候选。
