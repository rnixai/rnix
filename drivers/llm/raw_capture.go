package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// 56.2 「裁决 1 并发铁律」 — driver 是跨进程共享单例（drivers/llm/factory.go
// RegisterProviders 闭包），而 raw capture 是 per-call 可变数据。把 capture
// 存 driver 字段会同时触发「跨进程数据串味」与「-race 必爆」。正解是经
// ctx-scoped sink 在调用栈内传递，driver 出口无状态填充，LLMFile 调用返回
// 后归集到 per-Open `lastRawCapture` 字段（vfsfile.go）。

// rawCaptureSink 在 LLMFile.writeCall / writeStream 调用栈内创建，经
// context.Context 下传给 driver；driver 在 SDK middleware / RoundTripper /
// 手写 HTTP 出口取出并填入唯一一次完整 RawCapture。返回前 LLMFile 把
// sink.cap 归集到 f.lastRawCapture——共享的只有 driver 函数体（无状态）。
//
// mu 防御 streaming 路径下 SDK goroutine 写 vs LLMFile goroutine 读：虽然
// channel-close 已建立 happens-before，但加锁能让 -race 检测器对 set/get
// 的语义保持显式（设计裁决 1 第 2 步要求）。
type rawCaptureSink struct {
	mu  sync.Mutex
	cap *vfs.RawCapture
}

func (s *rawCaptureSink) set(c *vfs.RawCapture) {
	s.mu.Lock()
	s.cap = c
	s.mu.Unlock()
}

func (s *rawCaptureSink) get() *vfs.RawCapture {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cap
}

// rawSinkCtxKey 是包内私有 ctx key 类型——用未导出 struct 类型规避碰撞，
// 不让外部包能往同一 ctx 注入冒名顶替的 sink（drivers/llm 包内部唯一来源）。
type rawSinkCtxKey struct{}

// withRawSink 把 sink 挂到 ctx 上往下传。LLMFile.writeCall/writeStream 应在
// 调 driver.Call/Stream 之前调用；driver 出口经 rawSinkFromContext 取回。
func withRawSink(ctx context.Context, sink *rawCaptureSink) context.Context {
	return context.WithValue(ctx, rawSinkCtxKey{}, sink)
}

// rawSinkFromContext 从 ctx 取回 sink；nil ctx / 未注入 sink / 类型不匹配
// 一律返回 nil（driver 检查 nil 即跳过捕获，零开销）。
func rawSinkFromContext(ctx context.Context) *rawCaptureSink {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(rawSinkCtxKey{}).(*rawCaptureSink); ok {
		return v
	}
	return nil
}

// --- Shared API-family helpers ----------------------------------------------
//
// 56.2 各 API driver 出口（openai-compat 手写 HTTP / openai+anthropic SDK
// middleware / gemini RoundTripper）都需要：从 *http.Request 拿 method/url/
// headers/body 字符串 → 主脱敏 → 填进 RawCapture.Request。响应侧的 body 由
// 调用方按 streaming/non-streaming 各自 tee 得到。下面 helper 集中这套
// 重复逻辑，确保字段名 / 类型 / 脱敏调用点一致（裁决 3 字段约定）。

// newAPIRawCapture 构造一个 Kind="api" 的 RawCapture，已经设好 TsMs。
// Step 字段 driver 不知道（kernel hook 填，见 kernel/raw_writer.go:250），
// 留 0 即可。Request/Response 由调用方按需填充。
func newAPIRawCapture() *vfs.RawCapture {
	return &vfs.RawCapture{
		TsMs: time.Now().UnixMilli(),
		Kind: "api",
	}
}

// redactReqHeaders 把 http.Header (canonical map[string][]string) 拍平成
// map[string]string 再走 vfs.RedactHeaders 主脱敏。多值 header 用逗号拼接
// 保留可观察性（HTTP RFC 7230 §3.2.2 允许）。
//
// 这里是 driver 层「主脱敏」唯一入口；kernel 的 redactCredentialFields 是
// 纵深防御二次脱敏，对已脱敏串短路（vfs.RedactCredential idempotent）。
func redactReqHeaders(h http.Header) map[string]string {
	flat := make(map[string]string, len(h))
	for k, vs := range h {
		switch len(vs) {
		case 0:
			flat[k] = ""
		case 1:
			flat[k] = vs[0]
		default:
			flat[k] = stringsJoinComma(vs)
		}
	}
	return vfs.RedactHeaders(flat)
}

