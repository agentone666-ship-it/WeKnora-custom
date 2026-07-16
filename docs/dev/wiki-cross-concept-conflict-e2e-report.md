# 跨概念 Wiki 冲突检测 E2E 自测报告

## 结论

- 测试结果：**PASS**
- 执行时间：2026-07-15 15:57:17（Asia/Shanghai）
- 执行耗时：485 秒
- 测试分支：`codex/wiki-two-level-review`
- 测试环境：本地开发模式、PostgreSQL、Redis、Qdrant、真实 Embedding/LLM 调用

系统成功把第二份资料与不同概念下的既有 Wiki 页面进行内容级比较，识别出“退款应在 5 个工作日到账”与既有“3 个工作日到账”规则的冲突，并生成 `L1 + conflict` 人工审核 ChangeSet。测试随后选择采用新版本，系统原子发布候选、淘汰 4 个冲突旧页面、保存审核取舍和前后快照；旧页面不再出现在普通 Wiki 检索中，但仍可通过历史接口回溯。测试请求、令牌和模型密钥均未写入报告。

## 测试场景

1. 通过认证接口登录临时 E2E 用户。
2. 通过知识库接口创建启用向量、关键词和 Wiki 索引的临时知识库。
3. 上传基准资料《财务退款结算时限制度》，规定相同适用范围内退款应在 3 个工作日到账。
4. 等待资料解析完成，并确认已生成 Entity/Concept 页面。
5. 上传不同标题、不同概念的《客服售后响应与到账规范》，规定相同适用范围内退款应在 5 个工作日到账。
6. 等待资料解析完成，按 `review_level=L1&change_category=conflict` 查询 ChangeSet 列表和详情。
7. 断言 ChangeSet 包含跨页面冲突原因、结构化判断结果和相关页面证据。
8. 提交 `adopt_candidate` 裁决和最终保留说法。
9. 断言候选已发布、冲突旧页面已淘汰、审核日志完整、普通检索不可见且历史仍可回溯。

## 接口覆盖

| 接口 | 用途 | 结果 |
| --- | --- | --- |
| `POST /api/v1/auth/login` | 获取 E2E 用户令牌 | PASS |
| `POST /api/v1/knowledge-bases` | 创建临时 Wiki 知识库 | PASS |
| `POST /api/v1/knowledge-bases/:id/knowledge/manual` | 依次导入基准资料和冲突资料 | PASS |
| `GET /api/v1/knowledge/:id` | 轮询两份资料解析状态 | PASS |
| `GET /api/v1/knowledgebase/:id/wiki/pages` | 确认首份资料已生成可比较页面 | PASS |
| `GET /api/v1/knowledgebase/:id/wiki/change-sets` | 按 L1 和冲突分类查询候选 | PASS |
| `GET /api/v1/knowledgebase/:id/wiki/change-sets/:changeSetId` | 校验原因、判断和证据详情 | PASS |
| `POST /api/v1/knowledgebase/:id/wiki/change-sets/:changeSetId/review` | 采用新说法并淘汰旧版本 | PASS |
| `GET /api/v1/knowledgebase/:id/wiki/card-history` | 验证旧版本仍可回溯 | PASS |

## 关键断言

| 断言 | 实际结果 |
| --- | --- |
| 基准资料解析完成 | `completed` |
| 冲突资料解析完成 | `completed` |
| 首份资料产生 Entity/Concept 页面 | 4 个 |
| 产生跨页面冲突审核项 | `cross_page_claim_conflict` |
| 审核等级与业务分类 | `L1 / conflict` |
| 结构化跨页面判断 | 4 组 |
| 相关页面证据快照 | 4 组 |
| 审核后 ChangeSet 状态 | `applied` |
| 被淘汰旧页面 | 4 个 |
| 保存最终说法和淘汰说法的审核日志 | 1 条 |
| 旧页面退出普通检索 | PASS |
| 旧页面历史可回溯 | PASS |

本次 E2E 生成的 ChangeSet 原因如下：

```json
[
  "cross_page_claim_conflict",
  "high_risk_content",
  "missing_scenario",
  "new_authoritative_card"
]
```

## E2E 发现并修复的问题

此前的真实 PostgreSQL E2E 暴露并修复了 ChangeItem 空 JSON 被写为 SQL `NULL` 的问题。本轮进一步覆盖两级审核和版本取舍：所有新 ChangeSet 只产生 L0/L1；业务性质由独立分类表达；采用新说法后，旧页面通过同一个 ChangeSet 的 `archive/retirement` Item 留存快照、失效时间和 `superseded_by` 关系。

## 复现方式

服务和依赖启动后运行：

```bash
E2E_EMAIL='临时测试账号' \
E2E_PASSWORD='临时测试密码' \
E2E_EMBEDDING_MODEL_ID='可用的向量模型 ID' \
E2E_SUMMARY_MODEL_ID='可用的 LLM 模型 ID' \
scripts/e2e/wiki_cross_concept_conflict.sh
```

脚本输出脱敏 JSON 结果，不输出登录令牌、密码或模型密钥。默认保留临时知识库便于人工复核；设置 `E2E_CLEANUP=1` 可在退出时清理。
