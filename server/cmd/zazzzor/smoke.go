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
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Smoke check: `zazzzor smoke <base_url>` walks a running API through the
// main path a client takes (register, create a server, send and read a
// message, open the gateway) and cleans up the server it created. It talks
// to the API without database access and runs inside the distroless image
// (deploy/scripts/smoke-api.sh). --voice also connects to LiveKit. Messages are
// in Russian: they are read by whoever runs the server.

const (
	smokeRequestTimeout = 15 * time.Second
	smokeGatewayTimeout = 10 * time.Second
	smokeVoiceTimeout   = 15 * time.Second
	smokeReadLimit      = 1 << 20
	smokeUsage          = "использование: zazzzor smoke [--voice] <base_url>, например zazzzor smoke --voice https://zazzzor.example\n" +
		"--voice проверяет сигнализацию, токен и вебхуки LiveKit. Качество звука, RTP через NAT и демонстрацию экрана проверяйте отдельно."
)

type smokeOptions struct {
	// GatewayTimeout bounds the wait for HELLO and READY; zero means
	// smokeGatewayTimeout.
	GatewayTimeout time.Duration
	// Voice also checks LiveKit admission and departure through the gateway.
	Voice bool
	// VoiceTimeout bounds each connection or voice-state wait; zero takes the default.
	VoiceTimeout time.Duration
}

// smokeCommand parses `smoke [--voice] <base_url>`. The Origin sent with writes and the
// WebSocket upgrade is the first of ZAZZZOR_ALLOWED_ORIGINS, so inside the
// api container it matches the server's own setting.
func smokeCommand(args []string, env func(string) string, stdout, stderr io.Writer) int {
	opts := smokeOptions{}
	if len(args) > 0 && args[0] == "--voice" {
		opts.Voice = true
		args = args[1:]
	}
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
	return runSmoke(context.Background(), base, origin, opts, stdout, stderr)
}

// runSmoke runs every step against base and prints one line per step to
// stdout, then OK. The first failed step is printed to stderr and gives 1.
func runSmoke(ctx context.Context, base, origin string, opts smokeOptions, stdout, stderr io.Writer) int {
	if opts.GatewayTimeout <= 0 {
		opts.GatewayTimeout = smokeGatewayTimeout
	}
	if opts.VoiceTimeout <= 0 {
		opts.VoiceTimeout = smokeVoiceTimeout
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
	if err = s.step("регистрация", s.register(ctx, "smoke_"+randomHex(4), randomHex(16))); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), smokeRequestTimeout)
		defer cancel()
		_ = s.do(cleanup, http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent, nil)
	}()

	var guild struct{ ID string }
	if err = s.step("создание сервера", s.do(ctx, http.MethodPost, "/api/v1/guilds",
		map[string]string{"name": "smoke"}, http.StatusCreated, &guild)); err != nil {
		return err
	}
	// The server is deleted whatever happens next.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), smokeRequestTimeout)
		defer cancel()
		derr := s.do(cleanup, http.MethodDelete, "/api/v1/guilds/"+guild.ID, nil, http.StatusNoContent, nil)
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
	if err = s.step("чтение истории", s.findMessage(ctx, channelID, msg.ID, "")); err != nil {
		return err
	}
	if !opts.Voice {
		return s.step("WebSocket", s.gateway(ctx, opts.GatewayTimeout, nil))
	}
	// The voice step runs on the gateway connection once READY arrives, so
	// the WebSocket step is reported passed at that point, before it; a
	// failure is then named by the step it happened in, not by both.
	err = s.gateway(ctx, opts.GatewayTimeout, func(ctx context.Context, c *websocket.Conn, ready json.RawMessage) error {
		_ = s.step("WebSocket", nil)
		return s.step("голос", s.voice(ctx, c, ready, guild.ID, opts.VoiceTimeout))
	})
	if _, named := errors.AsType[*stepError](err); err == nil || named {
		return err
	}
	return s.step("WebSocket", err)
}

