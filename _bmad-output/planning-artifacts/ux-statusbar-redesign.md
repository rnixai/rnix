---
type: ux-design-addendum
parent: ux-design-specification.md
scope: Dashboard Status Bar Redesign
date: '2026-03-25'
author: Decker
status: implemented
decisions:
  - Layered discovery: core hints visible + ? overlay for full help
  - Process ops (K/a/l/r) removed from status bar, moved to ? help
  - Help overlay in English
  - Status bar never truncated — max 5-6 hints per mode
---

# UX 补充规范：Dashboard 状态栏重设计

## 1. 设计目标

| 目标 | 衡量标准 |
|------|---------|
| 永不溢出 | 任何视图模式下，状态栏视觉宽度 ≤ 55 字符（80 列终端安全） |
| 可发现性 | 新用户 10 秒内能找到 `?` 帮助入口 |
| 上下文感知 | 切换面板/模式时，hints 即时反映当前可用操作 |
| 视觉清晰 | 1 秒内能区分"按键"和"描述" |

## 2. 信息架构

### 2.1 分层发现模型

```
Layer 0: 状态栏（永久可见） ─── 5-6 个核心键
Layer 1: ? 帮助覆盖层（按需） ── 完整快捷键参考卡
```

**设计原则：** 状态栏只回答"我现在最可能需要按什么？"，帮助覆盖层回答"所有能按的键是什么？"

### 2.2 每个视图模式的状态栏内容

#### 默认视图 (viewDefault)

```
  j/k nav  z expand  f filter  H hist  ? help    q quit
```

5 个核心操作 + 退出 = 6 个 hint，~50 字符。

**选键逻辑：**
- `j/k` — 最高频操作（导航树）
- `z` — 面板放大/还原（用户最常问的功能）
- `f` — Timeline 过滤（上下文相关）
- `H` — 历史视图（用户关心历史数据）
- `?` — 帮助入口（可发现性保障）
- `q` — 退出（安全出口，右侧固定位置）

#### 展开面板 - Timeline Step 模式

```
  j/k nav  v detail  p prompt  s syscall  ? help    q quit
```

#### 展开面板 - Timeline Syscall 模式

```
  j/k nav  Enter detail  s step  f filter  ? help    q quit
```

#### 展开面板 - 其他面板通用

```
  j/k nav  Enter select  z restore  ? help    q quit
```

#### 过滤模式 (timelineFilterMode)

```
  l LLM  t Tool  i IPC  v VFS  a All    f/Esc done
```

无 `?` 和 `q` — 过滤模式是临时模态，Esc 是唯一出口。

#### History 视图

```
  j/k nav  Enter focus  L llm  / search  ? help    Esc back
```

#### LLM Viewer

```
  j/k scroll  h/l step  y copy  ? help    Esc close
```

状态信息（req/resp tokens）移到 LLM Viewer 的**标题栏**而非状态栏。

#### Replay 模式

```
  Space play  ,/. step  [/] speed  0 start  $ end    q quit
```

Replay 模式保持原有设计，独立于新状态栏系统。

### 2.3 状态消息 (statusMsg)

当有 statusMsg 时，替换全部 hints，仅保留 `q` 退出：

```
  ✓ Killed PID 3                                  q quit
```

TTL 到期后自动恢复正常 hints。

## 3. 视觉规范

### 3.1 Hint 渲染格式

每个 hint 由两部分组成：

```
[按键][描述]
```

- **按键部分**：`ColorAgent (#5B9BD5)` 前景色 + Bold
- **描述部分**：`ColorMuted (#666666)` 前景色
- 按键和描述之间**无空格**（紧凑感）：`jknav` → 视觉上 key 的蓝色加粗自然与灰色描述区分
- Hint 之间用**双空格**分隔

示例渲染伪码：

```
hint("j/k", "nav") → [蓝粗]j/k[灰]nav
hint("?", "help")  → [蓝粗]?[灰]help
hint("q", "quit")  → [蓝粗]q[灰]quit
```

### 3.2 整体布局

```
  ┌─ 2字符缩进 ──────────────────────────────────────────────┐
  │  {hint}  {hint}  {hint}  {hint}  {hint}    {exit-hint}   │
  │  ←── 核心操作（双空格分隔）──→  ←4空格→  ←── 退出区 ──→  │
  └───────────────────────────────────────────────────────────┘
```

- 左侧 2 字符缩进（与标题栏对齐）
- 核心操作 hints 之间双空格
- 退出区（`q quit` 或 `Esc back`）与核心区之间用 **4 空格**分隔
- 不使用 `│` 或 `║` 分隔符（减少视觉噪音）

### 3.3 宽度预算

| 模式 | 估算字符数 | 80列安全 | 60列安全 |
|------|-----------|---------|---------|
| 默认视图 | ~48 | ✅ | ✅ |
| Timeline 展开 | ~52 | ✅ | ✅ |
| 过滤模式 | ~42 | ✅ | ✅ |
| History | ~50 | ✅ | ✅ |
| LLM Viewer | ~46 | ✅ | ✅ |

## 4. `?` 帮助覆盖层设计

### 4.1 触发与退出

- **触发**：任何非模态视图下按 `?` 键
- **退出**：按 `?` 或 `Esc`
- **不可用**：过滤模式、Kill 确认、Prompt Pager 中不响应 `?`

### 4.2 布局

