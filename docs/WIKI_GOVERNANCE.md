# 场景驱动 Wiki 卡片与审核

## 数据流

Wiki ingest 保留原有 Summary / Entity / Concept 抽取，并在同一份资料上追加场景卡片抽取：

1. LLM 识别资料性质、业务线、场景、岗位和指标。
2. LLM 提出原子卡片及卡片关系，不直接发布。
3. 代码校验卡片类型、资料性质、Chunk 证据、不确定用语、风险边界和关系置信度。
4. 每个候选页生成 ChangeSet：L0 自动合并，L1 进入人工审核。审核等级只表达是否需要人工，`change_category` 独立表达新增、更新、冲突、纠错、淘汰或合并/重复。
5. 批准时使用页面版本号做乐观锁，事务内更新页面、证据、审核记录和前后快照。
6. 批准后同步场景、全局/业务线/场景知识包和 Wiki 链接；拒绝与冲突不会修改正式页。

卡片关系只允许 `supports` / `contradicts` / `tests` / `answers` / `causes` / `depends_on` / `applies_to` / `measures` / `example_of` / `mitigates`，目标必须是本次抽取中实际存在的卡片，置信度必须不低于 0.7。关系原因和置信度同时保存在 `page_metadata.relationships`。

## 审核 API

- `GET /api/v1/knowledgebase/:kb_id/wiki/change-sets?status=pending&review_level=L1`
- `GET /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id`
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id/review`
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/batch-review`（仅非冲突 L1；冲突必须逐条裁决）
- `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id/rollback`（生成新的 L1 纠错候选，不直接覆盖）
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

冲突变更必须额外提交 `resolution`：

- `keep_existing`：保留既有说法并拒绝候选。
- `adopt_candidate`：采用新说法，将冲突旧页面标记为过期并归档。
- `edit_candidate`：通过 `item_overrides` 编辑正文后采用，并淘汰冲突旧页面。
- `split_scope`：通过 `item_overrides.applicability` 拆分适用范围，双方同时保留。
- `defer`：只记录审核意见，ChangeSet 继续保持待审核。

审核记录保存 `retained_claim`、被淘汰说法、原因、审核人和前后页面快照。采用新版本时，系统在同一事务内为每个被淘汰页面追加 `archive/retirement` ChangeItem，写入 `superseded_by` 关系和失效时间。因此旧说法退出普通检索，但仍可通过页面历史查看，并可从原 ChangeSet 创建回滚候选。

对于疑似重复的新卡，可在批准请求中传 `merge_into_slug`。系统仅允许同知识类型卡片合并，保留已有正式结论，只合并来源、证据、场景、岗位、指标和标题别名。

### 明确纠错的 L0 自动合并

“纠错”不默认等于人工审核。当下列条件全部成立时，系统将已有卡片的明确纠错判定为 `L0/correction`，自动合并：

- 候选卡和正式卡是同一 slug，知识类型、业务线、解法强度、适用范围和风险边界均未改变。
- 新来源是 `official_rule` 或 `business_manual`，权威等级不低于旧来源，且 `source_updated_at` 严格晚于已发布版本。
- 引用的原始 Chunk 证据中明确出现“现更正为”、“废止”、“以本版本为准”等替代指令；不采信 LLM 自己补写的更正措辞。
- 不含“可能/推测/有待验证”等不确定性，不涉及价格、退款、合同、合规、隐私、安全、权限或对外承诺。
- 跨页校验没有失败、缺失或不确定结果。如需淘汰由旧资料派生的其他页，每个关系必须是重叠适用范围、置信度不低于 0.9，且其证据来源属于被更正的旧版资料。

自动纠错仍会生成完整 ChangeSet 和系统审核记录，`resolution=automatic_correction`。旧说法、新说法、字段差异、证据、来源时间和页面快照都会保留；被明确取代的旧页会在同一事务内归档退出普通检索，并可通过原 ChangeSet 生成 L1 回滚候选。任一条件不满足时，纠错保持 `L1`，由人工决定保留哪个说法。

旧 Wiki 渐进迁移接口返回 `has_more` 和 `next_page`。调用方逐批推进即可；它只把旧页面的来源文档重新送入卡片抽取队列，`force_review` 会确保结果至少为 L1，原有 Summary / Entity / Concept 页面不会被自动改型或覆盖。

## 问答可见性

普通 Wiki 列表、索引、图谱、搜索、直接读页和 Agent Wiki Tool 均只返回：

- `review_status = approved`
- `maturity_status in (verified, partially_verified)`
- 已到 `effective_from` 且未超过 `effective_to`
- 非归档页

Agent 读页结果会同时带上知识类型、成熟度、解法强度、适用边界和禁止承诺。Question 不得伪造答案，Hypothesis 必须标注待验证，Experiment 必须区分预期和实际结果。

## 运行指标

`governance/stats` 返回卡片类型/成熟度分布、L0/L1 与状态分布、自动发布率、审核通过/修改率、类型修正率、平均审核时长和待审积压。若问答层发现“把假设答成事实”，可通过 `POST governance/metrics` 上报 `hypothesis_answered_as_fact`，用于持续验收提示词与检索约束。

## 数据库升级

PostgreSQL 启动时会依次执行 Wiki governance 迁移，其中 `000068_wiki_two_level_review.up.sql` 增加业务变更分类和冲突审计字段，并将历史 L2 记录归一化为 L1。升级前建议备份数据库。迁移不物理删除旧 Wiki 页。
