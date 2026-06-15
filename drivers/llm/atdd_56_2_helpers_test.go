package llm

import (
	"os"
	"testing"
)

// mustReadFile 读 drivers/llm 包下的源文件用于文件级断言（AC #9
// drivers/llm 不导入 kernel 的 grep 保险绳等）。t.Helper 让失败定位
// 落到调用点。
//
// 仅 56.2 ATDD 测试使用（atdd_56_2_*_test.go）。
func mustReadFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
