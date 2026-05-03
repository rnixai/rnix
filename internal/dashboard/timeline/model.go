// Package timeline — TimelineModel 实现 PaneModel + Searchable 接口（Story 38-5 PR4）。
//
// PR4 阶段渐进落地：
//   - PR4 Step 1: 仅 TimelineState + 类型定义（本 commit）；
//   - PR4 Step 2: TimelineModel.KeyLayer() 抽离；
//   - PR4 Step 3: TimelineModel struct 实现 PaneModel + View()/Update() 抽离；
//   - PR4 Step 4: Searchable.SearchableLines() 实现，接入 SearchPlugin。
//
// PR1 创建空占位；PR4 Step 1 阶段保持空占位（实现行为留 Step 3）。
package timeline
