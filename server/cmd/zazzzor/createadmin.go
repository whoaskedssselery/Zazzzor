package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// createAdminCommand reads one password line and provisions a configured administrator.
func createAdminCommand(args []string, env func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "использование: zazzzor create-admin <username>")
		return 2
	}
	cfg, err := config.Load(env)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stderr, "Введите пароль администратора:")
	input := bufio.NewScanner(stdin)
	input.Buffer(make([]byte, 1024), 1024)
	if !input.Scan() {
		_, _ = fmt.Fprintln(stderr, "create-admin: не удалось прочитать строку с паролем из stdin")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), startupDBTimeout+migrateTimeout)
	defer cancel()
	pool, err := openDatabase(ctx, slog.New(slog.NewJSONHandler(stderr, nil)), cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
		return 1
	}
	defer pool.Close()
	svc := auth.NewService(auth.Options{
		Pool: pool, IDs: snowflake.NewGenerator(0, time.Now), AdminUsernames: cfg.AdminUsernames,
	})
	user, err := svc.CreateAdmin(ctx, args[0], input.Text())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Администратор %s создан.\n", user.Username)
	return 0
}
