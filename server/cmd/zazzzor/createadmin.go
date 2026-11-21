package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/config"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// createAdminCommand reads one password line and provisions a configured administrator.
func createAdminCommand(args []string, env func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: zazzzor create-admin <username>")
		return 2
	}
	cfg, err := config.Load(env)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
		return 1
	}
	password, err := readAdminPassword(stdin, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
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
	user, err := svc.CreateAdmin(ctx, args[0], password)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create-admin: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Administrator %s created.\n", user.Username)
	return 0
}

// readAdminPassword accepts a line from a pipe or reads an interactive line without echo.
func readAdminPassword(stdin io.Reader, stderr io.Writer) (string, error) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		_, _ = fmt.Fprint(stderr, "Administrator password: ")
		password, err := term.ReadPassword(int(file.Fd()))
		_, _ = fmt.Fprintln(stderr)
		if err != nil {
			return "", fmt.Errorf("read the password from the terminal: %w", err)
		}
		return string(password), nil
	}
	input := bufio.NewScanner(stdin)
	input.Buffer(make([]byte, 1024), 1024)
	if input.Scan() {
		return input.Text(), nil
	}
	err := input.Err()
	if err == nil {
		err = io.EOF
	}
	return "", fmt.Errorf("read the password line from stdin: %w", err)
}
