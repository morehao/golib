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
			s.mu.Unlock()
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

func TestSend_PlainText_ChineseSubject(t *testing.T) {
	srv := newFakeSMTPServer(t)

	n, err := New(EmailConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "from@example.com",
	}, WithDialer(srv.dialTo()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = n.Send(context.Background(), gnotify.Message{
		To:      []string{"a@example.com", "b@example.com"},
		Subject: "你好，世界",
		Body:    "hello body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	data, rcpt := srv.snapshot()
	if len(data) != 2 {
		t.Fatalf("want 2 messages (one per recipient), got %d", len(data))
	}
	if len(rcpt) != 2 {
		t.Fatalf("want 2 rcpt, got %d: %v", len(rcpt), rcpt)
	}
	for _, d := range data {
		// 中文标题应被 MIME Q-Encoding 编码（= 开头、含 =XX 进制或 =?utf-8?）。
		if !strings.Contains(d, "Subject: =?utf-8?") {
			t.Errorf("subject not encoded: %q", d)
		}
		if strings.Contains(d, "你好，世界") {
			t.Errorf("subject should be encoded, not raw utf-8")
		}
		// 解码验证内容正确。
		dec, err := new(mime.WordDecoder).DecodeHeader(extractHeader(d, "Subject"))
		if err != nil {
			t.Fatalf("decode subject: %v", err)
		}
		if dec != "你好，世界" {
			t.Errorf("decoded subject = %q", dec)
		}
		if !strings.Contains(d, "Content-Type: text/plain; charset=UTF-8") {
			t.Error("missing text/plain content-type")
		}
	}
}

func TestSend_HTML(t *testing.T) {
	srv := newFakeSMTPServer(t)
	n, err := New(EmailConfig{Host: "h", From: "f@x.com"}, WithDialer(srv.dialTo()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{
		To:      []string{"a@x.com"},
		Subject: "subj",
		Body:    "plain",
		HTML:    "<p>hi</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	data, _ := srv.snapshot()
	if len(data) != 1 {
		t.Fatalf("want 1 message, got %d", len(data))
	}
	if !strings.Contains(data[0], "multipart/alternative") {
		t.Error("missing multipart/alternative")
	}
	if !strings.Contains(data[0], "text/html") {
		t.Error("missing text/html part")
	}
}

func TestSend_NoRecipient(t *testing.T) {
	srv := newFakeSMTPServer(t)
	n, _ := New(EmailConfig{Host: "h", From: "f@x.com"}, WithDialer(srv.dialTo()))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
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
	n, _ := New(EmailConfig{Host: "h", From: "f@x.com"},
		WithDialer(func(network, addr string) (net.Conn, error) {
			return nil, errors.New("boom")
		}))
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

// extractHeader 从原始邮件中取某 header 的值（简单的单行读取）。
func extractHeader(raw, key string) string {
	for _, line := range strings.Split(raw, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(key)+":") {
			return strings.TrimSpace(line[len(key)+1:])
		}
	}
	return ""
}
