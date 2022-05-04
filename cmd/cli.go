package cmd

import (
	"context"
	"io"
	"log"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

// cli extends cobra.Command with our own config.
type cli struct {
	cmd *cobra.Command
	log *zap.SugaredLogger

	table   string
	version string
}

// Execute runs a standard CLI and can be called externally.
func Execute(version string, args []string, outW, errW io.Writer) int {
	zap, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to init zap logger: %v", err)
	}
	defer zap.Sync() //nolint:errcheck

	cli := &cli{}
	cli.log = zap.Sugar()
	cli.cmd = cli.newRootCmd()
	cli.version = version

	cli.cmd.SetArgs(args)
	cli.cmd.SetOut(outW)
	cli.cmd.SetErr(errW)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)
	defer cancel()

	err = cli.cmd.ExecuteContext(ctx)
	if err != nil {
		return exitFailure
	}

	return exitSuccess
}
