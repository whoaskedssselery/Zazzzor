package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Smoke check: `zazzzor smoke <base_url>` walks a running API through the
// main path a client takes (register, create a server, send and read a
// message, open the gateway) and cleans up the server it created. It talks
// to the API over HTTP only, so it runs inside the distroless image
// (deploy/scripts/smoke-api.sh) and needs no database access. Messages are
// in Russian: they are read by whoever runs the server.

const (
	smokeRequestTimeout = 15 * time.Second
	smokeGatewayTimeout = 10 * time.Second
	smokeReadLimit      = 1 << 20
	smokeUsage          = "использование: zazzzor smoke <base_url>, например zazzzor smoke http://127.0.0.1:8080"
)

type smokeOptions struct {
	// GatewayTimeout bounds the wait for HELLO and READY; zero means
	// smokeGatewayTimeout.
	GatewayTimeout time.Duration
}

// smokeCommand parses `smoke <base_url>`. The Origin sent with writes and the
// WebSocket upgrade is the first of ZAZZZOR_ALLOWED_ORIGINS, so inside the
// api container it matches the server's own setting.
func smokeCommand(args []string, env func(string) string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, smokeUsage)
		return 2
	}
	base := strings.TrimRight(args[0], "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		_, _ = fmt.Fprintf(stderr, "адрес должен начинаться с http:// или https://\n%s\n", smokeUsage)
		return 2
	}
	origin, _, _ := strings.Cut(env("ZAZZZOR_ALLOWED_ORIGINS"), ",")
	origin = strings.TrimSpace(origin)
	if origin == "" {
		_, _ = fmt.Fprintln(stderr, "не задан ZAZZZOR_ALLOWED_ORIGINS: из него берётся заголовок Origin для проверки")
		return 2
	}
	return runSmoke(context.Background(), base, origin, smokeOptions{}, stdout, stderr)
}

