package email

import (
	"bufio"
	"context"
	"errors"
	"mime"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// fakeSMTPServer 是一个最小 SMTP 服务器，仅实现 PLAIN 流程（无 STARTTLS），
// 用于在单测中通过注入 dialer 来验证邮件内容与收件人。
type fakeSMTPServer struct {
	ln   net.Listener
	wg   sync.WaitGroup
	mu   sync.Mutex
	data []string // 每个 DATA 事务的原始内容
	rcpt []string // 收到的所有 RCPT TO

	// rejectRcpt 非空时，对此地址的 RCPT TO 返回 550，用于模拟单个收件人失败。
	rejectRcpt string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{ln: ln}
	s.wg.Add(1)
	go s.serve()
	t.Cleanup(s.Close)
	return s
}

func (s *fakeSMTPServer) addr() string { return s.ln.Addr().String() }

func (s *fakeSMTPServer) Close() {
	_ = s.ln.Close()
	s.wg.Wait()
}

func (s *fakeSMTPServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}

	write("220 fake-smtp ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			// 只告知基本的 250，不宣告 STARTTLS/AUTH。
			write("250-fake-smtp")
			write("250 OK")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			write("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO"):
			addr := line
			if i := strings.Index(line, "<"); i >= 0 {
				addr = line[i+1:]
				if j := strings.Index(addr, ">"); j >= 0 {
					addr = addr[:j]
				}
			}
			s.mu.Lock()
			s.rcpt = append(s.rcpt, addr)
			reject := s.rejectRcpt != "" && addr == s.rejectRcpt
			s.mu.Unlock()
			if reject {
				write("550 No such user")
				continue
			}
			write("250 OK")
		case strings.HasPrefix(cmd, "DATA"):
			write("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				body.WriteString(dl)
			}
			s.mu.Lock()
			s.data = append(s.data, body.String())
			s.mu.Unlock()
			write("250 OK: queued")
		case strings.HasPrefix(cmd, "QUIT"):
			write("221 Bye")
			return
		case strings.HasPrefix(cmd, "RSET"), strings.HasPrefix(cmd, "NOOP"):
			write("250 OK")
		default:
			write("250 OK")
		}
	}
}

func (s *fakeSMTPServer) snapshot() (data []string, rcpt []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.data...), append([]string(nil), s.rcpt...)
}

// dialTo 返回一个忽略目标地址、统一连到 s 的 dialer。
func (s *fakeSMTPServer) dialTo() dialFunc {
	return func(network, addr string) (net.Conn, error) {
		return net.Dial("tcp", s.addr())
	}
}

// TestSend_TableDriven 覆盖中文主题 + 多收件人 + HTML 的组合场景。
//
// 每个用例断言：收件人数量、主题被 RFC 2047 编码且可正确解码、正文部分存在性。
func TestSend_TableDriven(t *testing.T) {
	cases := []struct {
		name        string
		msg         gnotify.Message
		wantRcpt    int
		wantSubject string
		wantHTML    bool
		wantBody    string
	}{
		{
			name: "chinese subject multi recipient plain",
			msg: gnotify.Message{
				To:      []string{"a@example.com", "b@example.com"},
				Subject: "你好，世界",
				Body:    "hello body",
			},
			wantRcpt:    2,
			wantSubject: "你好，世界",
			wantBody:    "hello body",
		},
		{
			name: "single recipient html",
			msg: gnotify.Message{
				To:      []string{"a@example.com"},
				Subject: "中文标题 HTML",
				Body:    "plain fallback",
				HTML:    "<p>你好</p>",
			},
			wantRcpt:    1,
			wantSubject: "中文标题 HTML",
			wantHTML:    true,
			wantBody:    "plain fallback",
		},
		{
			name: "multi recipient html",
			msg: gnotify.Message{
				To:      []string{"a@example.com", "b@example.com", "c@example.com"},
				Subject: "群发测试",
				Body:    "multi body",
				HTML:    "<h1>标题</h1>",
			},
			wantRcpt:    3,
			wantSubject: "群发测试",
			wantHTML:    true,
			wantBody:    "multi body",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeSMTPServer(t)
			n, err := NewWithDialer(EmailConfig{
				Host: "smtp.example.com",
				Port: 587,
				From: "from@example.com",
			}, srv.dialTo())
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if err := n.Send(context.Background(), tc.msg); err != nil {
				t.Fatalf("Send: %v", err)
			}

			data, rcpt := srv.snapshot()
			if len(data) != tc.wantRcpt {
				t.Fatalf("want %d messages (one per recipient), got %d", tc.wantRcpt, len(data))
			}
			if len(rcpt) != tc.wantRcpt {
				t.Fatalf("want %d rcpt, got %d: %v", tc.wantRcpt, len(rcpt), rcpt)
			}
			for _, d := range data {
				// 主题必须被 MIME 编码（RFC 2047：=?UTF-8?...?=），不能是原始 UTF-8。
				rawSubj := extractHeader(d, "Subject")
				if !strings.HasPrefix(strings.ToUpper(rawSubj), "=?UTF-8?") {
					t.Errorf("subject not RFC2047 encoded: %q", rawSubj)
				}
				if strings.Contains(rawSubj, tc.wantSubject) {
					t.Errorf("subject should be encoded, not raw utf-8: %q", rawSubj)
				}
				dec, err := new(mime.WordDecoder).DecodeHeader(rawSubj)
				if err != nil {
					t.Fatalf("decode subject: %v", err)
				}
				if dec != tc.wantSubject {
					t.Errorf("decoded subject = %q, want %q", dec, tc.wantSubject)
				}
				// 正文存在性（quoted-printable 会保持 ASCII 可读）。
				if !strings.Contains(d, tc.wantBody) {
					t.Errorf("missing plain body %q in message", tc.wantBody)
				}
				if tc.wantHTML {
					if !strings.Contains(d, "multipart/alternative") {
						t.Error("missing multipart/alternative")
					}
					if !strings.Contains(d, "text/html") {
						t.Error("missing text/html part")
					}
				} else if !strings.Contains(d, "text/plain") {
					t.Error("missing text/plain content-type")
				}
			}
		})
	}
}

