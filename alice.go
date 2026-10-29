package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

func listenAlice(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/system/vendor/quasar/maind", "--cli")
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=/system/vendor/quasar/libs:"+os.Getenv("LD_LIBRARY_PATH"))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Alice CLI: %w", err)
	}
	var waitErr error
	done := make(chan struct{})
	go func() { waitErr = cmd.Wait(); close(done) }()
	defer func() { _ = cmd.Process.Kill(); <-done }()
	pause := func(duration time.Duration) error {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			if waitErr != nil {
				return waitErr
			}
			return errors.New("Alice CLI exited before the command completed")
		case <-timer.C:
			return nil
		}
	}
	if err := pause(2 * time.Second); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, "s\n"); err != nil {
		return err
	}
	if err := pause(4 * time.Second); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, "x\n"); err != nil {
		return err
	}
	_ = stdin.Close()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return waitErr
	}
}
