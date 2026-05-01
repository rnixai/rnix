---
name: decompose
description: >
  将高层意图分解为子任务 DAG，通过 /dev/intent 设备进行意图分解、
  确认和执行。适用于用户需要将复杂目标拆解为可执行步骤的场景。
allowed-tools: /dev/intent/decompose /dev/intent/status /dev/intent/confirm /dev/intent/execute
system: true
metadata:
  author: rnix
  version: "1.0"
---

# Intent Decompose

## 使用场景

当用户给出一个高层目标或复杂意图时，使用此技能将其分解为结构化的子任务 DAG（有向无环图）。

## 分解规则

将高层意图分解为具体子任务时，遵循以下原则：

1. 每个子任务用 JSON 对象表示，包含 id（短标识符）、intent（具体任务描述）、depends_on（依赖的子任务 id 列表，无依赖则为空数组）
2. 子任务粒度适中——每个子任务应能由单个智能体独立完成
3. 正确声明依赖关系——有数据流依赖的任务必须声明 depends_on
4. 返回纯 JSON 数组，不要包含其他文本

示例分解结果:
```json
[
  {"id": "design", "intent": "设计数据模型和 API 接口", "depends_on": []},
  {"id": "backend", "intent": "实现后端 API 服务", "depends_on": ["design"]},
  {"id": "frontend", "intent": "实现前端界面", "depends_on": ["design"]},
  {"id": "test", "intent": "编写集成测试", "depends_on": ["backend", "frontend"]}
]
```

## 增量更新规则

当需要在已有意图树上增量更新时：

1. 分析新需求与现有子任务的关系
2. 返回合并后的完整子任务列表（包括已有的和新增的）
3. 已有子任务保持原 id 不变；新增子任务用新 id
4. 如果新需求修改了已有子任务的目标，更新其 intent 字段
5. 正确声明 depends_on 依赖关系

## 工具使用指南

### /dev/intent/decompose — 分解意图

向设备写入 JSON 对象触发分解：
```json
{"intent": "构建一个博客系统", "model": "claude-sonnet", "provider": "claude"}
```
- `intent`（必填）：高层意图描述
- `model`（可选）：用于分解的 LLM 模型
- `provider`（可选）：LLM provider 名称

返回 IntentTree JSON，状态为 `await_confirm`。

### /dev/intent/status — 查询状态

```json
{"intent_id": "intent-1"}
```
返回完整的 IntentTree 状态信息。

### /dev/intent/confirm — 确认执行

```json
{"intent_id": "intent-1"}
```
将意图从 `await_confirm` 转换为 `executing` 状态。

### /dev/intent/execute — 执行意图

```json
{"intent_id": "intent-1"}
```
使用 Reconciler 执行已确认的意图树。

## 工作流程

1. **分解** — 使用 /dev/intent/decompose 将高层意图分解为子任务
2. **审查** — 检查分解结果，确认子任务粒度和依赖关系合理
3. **确认** — 使用 /dev/intent/confirm 批准执行计划
4. **执行** — 使用 /dev/intent/execute 触发 Reconciler 按依赖顺序执行
5. **监控** — 使用 /dev/intent/status 跟踪执行进度
