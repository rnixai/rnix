package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

var flagServePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start OpenAI-compatible HTTP gateway",
	Long:  "Start an OpenAI-compatible HTTP server that exposes registered LLM providers as standard API endpoints.",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&flagServePort, "port", 8080, "HTTP listen port")
}

func runServe(cmd *cobra.Command, args []string) error {
	if flagServePort < 1 || flagServePort > 65535 {
		return fmt.Errorf("invalid port %d: must be between 1 and 65535", flagServePort)
	}

	// 1. Load providers config
	providersCfg, err := llm.LoadOrDefaultProvidersConfig()
	if err != nil {
		return fmt.Errorf("loading providers config: %w", err)
	}

	// 2. Create registries and register providers
	driverReg := llm.NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()
	if err := llm.RegisterProviders(providersCfg, driverReg, devReg); err != nil {
		return fmt.Errorf("registering providers: %w", err)
	}

	// 3. Health checks
	llm.RunHealthChecks(providersCfg, driverReg, 3*time.Second)

	// 4. Start HTTP server
	addr := fmt.Sprintf("127.0.0.1:%d", flagServePort)
	openaiSrv := ipc.NewOpenAIServer(driverReg, addr)
	openaiSrv.SetDefaultProvider(providersCfg.ResolveDefaultProvider())

	fmt.Fprintf(os.Stderr, "Serving %d providers on http://%s\n", driverReg.Len(), addr)

	errCh := make(chan error, 1)
	go func() {
		if err := openaiSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// 5. Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-sigCh:
	}

	// 6. Graceful shutdown (5s timeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return openaiSrv.Shutdown(shutdownCtx)
}
