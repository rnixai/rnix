// Package ipc — provider_lookup.go (Story 41.2 AC#3)
//
// Server 端 provider 名 → driver type 字符串的查询工具。
//
// 设计意图：dashboard 端需要知道 step-level driver type（来计算 cache hit rate
// 的正确公式 · Anthropic vs OpenAI 语义不同）。Process struct 只存 Provider 名
// （e.g. "anthropic-prod" / "deepseek"），所以需要在响应中扩展。本工具把
// providers.yaml 已 parsed 后的 ProviderConfig 切片 lazy 转成
// "provider name → driver" 映射，O(1) 查询，避免每次 step detail 请求都遍历
// providers 切片。
//
// 缓存策略：daemon 启动时通过 SetGlobalConfig 注入 GlobalConfig.Providers，
// 后续不变（providers.yaml 不支持热重载 · 改了要重启 daemon）。所以 driverTypeForProvider
// 第一次调用时构建映射并存到 s.providerDriverCache，sync.Once 保证只构建一次。
//
// 边界 case：
//   - GlobalConfig 未设置 / Providers 为 nil → 返回 ""
//   - provider name 在 config 中不存在 → 返回 ""
//   - provider name 为空 → 返回 ""
package ipc

import (
	"github.com/rnixai/rnix/drivers/llm"
)

// driverTypeForProvider 返回 provider 名对应的 driver 类型字符串（"anthropic"
// / "openai" / "claude-cli" 等）。
//
// providers.yaml 未配置该 provider 时返回 ""，由调用方处理（dashboard 会走
// fallback OpenAI 语义公式）。
//
// 注意：lazy 缓存放在调用栈第一次访问时构建。s.providerDriverCacheOnce 保证
// 多 goroutine 安全。
func (s *Server) driverTypeForProvider(providerName string) string {
	if providerName == "" {
		return ""
	}
	s.providerDriverCacheOnce.Do(s.buildProviderDriverCache)
	if s.providerDriverCache == nil {
		return ""
	}
	return s.providerDriverCache[providerName]
}

// buildProviderDriverCache 从 s.globalConfig.Providers 构建 provider name →
// driver type 的 map。仅由 sync.Once 调用一次。
func (s *Server) buildProviderDriverCache() {
	if s.globalConfig == nil {
		return
	}
	cfg, ok := s.globalConfig.Providers.(*llm.ProvidersConfig)
	if !ok || cfg == nil {
		return
	}
	cache := make(map[string]string, len(cfg.Providers))
	for _, pc := range cfg.Providers {
		if pc.Name == "" {
			continue
		}
		cache[pc.Name] = pc.Driver
	}
	s.providerDriverCache = cache
}