func TestSend_NoRecipient(t *testing.T) {
	srv := newFakeSMTPServer(t)
	n, _ := NewWithDialer(EmailConfig{Host: "h", From: "f@x.com"}, srv.dialTo())
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
}

// TestSend_MultiRecipient_ConcurrentAllSuccess 验证多收件人并发下全部送到且返回 nil。
func TestSend_MultiRecipient_ConcurrentAllSuccess(t *testing.T) {
	srv := newFakeSMTPServer(t)
	n, err := NewWithDialer(EmailConfig{Host: "h", From: "f@x.com"}, srv.dialTo())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 收件人数量超过 DefaultMailConcurrency，确保并发限流路径被覆盖。
	to := []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com", "e@x.com", "f@x.com"}
	if err := n.Send(context.Background(), gnotify.Message{To: to, Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("Send: want nil, got %v", err)
	}

	data, rcpt := srv.snapshot()
	if len(data) != len(to) {
		t.Fatalf("want %d messages, got %d", len(to), len(data))
	}
	if len(rcpt) != len(to) {
		t.Fatalf("want %d rcpt, got %d: %v", len(to), len(rcpt), rcpt)
	}
}

// TestSend_MultiRecipient_PartialFailure 验证某收件人失败时其它收件人仍成功送达，
// 且返回错误可 errors.Is(ErrSendFailed)。
func TestSend_MultiRecipient_PartialFailure(t *testing.T) {
	srv := newFakeSMTPServer(t)
	srv.rejectRcpt = "bad@x.com"

	n, err := NewWithDialer(EmailConfig{Host: "h", From: "f@x.com"}, srv.dialTo())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = n.Send(context.Background(), gnotify.Message{
		To:   []string{"a@x.com", "bad@x.com", "c@x.com"},
		Body: "x",
	})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	// 两个好收件人的邮件应已成功投递；坏收件人不会进入 DATA。
	data, _ := srv.snapshot()
	if len(data) != 2 {
		t.Fatalf("want 2 successful deliveries, got %d", len(data))
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	if _, err := New(EmailConfig{From: "f@x.com"}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for empty host, got %v", err)
	}
	if _, err := New(EmailConfig{Host: "h"}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for empty from, got %v", err)
	}
}

func TestSend_FailureAggregation(t *testing.T) {
	// dialer 直接失败，验证错误被 ErrSendFailed 包装且逐收件人聚合。
	n, _ := NewWithDialer(EmailConfig{Host: "h", From: "f@x.com"},
		func(network, addr string) (net.Conn, error) {
			return nil, errors.New("boom")
		})
	err := n.Send(context.Background(), gnotify.Message{
		To:   []string{"a@x.com", "b@x.com"},
		Body: "x",
	})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	// errors.Join 聚合两条错误。
	if strings.Count(err.Error(), "boom") != 2 {
		t.Fatalf("want 2 aggregated errors, got: %v", err)
	}
}

func TestRegistry_Email(t *testing.T) {
	n, err := gnotify.New("email")
	if err != nil {
		t.Fatalf("gnotify.New(email): %v", err)
	}
	if n.Channel() != "email" {
		t.Fatalf("channel = %q", n.Channel())
	}
	// 未带配置时 Send 应返回 ErrInvalidConfig。
	err = n.Send(context.Background(), gnotify.Message{To: []string{"a@x.com"}, Body: "x"})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestAddr_Defaults(t *testing.T) {
	if got := (EmailConfig{Host: "h"}).addr(); got != "h:587" {
		t.Errorf("plain default addr = %q", got)
	}
	if got := (EmailConfig{Host: "h", TLS: true}).addr(); got != "h:465" {
		t.Errorf("tls default addr = %q", got)
	}
	if got := (EmailConfig{Host: "h", Port: 25}).addr(); got != "h:25" {
		t.Errorf("explicit port addr = %q", got)
	}
}

// extractHeader 从原始邮件中取某 header 的值（简单的单行读取，支持折行续接）。
func extractHeader(raw, key string) string {
	lines := strings.Split(raw, "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(key)+":") {
			val := strings.TrimSpace(line[len(key)+1:])
			// 处理 RFC 2047 折行续接（以空白开头的后续行）。
			j := i + 1
			for j < len(lines) && (strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t")) {
				val += strings.TrimSpace(lines[j])
				j++
			}
			return val
		}
	}
	return ""
}

// TestNew_DisabledSkipsSMTP 验证 email.New 也受 gnotify 全局 Disabled 开关控制：
// Disabled 时 Send 直接返回 nil，且不会真正拨号（dialer 不被调用）。
func TestNew_DisabledSkipsSMTP(t *testing.T) {
	dialed := false
	dialer := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("should not dial")
	}
	n, err := NewWithDialer(EmailConfig{Host: "h", From: "f@x.com"}, dialer, gnotify.WithDisabled(true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"a@x.com"}, Body: "x"}); err != nil {
		t.Fatalf("disabled send: %v", err)
	}
	if dialed {
		t.Fatal("Disabled 下不应拨号")
	}
}
