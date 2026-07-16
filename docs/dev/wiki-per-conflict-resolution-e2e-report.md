# Wiki 逐冲突点裁决 E2E 自测报告

- 日期：2026-07-16
- 结果：PASS
- 脚本：`scripts/e2e/wiki_per_conflict_resolution.sh`

## 场景

构造同一关联 Wiki 页面中的两处报价时长冲突：

1. 手机商品：候选 8 分钟，既有 30 分钟，选择保留候选说法。
2. 其他多品类：候选 30 分钟，既有 2 小时，选择保留既有说法。

通过真实登录、租户鉴权和 `POST /api/v1/knowledgebase/:kb_id/wiki/change-sets/:change_set_id/review` 完成审核。

## 验证结果

- ChangeSet 状态由 `pending` 变为 `applied`。
- 候选页面最终同时包含“手机 8 分钟”和“其他多品类 2 小时”。
- 关联旧页面只纠正手机时长，其他多品类旧说法保持不变，页面没有被整页归档。
- 审核记录保存两项 `conflict_choices`、每项保留版本和两条废弃说法。
- 两个页面更新均在同一事务和 ChangeSet 内完成，继续使用版本号检查。
