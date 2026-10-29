package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

// fakeAntdAllStored is an antd whose prepare reports every chunk already on
// the network (no payments), plus the eth_chainId the signer asks for. Its
// finalize enforces antd's rule: tx_hashes must be present, even when empty.
type fakeAntdAllStored struct {
	mu          sync.Mutex
	finalizeTxs []string // raw tx_hashes JSON of each finalize call ("" when absent)
}

func (f *fakeAntdAllStored) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/rpc":
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(body["id"]) + `,"result":"0x7a69"}`))
	case "/v1/upload/prepare":
		_, _ = w.Write([]byte(`{"upload_id":"u-all-stored","payment_type":"wave_batch","payments":[],
			"total_amount":"0","total_chunks":3,"already_stored_count":3}`))
	case "/v1/upload/finalize":
		raw := string(body["tx_hashes"])
		f.mu.Lock()
		f.finalizeTxs = append(f.finalizeTxs, raw)
		f.mu.Unlock()
		if raw == "" || raw == "null" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"tx_hashes required for wave-batch upload (this upload used wave_batch payment); pass an empty object when prepare reported no payments","code":"BAD_REQUEST"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data_map":"deadbeef","sha256":"","chunks_stored":0}`))
	default:
		http.NotFound(w, r)
	}
}

// TestFinalizeAllAlreadyStoredSendsEmptyTxHashes: when prepare finds every
// chunk already stored (a duplicate upload, or a Retry after antd finished a
// store the worker lost track of), nothing is paid and finalize must send
// "tx_hashes": {} — a nil map went out as null, antd answered 400, and the
// upload failed although its data was on the network.
func TestFinalizeAllAlreadyStoredSendsEmptyTxHashes(t *testing.T) {
	fake := &fakeAntdAllStored{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	db := dbtest.OpenDB(t)
	cfg := &config.Config{
		DataDir:             t.TempDir(),
		AntdURL:             srv.URL,
		EvmRPCURL:           srv.URL + "/rpc",
		EvmTokenAddress:     "0x5FbDB2315678afecb367f032d93F642f64180aa3",
		WalletEncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111",
	}
	w := NewUploadWorker(db, cfg, nil)
	if _, err := db.Exec(`INSERT INTO users (id, email) VALUES (8101, 'stored@example.com')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := w.walletSvc.Create("w", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		"0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), "dup.bin")
	if err := os.WriteFile(tmp, []byte("already on the network"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := w.uploadSvc.Create(8101, nil, "dup.bin", "dup.bin", 22, "application/octet-stream", "private", tmp, nil); err != nil {
		t.Fatalf("create upload: %v", err)
	}
	up, err := w.uploadSvc.DequeueNext()
	if err != nil || up == nil {
		t.Fatalf("dequeue: %v %v", up, err)
	}

	if err := w.processUpload(context.Background(), up); err != nil {
		t.Fatalf("processUpload: %v", err)
	}
	if len(fake.finalizeTxs) != 1 || fake.finalizeTxs[0] != "{}" {
		t.Fatalf("finalize tx_hashes = %q, want one call with {}", fake.finalizeTxs)
	}
	got, err := w.uploadSvc.GetByID(up.ID)
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if got.Status != "already_stored" {
		t.Fatalf("status = %q, want already_stored", got.Status)
	}
}