全屏覆盖层（类似 promptPager），使用 viewport 支持滚动。

```
╭─ Keyboard Shortcuts ─────────────────────────────────────────╮
│                                                               │
│  Navigation                  View                             │
│  j/k    Move up/down         z      Expand / restore pane     │
│  Tab    Cycle panes           1-8    Jump to pane              │
│  Enter  Expand / select      Esc    Back / close              │
│                                                               │
│  Timeline                    Process                          │
│  f      Filter mode          K      Kill process              │
│  s      Toggle step/syscall  a      Attach GDB                │
│  h/l    Scroll timeline      l      View log                  │
│  v      Expand step detail   r      Toggle recording          │
│  p      Prompt viewer                                         │
│  +/-    Zoom time bar                                         │
│                                                               │
│  Global                                                       │
│  L      LLM conversation     H      History view              │
│  ?      This help            q      Quit                      │
│                                                               │
│  Filter Mode (press f)                                        │
│  l      Toggle LLM           t      Toggle Tool               │
│  i      Toggle IPC           v      Toggle VFS                │
│  a      Enable all           Esc    Exit filter               │
│                                                               │
╰──────────────────────────── ? or Esc to close ────────────────╯
```

### 4.3 渲染规范

- **边框**：`lipgloss.RoundedBorder()`，前景色 `ColorAgent`
- **分组标题**（Navigation / View / Timeline 等）：`ColorAgent` + Bold
- **按键列**：`ColorAgent` + Bold，固定 6 字符宽，左对齐
- **描述列**：默认前景色（白色/浅灰），左对齐
- **底部提示**：`ColorMuted`，居中
- **布局**：两列并排（左列 + 右列），每列内部 key-desc 表格对齐
- **viewport**：支持 j/k 滚动（小终端高度不足时）

### 4.4 帮助内容分组

| 分组 | 包含的键 |
|------|---------|
| **Navigation** | j/k, Tab, Enter |
| **View** | z, 1-8, Esc |
| **Timeline** | f, s, h/l, v, p, +/- |
| **Process** | K, a, l, r |
| **Global** | L, H, ?, q |
| **Filter Mode** | l, t, i, v, a, Esc |

## 5. 状态指示器

### 5.1 录制指示器 (●REC)

当选中进程正在录制时，在状态栏**最左侧**显示：

```
  ●REC  j/k nav  z expand  f filter  H hist  ? help    q quit
```

- `●REC` 使用 `ColorError` (#FF6B6B) 渲染
- 与 hints 之间双空格分隔
- 录制状态优先级最高，始终可见

### 5.2 Dead 进程过滤提示

当选中进程已死亡（Timeline 自动过滤）时，在退出区前显示：

```
  j/k nav  z expand  f filter  ? help  (PID 3)    q quit
```

- `(PID N)` 使用 `ColorMuted` 渲染
- 紧跟在最后一个核心 hint 之后

## 6. 实现要点

### 6.1 新增 dashboardModel 字段

```go
helpOverlay bool  // ? 帮助覆盖层是否打开
```

### 6.2 按键路由

`?` 键在 `dashboardKey()` 的 Layer 1.5（在 Prompt Pager 之后、History 之前）处理：

```
Layer 0:  ctrl+c
Layer 1:  Prompt Pager
Layer 1.5: Help Overlay (新增)  ← ? 或 Esc 关闭
Layer 2:  History
Layer 2.5: LLM Viewer
Layer 3:  Kill 确认
Layer 4:  Replay
Layer 5:  全局快捷键 (包括 ? 打开帮助)
Layer 6:  面板内按键
```

### 6.3 renderDashboardStatus 重构

```go
func (m dashboardModel) renderDashboardStatus() string {
    // 1. statusMsg 优先
    if m.statusMsg != "" {
        return "  " + rec + statusMsg + "    " + hint("q", "quit")
    }
    // 2. 根据 viewMode 选择 hints（固定 5-6 个）
    // 3. 组装：缩进 + rec + hints + 4空格 + exitHint
    // 无需 truncateAnsi — 设计保证不溢出
}
```

### 6.4 文件变更清单

| 文件 | 变更 |
|------|------|
| `dashboard.go` | 新增 `helpOverlay` 字段、重写 `renderDashboardStatus`、新增 `renderHelpOverlay` |
| `dashboard_nav.go` | Layer 1.5 添加 `?` 键路由、帮助覆盖层按键处理 |
| `dashboard_types.go` | 无（不需要新类型） |
| `dashboard_test.go` | 更新状态栏文本断言、新增 `?` 帮助覆盖层测试 |

## 7. 验收标准

- [x] AC-1: 默认视图状态栏 ≤ 55 字符可见宽度
- [x] AC-2: 展开任何面板后状态栏 ≤ 55 字符
- [x] AC-3: 过滤模式状态栏仅显示过滤操作
- [x] AC-4: 按 `?` 打开帮助覆盖层，显示全部快捷键
- [x] AC-5: 帮助覆盖层按 `?` 或 `Esc` 关闭
- [x] AC-6: 帮助覆盖层内容为英文
- [x] AC-7: 进程操作键 (K/a/l/r) 仅在帮助覆盖层中出现
- [x] AC-8: 录制状态 ●REC 在状态栏最左侧可见
- [x] AC-9: statusMsg 显示时替换 hints，仅保留 q quit
- [x] AC-10: 80 列终端下所有模式状态栏无截断
