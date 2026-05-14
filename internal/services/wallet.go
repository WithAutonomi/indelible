package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/WithAutonomi/indelible/internal/crypto"
	"github.com/WithAutonomi/indelible/internal/database"
)

var (
	ErrWalletNotFound  = errors.New("wallet not found")
	ErrNoDefaultWallet = errors.New("no default wallet configured")
	ErrDeleteDefault   = errors.New("cannot delete the default wallet")
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
	db            *database.DB
	encryptionKey string // hex-encoded AES-256 key
}

// NewWalletService creates a new WalletService.
func NewWalletService(db *database.DB, encryptionKey string) *WalletService {
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
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM wallets`).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to check existing wallets: %w", err)
	}
	isDefault := count == 0

	var id int64
	err = s.db.QueryRow(
		`INSERT INTO wallets (name, address, encrypted_key, is_default) VALUES (?, ?, ?, ?) RETURNING id`,
		name, address, encryptedKey, isDefault,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
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
	if _, err := tx.Exec(`UPDATE wallets SET is_default = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
}

// Delete removes a wallet. Cannot delete the default wallet.
func (s *WalletService) Delete(id int64) error {
	var isDefault bool
	err := s.db.QueryRow(`SELECT is_default FROM wallets WHERE id = ?`, id).Scan(&isDefault)
	if err != nil {
		return ErrWalletNotFound
	}
	if isDefault {
		return ErrDeleteDefault
	}
	_, err = s.db.Exec(`DELETE FROM wallets WHERE id = ?`, id)
	return err
}

// DecryptKey decrypts and returns the wallet's private key.
func (s *WalletService) DecryptKey(w *Wallet) (string, error) {
	return crypto.Decrypt(s.encryptionKey, w.EncryptedKey)
}

// UpdateBalance updates a wallet's payment and gas balances.
func (s *WalletService) UpdateBalance(id int64, paymentBalance, gasBalance string) error {
	_, err := s.db.Exec(
		`UPDATE wallets SET payment_balance = ?, gas_balance = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		paymentBalance, gasBalance, id,
	)
	return err
}
