package services

import (
	"database/sql"
	"time"
)

// Transaction represents a cost transaction linked to a wallet and optionally an upload.
type Transaction struct {
	ID           int64
	WalletID     int64
	UploadID     sql.NullInt64
	TxType       string // "upload", "refund"
	Amount       string // atto tokens
	BalanceAfter string
	CreatedAt    time.Time
}

// TransactionService handles transaction logging.
type TransactionService struct {
	db *sql.DB
}

// NewTransactionService creates a new TransactionService.
func NewTransactionService(db *sql.DB) *TransactionService {
	return &TransactionService{db: db}
}

// Record logs a new transaction.
func (s *TransactionService) Record(walletID int64, uploadID *int64, txType, amount, balanceAfter string) (*Transaction, error) {
	var uID sql.NullInt64
	if uploadID != nil {
		uID = sql.NullInt64{Int64: *uploadID, Valid: true}
	}

	result, err := s.db.Exec(
		`INSERT INTO transactions (wallet_id, upload_id, tx_type, amount, balance_after) VALUES (?, ?, ?, ?, ?)`,
		walletID, uID, txType, amount, balanceAfter,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return s.GetByID(id)
}

// GetByID retrieves a transaction by ID.
func (s *TransactionService) GetByID(id int64) (*Transaction, error) {
	t := &Transaction{}
	err := s.db.QueryRow(
		`SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, created_at FROM transactions WHERE id = ?`, id,
	).Scan(&t.ID, &t.WalletID, &t.UploadID, &t.TxType, &t.Amount, &t.BalanceAfter, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListByWallet returns transactions for a wallet, newest first.
func (s *TransactionService) ListByWallet(walletID int64, limit, offset int) ([]*Transaction, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var total int64
	s.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE wallet_id = ?`, walletID).Scan(&total)

	rows, err := s.db.Query(
		`SELECT id, wallet_id, upload_id, tx_type, amount, balance_after, created_at
		 FROM transactions WHERE wallet_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		walletID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var txns []*Transaction
	for rows.Next() {
		t := &Transaction{}
		if err := rows.Scan(&t.ID, &t.WalletID, &t.UploadID, &t.TxType, &t.Amount, &t.BalanceAfter, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		txns = append(txns, t)
	}
	return txns, total, rows.Err()
}