// stringsJoinComma 本可以用 strings.Join，独立出来避开 raw_capture.go 顶部
// 增加一个 strings import 引来的回归——保持 import 列表最小化。
func stringsJoinComma(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	n := len(vs) - 1
	for _, v := range vs {
		n += len(v)
	}
	var b []byte
	if n > 0 {
		b = make([]byte, 0, n)
	}
	for i, v := range vs {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, v...)
	}
	return string(b)
}

// readAndResetReqBody 读取 *http.Request 的 body 字节并将 body 重置回可读
// 状态——SDK middleware 在 next(req) 之前消费一次 body 拿真实发送字节，但
// SDK 仍需读到原 body 把请求发出去。优先使用 req.GetBody（net/http 标准约定
// 用于重定向/重试），它返回独立可读的副本；fallback 时通过 io.ReadAll + 重置
// 为 NopCloser 完成。
//
// 返回的字节是 *实际发送* 的请求体（含 SDK 注入的所有字段）。
func readAndResetReqBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(data))
	return data, nil
}

// fillAPIRequest 把 *http.Request 填进 RawCapture.Request：method/url/headers
// 脱敏 + body 字符串（裁决 3 字段约定）。bodyBytes 由调用方提供——
// SDK middleware 路径需要先 readAndResetReqBody 然后传进来，手写 HTTP 路径
// 直接传已 marshal 的 jsonBody。
func fillAPIRequest(cap *vfs.RawCapture, req *http.Request, bodyBytes []byte) {
	if cap == nil || req == nil {
		return
	}
	url := ""
	if req.URL != nil {
		url = req.URL.String()
	}
	cap.Request = map[string]any{
		"method":  req.Method,
		"url":     url,
		"headers": redactReqHeaders(req.Header),
		"body":    string(bodyBytes),
	}
}

// fillAPIResponse 填 RawCapture.Response。bodyStr 是「底层原始字节」的字符串
// 形态（CAP-2 要求）——non-stream 由 ReadAll 得到，stream 由 tee buffer 累积
// 得到。respHeaders 可选（裁决 3 字段约定 headers 为 map[string]string）。
func fillAPIResponse(cap *vfs.RawCapture, status int, respHeaders http.Header, bodyStr string) {
	if cap == nil {
		return
	}
	resp := map[string]any{
		"status": status,
		"body":   bodyStr,
	}
	if len(respHeaders) > 0 {
		resp["headers"] = redactReqHeaders(respHeaders)
	}
	cap.Response = resp
}

// --- SDK middleware (openai-go + anthropic-sdk-go) --------------------------
//
// openai-go v3 与 anthropic-sdk-go 的 `option.Middleware` 是 type alias，
// 底层都是 `func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error)`。
// 同底层类型可以隐式赋值，所以一个核心函数能复用给两 SDK。