// register creates the account user and keeps its session.
func (s *smokeSession) register(ctx context.Context, user, pass string) error {
	if err := s.signIn(ctx, "/api/v1/auth/register", user, pass, http.StatusCreated, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(s.out, "пользователь %s создан\n", user)
	return nil
}

// signIn posts user and pass to path (register or login), expects status,
// decodes the body into out (when not nil) and keeps the session cookie.
func (s *smokeSession) signIn(ctx context.Context, path, user, pass string, status int, out any) error {
	resp, err := s.send(ctx, http.MethodPost, path, map[string]string{"username": user, "password": pass})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := expectStatus(resp, status, out); err != nil {
		return err
	}
	for _, c := range resp.Cookies() {
		if c.Name == "zz_session" && c.Value != "" {
			s.session = c.Name + "=" + c.Value
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

// findMessage confirms the message and, when specified, its bound attachment.
func (s *smokeSession) findMessage(ctx context.Context, channelID, messageID, attachmentURL string) error {
	var page []struct {
		ID          string
		Attachments []struct{ URL string }
	}
	if err := s.do(ctx, http.MethodGet, "/api/v1/channels/"+channelID+"/messages", nil, http.StatusOK, &page); err != nil {
		return err
	}
	for _, m := range page {
		if m.ID != messageID {
			continue
		}
		if attachmentURL == "" {
			return nil
		}
		for _, file := range m.Attachments {
			if file.URL == attachmentURL {
				return nil
			}
		}
		return errors.New("в сообщении нет ожидаемого вложения")
	}
	return errors.New("отправленного сообщения нет в истории")
}

// gateway opens /api/v1/gateway with the session cookie and expects HELLO
// (op 10) and then READY (op 0, t READY) within timeout. When follow is set,
// it runs on the ready connection, kept alive until the check finishes.
func (s *smokeSession) gateway(parent context.Context, timeout time.Duration, follow func(context.Context, *websocket.Conn, json.RawMessage) error) error {
	ctx := parent
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
		Op int             `json:"op"`
		T  string          `json:"t"`
		D  json.RawMessage `json:"d"`
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
	var hello struct {
		HeartbeatIntervalMS int64 `json:"heartbeat_interval_ms"`
	}
	_ = json.Unmarshal(f.D, &hello)
	f, err = read("READY")
	if err != nil {
		return err
	}
	if f.Op != 0 || f.T != "READY" {
		return fmt.Errorf("после HELLO пришёл кадр op %d t %q, ожидался READY", f.Op, f.T)
	}
	if follow != nil {
		if hello.HeartbeatIntervalMS <= 0 || hello.HeartbeatIntervalMS > int64(time.Hour/time.Millisecond) {
			return errors.New("HELLO не содержит допустимый интервал heartbeat")
		}
		alive, stop := context.WithCancel(parent)
		done := make(chan struct{})
		go func() {
			defer close(done)
			tick := time.NewTicker(time.Duration(hello.HeartbeatIntervalMS) * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-alive.Done():
					return
				case <-tick.C:
					if err := c.Write(alive, websocket.MessageText, []byte(`{"op":1,"d":null}`)); err != nil {
						_ = c.CloseNow()
						return
					}
				}
			}
		}()
		defer func() {
			stop()
			<-done
		}()
		if err := follow(parent, c, f.D); err != nil {
			return err
		}
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

// send sends body (when not nil) as JSON.
func (s *smokeSession) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	if body == nil {
		return s.sendRaw(ctx, method, path, "", nil)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return s.sendRaw(ctx, method, path, "application/json", bytes.NewReader(b))
}

// upload sends image as the multipart field "file" with PUT, the way the
// client uploads avatars and icons, expects 200 and decodes the body into
// out.
func (s *smokeSession) upload(ctx context.Context, path string, image []byte, out any) error {
	return s.uploadFile(ctx, http.MethodPut, path, "image.png", image, nil, http.StatusOK, out)
}

// uploadFile sends one file and optional text fields in a multipart request.
func (s *smokeSession) uploadFile(ctx context.Context, method, path, name string, data []byte, fields map[string]string, status int, out any) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := mw.WriteField(key, value); err != nil {
			return fmt.Errorf("сборка поля формы: %w", err)
		}
	}
	part, err := mw.CreateFormFile("file", name)
	if err == nil {
		_, err = part.Write(data)
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		return fmt.Errorf("сборка запроса: %w", err)
	}
	resp, err := s.sendRaw(ctx, method, path, mw.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus(resp, status, out)
}

// fetch GETs path, expects 200 and returns the body.
func (s *smokeSession) fetch(ctx context.Context, path string) ([]byte, error) {
	resp, err := s.sendRaw(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, expectStatus(resp, http.StatusOK, nil)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, smokeReadLimit))
	if err != nil {
		return nil, fmt.Errorf("чтение ответа: %w", err)
	}
	return data, nil
}

// sendRaw sends body with contentType (when not empty), the Origin on
// writes and the session cookie once there is one.
func (s *smokeSession) sendRaw(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