// runSmoke runs every step against base and prints one line per step to
// stdout, then OK. The first failed step is printed to stderr and gives 1.
func runSmoke(ctx context.Context, base, origin string, opts smokeOptions, stdout, stderr io.Writer) int {
	if opts.GatewayTimeout <= 0 {
		opts.GatewayTimeout = smokeGatewayTimeout
	}
	s := &smokeSession{
		base:   base,
		origin: origin,
		http:   &http.Client{Timeout: smokeRequestTimeout},
		out:    stdout,
	}
	if err := s.run(ctx, opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "ОШИБКА: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "OK")
	return 0
}

type smokeSession struct {
	base, origin string
	http         *http.Client
	out          io.Writer
	session      string // "zz_session=<token>", never printed
}

// stepError names the step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return fmt.Sprintf("шаг «%s»: %v", e.step, e.err) }
func (e *stepError) Unwrap() error { return e.err }

func (s *smokeSession) step(name string, err error) error {
	if err != nil {
		return &stepError{step: name, err: err}
	}
	_, _ = fmt.Fprintf(s.out, "%s: ok\n", name)
	return nil
}

func (s *smokeSession) run(ctx context.Context, opts smokeOptions) (err error) {
	if err = s.step("регистрация", s.register(ctx)); err != nil {
		return err
	}
	defer func() { _ = s.do(ctx, http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil) }()

	var guild struct{ ID string }
	if err = s.step("создание сервера", s.do(ctx, http.MethodPost, "/api/v1/guilds",
		map[string]string{"name": "smoke"}, http.StatusCreated, &guild)); err != nil {
		return err
	}
	// The server is deleted whatever happens next.
	defer func() {
		derr := s.do(ctx, http.MethodDelete, "/api/v1/guilds/"+guild.ID, nil, http.StatusNoContent, nil)
		switch {
		case err == nil:
			err = s.step("удаление сервера", derr)
		case derr != nil:
			err = fmt.Errorf("%w (проверочный сервер %s тоже не удалось удалить: %v)", err, guild.ID, derr)
		}
	}()

	channelID, err := s.textChannel(ctx, guild.ID)
	if err = s.step("список каналов", err); err != nil {
		return err
	}
	var msg struct{ ID string }
	if err = s.step("отправка сообщения", s.do(ctx, http.MethodPost, "/api/v1/channels/"+channelID+"/messages",
		map[string]string{"content": "smoke"}, http.StatusCreated, &msg)); err != nil {
		return err
	}
	if err = s.step("чтение истории", s.findMessage(ctx, channelID, msg.ID)); err != nil {
		return err
	}
	return s.step("WebSocket", s.gateway(ctx, opts.GatewayTimeout))
}

func (s *smokeSession) register(ctx context.Context) error {
	user, pass := "smoke_"+randomHex(4), randomHex(16)
	resp, err := s.send(ctx, http.MethodPost, "/api/v1/auth/register",
		map[string]string{"username": user, "password": pass})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus(resp, http.StatusCreated, nil); err != nil {
		return err
	}
	for _, c := range resp.Cookies() {
		if c.Name == "zz_session" && c.Value != "" {
			s.session = c.Name + "=" + c.Value
			_, _ = fmt.Fprintf(s.out, "пользователь %s создан\n", user)
			return nil
		}
	}
	return errors.New("в ответе нет cookie zz_session")
}

func (s *smokeSession) textChannel(ctx context.Context, guildID string) (string, error) {
	var channels []struct {
		ID   string
		Type int
	}
	if err := s.do(ctx, http.MethodGet, "/api/v1/guilds/"+guildID+"/channels", nil, http.StatusOK, &channels); err != nil {
		return "", err
	}
	for _, c := range channels {
		if c.Type == 0 {
			return c.ID, nil
		}
	}
	return "", errors.New("у нового сервера нет текстового канала")
}

func (s *smokeSession) findMessage(ctx context.Context, channelID, messageID string) error {
	var page []struct{ ID string }
	if err := s.do(ctx, http.MethodGet, "/api/v1/channels/"+channelID+"/messages", nil, http.StatusOK, &page); err != nil {
		return err
	}
	for _, m := range page {
		if m.ID == messageID {
			return nil
		}
	}
	return errors.New("отправленного сообщения нет в истории")
}

// gateway opens /api/v1/gateway with the session cookie and expects HELLO
// (op 10) and then READY (op 0, t READY) within timeout.
func (s *smokeSession) gateway(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h := http.Header{}
	h.Set("Origin", s.origin)
	h.Set("Cookie", s.session)
	c, resp, err := websocket.Dial(ctx, s.base+"/api/v1/gateway", &websocket.DialOptions{
		HTTPHeader: h,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return fmt.Errorf("подключение к /api/v1/gateway: ответ %d вместо 101 Switching Protocols%s",
				resp.StatusCode, statusHint(resp.StatusCode, ""))
		}
		return fmt.Errorf("подключение к /api/v1/gateway: %w", err)
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(smokeReadLimit)

	type frame struct {
		Op int    `json:"op"`
		T  string `json:"t"`
	}
	read := func(what string) (frame, error) {
		var f frame
		_, data, rerr := c.Read(ctx)
		if rerr != nil {
			if ctx.Err() != nil {
				return f, fmt.Errorf("за %v не пришёл %s", timeout, what)
			}
			return f, fmt.Errorf("соединение закрылось до %s: %w", what, rerr)
		}
		if json.Unmarshal(data, &f) != nil {
			return f, fmt.Errorf("кадр вместо %s не JSON", what)
		}
		return f, nil
	}
	f, err := read("HELLO (op 10)")
	if err != nil {
		return err
	}
	if f.Op != 10 {
		return fmt.Errorf("первый кадр op %d, ожидался HELLO (op 10)", f.Op)
	}
	f, err = read("READY")
	if err != nil {
		return err
	}
	if f.Op != 0 || f.T != "READY" {
		return fmt.Errorf("после HELLO пришёл кадр op %d t %q, ожидался READY", f.Op, f.T)
	}
	if err := c.Close(websocket.StatusNormalClosure, "smoke done"); err != nil {
		return fmt.Errorf("закрытие соединения: %w", err)
	}
	return nil
}

// do sends a JSON request, expects status and decodes the body into out
// (when not nil).
func (s *smokeSession) do(ctx context.Context, method, path string, body any, status int, out any) error {
	resp, err := s.send(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus(resp, status, out)
}

func (s *smokeSession) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", s.origin)
	}
	if s.session != "" {
		req.Header.Set("Cookie", s.session)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API не отвечает по адресу %s: %w", s.base, err)
	}
	return resp, nil
}

func expectStatus(resp *http.Response, status int, out any) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, smokeReadLimit))
	if err != nil {
		return fmt.Errorf("чтение ответа: %w", err)
	}
	if resp.StatusCode != status {
		var p struct{ Code string }
		_ = json.Unmarshal(data, &p)
		code := ""
		if p.Code != "" {
			code = " " + p.Code
		}
		return fmt.Errorf("ответ %d%s вместо %d%s", resp.StatusCode, code, status, statusHint(resp.StatusCode, p.Code))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return errors.New("ответ не разбирается как JSON")
		}
	}
	return nil
}

// statusHint explains the failures a person running the check can fix.
func statusHint(status int, code string) string {
	switch {
	case code == "origin_not_allowed":
		return " (Origin проверки не входит в ZAZZZOR_ALLOWED_ORIGINS сервера)"
	case code == "registration_closed":
		return " (регистрация закрыта в настройках сервера; откройте её на время проверки)"
	case status == http.StatusTooManyRequests:
		return " (сработал лимит частоты: регистраций не больше 3 в час с одного адреса, новых серверов не больше 5 в час; подождите и повторите)"
	case status == http.StatusNotFound && code == "":
		return " (такого пути нет: шлюз WebSocket не установлен или запрос ушёл не в API)"
	case status == http.StatusBadGateway || status == http.StatusServiceUnavailable:
		return " (API не готов: смотрите docker compose ps и логи api)"
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