// captureMiddlewareFunc 是两 SDK 共享的中间件函数体——从 req.Context() 拿
// sink、读真实 req body 后归还、调 next(req)、tee resp.Body 到 sink。
//
// 时序铁律（streaming SSE 关键，AC #4）：
//   - 不能 io.ReadAll(resp.Body) —— 那会一次性吞掉流，SDK 读不到、阻塞流；
//   - 必须 io.TeeReader 包 resp.Body —— SDK 读 SSE 的同时 tee 进 sinkBuf；
//   - sink.set 在「请求阶段」就调一次写入 Request 字段；Response 字段在
//     stream 真正全部读完时通过 wrappedBody.Close() 落入 sink（close 即
//     driver goroutine 完成所有 stream.Next() 后归还 body 的时刻）。
//
// non-streaming 路径下 SDK 也会读尽 body 然后 Close()，同样 tee 完成后落
// Response.body——同一段代码无需分流。
func captureMiddlewareFunc(req *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	sink := rawSinkFromContext(req.Context())
	if sink == nil {
		return next(req)
	}
	cap := newAPIRawCapture()

	bodyBytes, _ := readAndResetReqBody(req)
	fillAPIRequest(cap, req, bodyBytes)
	// 先把 Request 填上（即便 next 出错也保留请求形态）。Response 由
	// teeReadCloser.Close 完成填充——SDK 读完 stream/body 后 Close。
	sink.set(cap)

	resp, err := next(req)
	if err != nil {
		return resp, err
	}
	if resp == nil || resp.Body == nil {
		return resp, nil
	}
	// 包 resp.Body：SDK 读 stream/body 时，tee 同步写入 sinkBuf。
	// 直到 SDK Close() 时把 sinkBuf 的内容落入 cap.Response。
	resp.Body = newCaptureResponseBody(resp, sink, cap)
	return resp, nil
}

// captureResponseBody wraps resp.Body so that SDK reads tee into a buffer
// and on Close we publish the body + status + headers into sink/cap.
type captureResponseBody struct {
	inner   io.ReadCloser
	tee     io.Reader
	buf     *bytes.Buffer
	sink    *rawCaptureSink
	cap     *vfs.RawCapture
	status  int
	headers http.Header
	closed  bool
}

func newCaptureResponseBody(resp *http.Response, sink *rawCaptureSink, cap *vfs.RawCapture) *captureResponseBody {
	buf := &bytes.Buffer{}
	return &captureResponseBody{
		inner:   resp.Body,
		tee:     io.TeeReader(resp.Body, buf),
		buf:     buf,
		sink:    sink,
		cap:     cap,
		status:  resp.StatusCode,
		headers: resp.Header,
	}
}

func (c *captureResponseBody) Read(p []byte) (int, error) {
	return c.tee.Read(p)
}

func (c *captureResponseBody) Close() error {
	err := c.inner.Close()
	if c.closed {
		return err
	}
	c.closed = true
	// SDK 读完 → Close → 此刻 buf 已含原始字节流（含 SSE 全部 chunk）。
	// 落入 cap.Response 并重新 set sink（cap 是同一指针，set 是幂等
	// publish，让 LLMFile 在 driver 返回后 get 到的就是终态 cap）。
	fillAPIResponse(c.cap, c.status, c.headers, c.buf.String())
	c.sink.set(c.cap)
	return err
}

// --- Capturing http.RoundTripper (gemini path) ------------------------------

// captureRoundTripper wraps an existing http.RoundTripper to populate raw
// capture via ctx-scoped sink. Used by gemini driver where ClientConfig.HTTPClient
// is the only injection point.
type captureRoundTripper struct {
	base http.RoundTripper
}

func newCaptureRoundTripper(base http.RoundTripper) *captureRoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &captureRoundTripper{base: base}
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	sink := rawSinkFromContext(req.Context())
	if sink == nil {
		return c.base.RoundTrip(req)
	}
	cap := newAPIRawCapture()

	bodyBytes, _ := readAndResetReqBody(req)
	fillAPIRequest(cap, req, bodyBytes)
	sink.set(cap)

	resp, err := c.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp == nil || resp.Body == nil {
		return resp, nil
	}
	resp.Body = newCaptureResponseBody(resp, sink, cap)
	return resp, nil
}

// wrapHTTPClientWithCapture 把现有 *http.Client 包成「Transport=captureRoundTripper」
// 的新 client，保留原 client 的所有其它配置（Timeout/Jar/CheckRedirect）。
//
// nil-safe：传 nil 时返回一个 *http.Client，Transport = captureRoundTripper(default)。
//
// 注意：返回新 *http.Client 而非 in-place 改 base.Transport——in-place 改会
// 污染外部传入的 client 实例（测试场景中可能被多次复用）。
func wrapHTTPClientWithCapture(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{
			Transport: newCaptureRoundTripper(nil),
		}
	}
	clone := *base
	clone.Transport = newCaptureRoundTripper(base.Transport)
	return &clone
}
