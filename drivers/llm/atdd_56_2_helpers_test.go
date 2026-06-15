package llm

import (
	"os"
	"testing"

	"github.com/rnixai/rnix/vfs"
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

// openLLMFile 通过 FileFactory 产生一个真实的 LLMFile（mode=call/stream），
// 经 vfs.VFSFile 接口降级回 *LLMFile 取 raw capture——56.2 测试要绕过
// 真实 mount，但又需要 LLMFile 的 writeCall/writeStream sink 注入逻辑。
func openLLMFile(t *testing.T, drv LLMDriver, mode string) *LLMFile {
	t.Helper()
	factory := FileFactory(drv, "/dev/llm/test", mode)
	vf, err := factory("", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("FileFactory: %v", err)
	}
	f, ok := vf.(*LLMFile)
	if !ok {
		t.Fatalf("expected *LLMFile, got %T", vf)
	}
	return f
}

// writeStringReq 把 LLMRequest JSON-encode 后调 LLMFile.Write（真实路径）。
func writeStringReq(t *testing.T, f *LLMFile, req string) {
	t.Helper()
	if err := f.Write(t.Context(), []byte(req)); err != nil {
		t.Fatalf("LLMFile.Write: %v", err)
	}
}

// captureHeader 取 RawCapture.Request.headers map 里的某个 key（脱敏后），
// 失败时 t.Fatalf 详细打印 capture 形态便于排错。
func captureHeader(t *testing.T, cap *vfs.RawCapture, key string) string {
	t.Helper()
	if cap == nil || cap.Request == nil {
		t.Fatalf("RawCapture or Request is nil: cap=%+v", cap)
	}
	hdrs, ok := cap.Request["headers"].(map[string]string)
	if !ok {
		t.Fatalf("Request[headers] is not map[string]string: %T", cap.Request["headers"])
	}
	return hdrs[key]
}

// captureReqBody / captureRespBody 取请求/响应 body string（裁决 3 字段约定）。
func captureReqBody(t *testing.T, cap *vfs.RawCapture) string {
	t.Helper()
	if cap == nil || cap.Request == nil {
		t.Fatalf("RawCapture or Request is nil: cap=%+v", cap)
	}
	s, ok := cap.Request["body"].(string)
	if !ok {
		t.Fatalf("Request[body] is not string: %T", cap.Request["body"])
	}
	return s
}

func captureRespBody(t *testing.T, cap *vfs.RawCapture) string {
	t.Helper()
	if cap == nil || cap.Response == nil {
		t.Fatalf("RawCapture or Response is nil: cap=%+v", cap)
	}
	s, ok := cap.Response["body"].(string)
	if !ok {
		t.Fatalf("Response[body] is not string: %T", cap.Response["body"])
	}
	return s
}

func captureRespStatus(t *testing.T, cap *vfs.RawCapture) int {
	t.Helper()
	if cap == nil || cap.Response == nil {
		t.Fatalf("RawCapture or Response is nil: cap=%+v", cap)
	}
	s, ok := cap.Response["status"].(int)
	if !ok {
		t.Fatalf("Response[status] is not int: %T", cap.Response["status"])
	}
	return s
}
