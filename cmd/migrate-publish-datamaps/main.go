// migrate-publish-datamaps is a one-shot operator tool for indelible#18.
//
// It walks completed uploads whose visibility is still "private" but whose
// underlying DataMap was never published to the network, republishes each
// DataMap as a single on-network chunk through antd, and flips the row to
// visibility='public' with the resulting datamap_address. After this runs,
// downstream consumers (pub-library, etc.) can fetch the upload directly via
// autonomi://<addr> instead of proxying through indelible.
//
// Runbook (PROD-01 wallet-env prereq):
//
//  1. antd today is launched wallet-less in indelible's systemd unit.
//     POST /v1/chunks requires AUTONOMI_WALLET_KEY on the daemon. Provision a
//     hot wallet (typically the same address indelible's wallets table uses)
//     and add it to the systemd environment, e.g.:
//
//       sudo mkdir -p /etc/indelible
//       sudo install -m 0600 /dev/null /etc/indelible/antd.env
//       echo "AUTONOMI_WALLET_KEY=<hex private key>" | sudo tee /etc/indelible/antd.env
//       sudo systemctl edit indelible.service
//         [Service]
//         EnvironmentFile=/etc/indelible/antd.env
//       sudo systemctl restart indelible.service
//
//  2. Verify with `--dry-run` first to confirm candidate count.
//  3. Run for real. Per-row JSONLines progress is written to stdout, summary
//     JSON to stderr on exit.
//  4. Revoke the wallet env when done — the daemon doesn't need it for steady-
//     state operation. Indelible's normal payment path is external-signer via
//     PrepareUpload + PayForQuotes + FinalizeUpload.
//
// Upstream tracking for removing step 1: V2-266.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/WithAutonomi/ant-sdk/antd-go"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/migrate"
	"github.com/WithAutonomi/indelible/internal/services"
)

// antdChunkAdapter wraps *sdk.Client to fit migrate.ChunkPublisher.
// antd-go's ChunkPut returns a *PutResult (with cost/address); the migrator
// only needs the address.
type antdChunkAdapter struct{ c *sdk.Client }

func (a antdChunkAdapter) ChunkPut(ctx context.Context, data []byte) (string, error) {
	r, err := a.c.ChunkPut(ctx, data)
	if err != nil {
		return "", err
	}
	return r.Address, nil
}

func (a antdChunkAdapter) ChunkGet(ctx context.Context, address string) ([]byte, error) {
	return a.c.ChunkGet(ctx, address)
}

func main() {
	var (
		configPath string
		dryRun     bool
		limit      int
		uuid       string
		verify     bool
	)
	flag.StringVar(&configPath, "config", "", "path to indelible.toml (same file the server uses)")
	flag.BoolVar(&dryRun, "dry-run", false, "list candidates and exit without publishing")
	flag.IntVar(&limit, "limit", 0, "process at most N candidates (0 = unlimited)")
	flag.StringVar(&uuid, "uuid", "", "single-row mode: only this upload UUID")
	flag.BoolVar(&verify, "verify", false, "round-trip ChunkGet after each publish and assert byte equality")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		fail("load config", err)
	}
	if err := cfg.ApplyNetworkPreset(); err != nil {
		fail("apply network preset", err)
	}

	db, err := database.Open(cfg.DBURL)
	if err != nil {
		fail("open db", err)
	}
	defer db.Close()

	// No migrate.Migrate call here — this binary attaches to an existing
	// indelible DB and must not mutate schema. The server owns migrations.

	antdURL := cfg.AntdURL
	if antdURL == "" {
		if url := sdk.DiscoverDaemonURL(); url != "" {
			antdURL = url
		}
	}
	if antdURL == "" {
		fail("antd_url", errors.New("no antd_url in config and discovery returned empty"))
	}
	client := sdk.NewClient(antdURL, sdk.WithTimeout(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		slog.Warn("interrupt received, finishing current row then exiting")
		cancel()
	}()

	// Health probe — informative only. The migrator surfaces the
	// AUTONOMI_WALLET_KEY-missing case on the first failed ChunkPut.
	if h, err := client.Health(ctx); err == nil {
		fmt.Fprintf(os.Stderr, "antd: %s (%s) commit=%s\n", h.Version, h.EvmNetwork, h.BuildCommit)
	} else {
		fmt.Fprintf(os.Stderr, "warn: antd /health failed: %v\n", err)
	}

	run, runErr := migrate.PublishDataMaps(
		ctx,
		services.NewUploadService(db),
		antdChunkAdapter{c: client},
		migrate.PublishDataMapsOptions{
			DryRun: dryRun,
			Limit:  limit,
			UUID:   uuid,
			Verify: verify,
		},
		os.Stdout,
	)

	// Summary always goes to stderr so stdout stays clean JSONLines.
	if run != nil {
		summary, _ := json.MarshalIndent(struct {
			Candidates int  `json:"candidates"`
			Published  int  `json:"published"`
			Verified   int  `json:"verified"`
			Failed     int  `json:"failed"`
			DryRun     bool `json:"dry_run"`
		}{run.Candidates, run.Published, run.Verified, run.Failed, run.DryRun}, "", "  ")
		fmt.Fprintln(os.Stderr, string(summary))
	}

	switch {
	case errors.Is(runErr, migrate.ErrDaemonWalletMissing):
		fmt.Fprintln(os.Stderr, "\naborted: antd has no daemon wallet. See the runbook at the top of cmd/migrate-publish-datamaps/main.go.")
		os.Exit(2)
	case runErr != nil && !errors.Is(runErr, context.Canceled):
		fail("run", runErr)
	case run != nil && run.Failed > 0:
		os.Exit(1)
	}
}

func fail(stage string, err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", stage, err)
	os.Exit(2)
}
