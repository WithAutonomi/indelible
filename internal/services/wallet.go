package services

import (
	"database/sql"
	"errors"
	"time"

	"github.com/WithAutonomi/indelible/internal/crypto"
)

var (
	ErrWalletNotFound = errors.New("wallet not found")
	ErrNoDefaultWallet = errors.New("no default wallet configured")
)

// Wallet represents a wallet record.
type Wallet struct {
	ID             int64
	Name           string
	Address        string
	EncryptedKey   string
	IsDefault      bool
	PaymentBalance string
	GasBalance     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// WalletService handles wallet operations.
type WalletService struct {
	db            *sql.DB
	encryptionKey string // hex-encoded AES-256 key
}

// NewWalletService creates a new WalletService.
func NewWalletService(db *sql.DB, encryptionKey string) *WalletService {
	return &WalletService{db: db, encryptionKey: encryptionKey}
}

// Create adds a new wallet. If it's the first wallet, it becomes default.
func (s *WalletService) Create(name, address, privateKey string) (*Wallet, error) {
	// Encrypt the private key
	encryptedKey, err := crypto.Encrypt(s.encryptionKey, privateKey)
	if err != nil {
		return nil, err
	}

	// Check if there are any existing wallets
	var count int64
	s.db.QueryRow(`SELECT COUNT(*) FROM wallets`).Scan(&count)
	isDefault := count == 0

	result, err := s.db.Exec(
		`INSERT INTO wallets (name, address, encrypted_key, is_default) VALUES (?, ?, ?, ?)`,
		name, address, encryptedKey, isDefault,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

// GetByID retrieves a wallet by ID.
func (s *WalletService) GetByID(id int64) (*Wallet, error) {
	w := &Wallet{}
	err := s.db.QueryRow(
		`SELECT id, name, address, encrypted_key, is_default, payment_balance, gas_balance, created_at, updated_at
		 FROM wallets WHERE id = ?`, id,
	).Scan(&w.ID, &w.Name, &w.Address, &w.EncryptedKey, &w.IsDefault, &w.PaymentBalance, &w.GasBalance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return w, nil
}

// GetDefault returns the default wallet.
func (s *WalletService) GetDefault() (*Wallet, error) {
	w := &Wallet{}
	err := s.db.QueryRow(
		`SELECT id, name, address, encrypted_key, is_default, payment_balance, gas_balance, created_at, updated_at
		 FROM wallets WHERE is_default = 1`,
	).Scan(&w.ID, &w.Name, &w.Address, &w.EncryptedKey, &w.IsDefault, &w.PaymentBalance, &w.GasBalance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoDefaultWallet
		}
		return nil, err
	}
	return w, nil
}

// List returns all wallets.
func (s *WalletService) List() ([]*Wallet, error) {
	rows, err := s.db.Query(
		`SELECT id, name, address, encrypted_key, is_default, payment_balance, gas_balance, created_at, updated_at
		 FROM wallets ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wallets []*Wallet
	for rows.Next() {
		w := &Wallet{}
		if err := rows.Scan(&w.ID, &w.Name, &w.Address, &w.EncryptedKey, &w.IsDefault, &w.PaymentBalance, &w.GasBalance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}

// SetDefault makes the given wallet the default, unsetting all others.
func (s *WalletService) SetDefault(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Verify wallet exists
	var exists bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM wallets WHERE id = ?)`, id).Scan(&exists)
	if err != nil || !exists {
		return ErrWalletNotFound
	}

	// Clear all defaults
	if _, err := tx.Exec(`UPDATE wallets SET is_default = 0`); err != nil {
		return err
	}

	// Set new default
	if _, err := tx.Exec(`UPDATE wallets SET is_default = 1, updated_at = datetime('now') WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

// DecryptKey decrypts and returns the wallet's private key.
func (s *WalletService) DecryptKey(w *Wallet) (string, error) {
	return crypto.Decrypt(s.encryptionKey, w.EncryptedKey)
}

// UpdateBalance updates a wallet's payment and gas balances.
func (s *WalletService) UpdateBalance(id int64, paymentBalance, gasBalance string) error {
	_, err := s.db.Exec(
		`UPDATE wallets SET payment_balance = ?, gas_balance = ?, updated_at = datetime('now') WHERE id = ?`,
		paymentBalance, gasBalance, id,
	)
	return err
}
