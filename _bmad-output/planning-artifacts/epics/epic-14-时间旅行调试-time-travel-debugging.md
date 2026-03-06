# Epic 14: 时间旅行调试

用户可以录制智能体的完整执行历史并持久化，回放和反向追踪执行轨迹，查看任意时间点的上下文 diff，在历史分叉点探索替代执行路径。

## Story 14.1: 执行录制与持久化

As a 平台构建者,
I want 对指定智能体开启完整执行录制，将所有 syscall、LLM 调用、上下文变更持久化到磁盘,
So that 我可以在智能体完成后离线分析其完整执行历史。

**Acceptance Criteria:**

**Given** 一个 Running 状态的智能体进程
**When** 用户执行 `rnix record <pid>` 或在 agdb 中执行 `record start`
**Then** 系统开始捕获该进程的所有 DebugEvent 并写入磁盘

**Given** 录制进行中
**When** 智能体完成执行或用户停止录制
**Then** 录制数据持久化到 `$PROJECT/.rnix/records/<pid>-<timestamp>/` 目录
**And** 格式为 JSON Lines（每行一个事件），包含完整的 syscall 序列、上下文快照和 LLM 响应

**Given** 录制已开启
**When** 智能体正常执行推理循环
**Then** 录制性能开销 <= 20%（NFR32）

## Story 14.2: 录制回放与导航

As a 平台构建者,
I want 回放录制的执行轨迹，支持正向播放、反向单步和任意跳转到指定时间点,
So that 我可以自由地浏览智能体的历史执行过程。

**Acceptance Criteria:**

**Given** 存在一个有效的录制文件
**When** 用户执行 `rnix replay <record-id>`
**Then** 系统加载录制数据并进入回放界面

**Given** 用户在回放界面中
**When** 用户执行正向播放
**Then** 系统按时间顺序逐步展示每个 DebugEvent 的详细信息

**Given** 用户在回放界面中
**When** 用户执行反向单步
**Then** 系统回退到上一个 DebugEvent，显示该时间点的完整状态

**Given** 用户在回放界面中
**When** 用户跳转到指定时间点或事件编号
**Then** 系统立即定位到该时间点并显示对应状态

## Story 14.3: 上下文快照对比

As a 平台构建者,
I want 在回放过程中查看任意两个时间点之间的上下文差异,
So that 我可以准确理解哪个步骤导致了上下文的关键变化。

**Acceptance Criteria:**

**Given** 用户在回放界面选中两个时间点 T1 和 T2
**When** 用户执行 `diff T1 T2`
**Then** 系统展示两个时间点之间的上下文差异，高亮标记新增、删除和修改的内容

**Given** 用户查看上下文 diff
**When** diff 包含 token 消耗变化
**Then** 同时显示各段 token 增减量

## Story 14.4: Fork-Continue 分支探索

As a 平台构建者,
I want 在回放的任意时间点创建新分支，修改上下文后重新执行（产生真实 LLM 调用）,
So that 我可以验证"如果当时做了不同决定会怎样"。

**Acceptance Criteria:**

**Given** 用户在回放界面的某个时间点
**When** 用户执行 `fork`
**Then** 系统从该时间点恢复上下文快照，CtxAlloc 新上下文空间并回放消息历史

**Given** fork 创建的新上下文
**When** 用户修改上下文内容并执行 `continue`
**Then** 系统 Spawn 新进程（PPID 指向原录制进程 PID），进入正常 reasonStep 循环产生真实 LLM 调用

**Given** fork 产生的新进程
**When** 新进程执行完成
**Then** 用户可以通过 `rnix ps` 和 `rnix strace` 正常查看该分支进程
