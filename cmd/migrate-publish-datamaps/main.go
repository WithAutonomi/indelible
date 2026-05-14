// migrate-publish-datamaps is an operator tool for back-publishing DataMaps
// of pre-existing private uploads that were created before public visibility
// was supported.
//
// For each completed upload with visibility='private' and no datamap_address,
// it takes the locally-stored serialized DataMap, publishes it as a single
// chunk on the network via antd's /v1/chunks/prepare + /v1/chunks/finalize
// (external-signer flow), and updates the row to visibility='public' with the
// resulting datamap_address. Payment flows through indelible's own configured
// wallet — no daemon-wallet provisioning required.
//
// Requires antd >= 0.7.0 (for the chunk prepare/finalize endpoints).
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
	"github.com/WithAutonomi/indelible/internal/evm"
	"github.com/WithAutonomi/indelible/internal/migrate"
	"github.com/WithAutonomi/indelible/internal/services"
)

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

	// Resolve indelible's own wallet — same path the upload worker uses.
	walletSvc := services.NewWalletService(db, cfg.WalletEncryptionKey)
	uploadSvc := services.NewUploadService(db)

	var walletKey string
	if !dryRun {
		wallet, err := walletSvc.GetDefault()
		if err != nil {
			fail("get default wallet", err)
		}
		walletKey, err = walletSvc.DecryptKey(wallet)
		if err != nil {
			fail("decrypt wallet key", err)
		}
	}

	// Resolve EVM RPC. Config wins (matches the upload worker's resolve order).
	rpcURL := cfg.EvmRPCURL
	if rpcURL == "" {
		// Fall back to antd /health to pick up the daemon's default. /health
		// doesn't expose the RPC URL directly, but the network preset already
		// produced one above via ApplyNetworkPreset.
		fail("evm_rpc_url", errors.New("EvmRPCURL not set — set INDELIBLE_NETWORK or evm_rpc_url"))
	}

	var signer *evm.Signer
	if !dryRun {
		signer, err = evm.NewSigner(rpcURL)
		if err != nil {
			fail("evm signer", err)
		}
		defer signer.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		slog.Warn("interrupt received, finishing current row then exiting")
		cancel()
	}()

	// Health probe — informative only.
	if h, err := client.Health(ctx); err == nil {
		fmt.Fprintf(os.Stderr, "antd: %s (%s) commit=%s\n", h.Version, h.EvmNetwork, h.BuildCommit)
	} else {
		fmt.Fprintf(os.Stderr, "warn: antd /health failed: %v\n", err)
	}

	var payer migrate.EvmPayer
	if signer != nil {
		payer = signer
	}

	run, runErr := migrate.PublishDataMaps(ctx, uploadSvc, client, payer, walletKey,
		migrate.Options{DryRun: dryRun, Limit: limit, UUID: uuid, Verify: verify}, os.Stdout)

	// Summary always to stderr so stdout stays clean JSONLines.
	if run != nil {
		summary, _ := json.MarshalIndent(struct {
			Candidates    int  `json:"candidates"`
			Published     int  `json:"published"`
			Verified      int  `json:"verified"`
			AlreadyStored int  `json:"already_stored"`
			Failed        int  `json:"failed"`
			DryRun        bool `json:"dry_run"`
		}{run.Candidates, run.Published, run.Verified, run.AlreadyStored, run.Failed, run.DryRun}, "", "  ")
		fmt.Fprintln(os.Stderr, string(summary))
	}

	switch {
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
