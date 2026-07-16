# Wiki 权威纠错自动合并 E2E 自测报告

## 结论

- 测试结果：**PASS**
- 执行时间：2026-07-15 17:42:57（Asia/Shanghai）
- 执行耗时：303 秒
- 测试分支：`codex/wiki-two-level-review`
- 环境：本地开发模式、PostgreSQL、Redis、Qdrant、真实 Embedding/LLM 调用

系统成功将新版正式制度中“现更正为 5 个工作日、原 3 个工作日规定废止、以本版本为准”识别为对已发布规则卡的权威明确纠错。即使 LLM 改写了候选标题，系统仍通过高置信度跨页关系找到原卡，保留原卡身份与适用边界，生成并自动应用 `L0/correction` ChangeSet。旧说法、新说法、原始 Chunk 证据和前后快照均已留存，且可生成需人工审核的回滚候选。

## 测试流程

1. 注册临时 E2E 用户，为租户配置真实向量和 LLM 模型。
2. 通过 API 创建开启 Wiki 和 Qdrant 向量索引的临时知识库。
3. 导入基准正式制度，明确普通服务请求的响应时限为 3 个工作日。
4. 等待解析和 Wiki 抽取完成，通过 L1 审核建立正式规则卡。
5. 导入更新的正式制度，原文明确更正为 5 个工作日并废止旧说法。
6. 断言系统自动生成并应用 `L0/correction`，原因包含 `authoritative_explicit_correction`。
7. 断言变更项更新同一 card slug、页面版本递增、系统审核的 `resolution=automatic_correction`。
8. 断言审核日志同时包含保留说法与被淘汰说法，卡片历史可回溯。
9. 调用回滚接口，断言只生成 `L1/correction/pending` 候选，不直接覆盖正式页。

## 关键结果

| 项目 | 实际结果 |
| --- | --- |
| 基准资料 | `completed` |
| 权威纠错资料 | `completed` |
| 目标卡片 | `card/rule-普通服务请求响应时限标准` |
| 自动纠错 ChangeSet | `0869e75e-11bf-401c-b4fa-3580e1b7fdc0` |
| 审核级别/分类/状态 | `L0 / correction / applied` |
| 系统裁决 | `automatic_correction` |
| 卡片历史 | PASS |
| 回滚候选 | `L1 / correction / pending` |

## 覆盖的接口

- `POST /api/v1/auth/register` / `POST /api/v1/auth/login`
- `POST /api/v1/knowledge-bases`
- `POST /api/v1/knowledge-bases/:id/knowledge/manual`
- `GET /api/v1/knowledge/:id`
- `GET /api/v1/knowledgebase/:id/wiki/change-sets`
- `GET /api/v1/knowledgebase/:id/wiki/change-sets/:changeSetId`
- `POST /api/v1/knowledgebase/:id/wiki/change-sets/:changeSetId/review`
- `GET /api/v1/knowledgebase/:id/wiki/card-history`
- `POST /api/v1/knowledgebase/:id/wiki/change-sets/:changeSetId/rollback`

## 复现

```bash
E2E_EMAIL='临时测试账号' \
E2E_PASSWORD='临时测试密码' \
E2E_EMBEDDING_MODEL_ID='可用的向量模型 ID' \
E2E_SUMMARY_MODEL_ID='可用的 LLM 模型 ID' \
scripts/e2e/wiki_automatic_correction.sh
```

脚本不输出登录令牌、密码或模型密钥。
