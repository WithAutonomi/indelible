package evm

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	antd "github.com/WithAutonomi/ant-sdk/antd-go"
)

// Signer handles EVM transaction signing and submission for storage payments.
// The private key never leaves this process — only signed transactions are sent.
type Signer struct {
	rpcURL  string
	client  *ethclient.Client
	chainID *big.Int
	mu      sync.Mutex // serializes nonce management per wallet
}

// NewSigner connects to the EVM RPC endpoint and determines the chain ID.
func NewSigner(rpcURL string) (*Signer, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to EVM RPC %s: %w", rpcURL, err)
	}

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("getting chain ID: %w", err)
	}

	return &Signer{
		rpcURL:  rpcURL,
		client:  client,
		chainID: chainID,
	}, nil
}

// RPCUrl returns the configured RPC URL.
func (s *Signer) RPCUrl() string {
	return s.rpcURL
}

// Close disconnects from the RPC endpoint.
func (s *Signer) Close() {
	if s.client != nil {
		s.client.Close()
	}
}

// PayForQuotes signs and submits the EVM payment transaction for storage quotes.
// Returns a map of quote_hash → tx_hash that antd needs to finalize the upload.
//
// The flow:
// 1. Parse the private key
// 2. Ensure the payment token has sufficient allowance for the data payments contract
// 3. Call pay_for_quotes on the data payments contract with all quote payments
// 4. Return the transaction hash mapped to each quote hash
func (s *Signer) PayForQuotes(
	ctx context.Context,
	privateKeyHex string,
	payments []antd.PaymentInfo,
	tokenAddress string,
	dataPaymentsAddress string,
) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse private key
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	tokenAddr := common.HexToAddress(tokenAddress)
	paymentsAddr := common.HexToAddress(dataPaymentsAddress)

	// Calculate total amount for approval check
	totalAmount := new(big.Int)
	for _, p := range payments {
		amt, ok := new(big.Int).SetString(p.Amount, 10)
		if !ok {
			return nil, fmt.Errorf("invalid payment amount: %s", p.Amount)
		}
		totalAmount.Add(totalAmount, amt)
	}

	// Check and set token allowance if needed
	if err := s.ensureAllowance(ctx, privateKey, fromAddress, tokenAddr, paymentsAddr, totalAmount); err != nil {
		return nil, fmt.Errorf("token approval: %w", err)
	}

	// Build DataPayment structs for payForQuotes(DataPayment[])
	dataPayments := make([]DataPayment, len(payments))
	for i, p := range payments {
		hash, err := hexTo32Bytes(p.QuoteHash)
		if err != nil {
			return nil, fmt.Errorf("invalid quote hash %s: %w", p.QuoteHash, err)
		}
		amt, _ := new(big.Int).SetString(p.Amount, 10)
		dataPayments[i] = DataPayment{
			RewardsAddress: common.HexToAddress(p.RewardsAddress),
			Amount:         amt,
			QuoteHash:      hash,
		}
	}

	calldata, err := packPayForQuotes(dataPayments)
	if err != nil {
		return nil, fmt.Errorf("encoding pay_for_quotes: %w", err)
	}

	// Send transaction
	txHash, err := s.sendTx(ctx, privateKey, fromAddress, paymentsAddr, calldata)
	if err != nil {
		return nil, fmt.Errorf("pay_for_quotes tx failed: %w", err)
	}

	// Map every quote hash to this single tx hash
	result := make(map[string]string, len(payments))
	for _, p := range payments {
		result[p.QuoteHash] = txHash
	}

	return result, nil
}

// ensureAllowance checks the ERC-20 allowance and approves if insufficient.
func (s *Signer) ensureAllowance(
	ctx context.Context,
	privateKey *ecdsa.PrivateKey,
	owner common.Address,
	token common.Address,
	spender common.Address,
	required *big.Int,
) error {
	// Check current allowance
	allowanceData, err := packAllowance(owner, spender)
	if err != nil {
		return err
	}

	result, err := s.client.CallContract(ctx, toCallMsg(owner, token, allowanceData), nil)
	if err != nil {
		return fmt.Errorf("checking allowance: %w", err)
	}

	currentAllowance := new(big.Int).SetBytes(result)
	if currentAllowance.Cmp(required) >= 0 {
		return nil // sufficient allowance
	}

	// Approve max uint256
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	approveData, err := packApprove(spender, maxUint256)
	if err != nil {
		return err
	}

	_, err = s.sendTx(ctx, privateKey, owner, token, approveData)
	if err != nil {
		return fmt.Errorf("approve tx failed: %w", err)
	}

	return nil
}

// sendTx builds, signs, and submits a transaction, waiting for the receipt.
func (s *Signer) sendTx(
	ctx context.Context,
	privateKey *ecdsa.PrivateKey,
	from common.Address,
	to common.Address,
	data []byte,
) (string, error) {
	nonce, err := s.client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", fmt.Errorf("getting nonce: %w", err)
	}

	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("getting gas price: %w", err)
	}

	msg := toCallMsg(from, to, data)
	gasLimit, err := s.client.EstimateGas(ctx, msg)
	if err != nil {
		return "", fmt.Errorf("estimating gas: %w", err)
	}
	// 20% buffer on gas estimate
	gasLimit = gasLimit * 120 / 100

	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(s.chainID), privateKey)
	if err != nil {
		return "", fmt.Errorf("signing tx: %w", err)
	}

	if err := s.client.SendTransaction(ctx, signedTx); err != nil {
		return "", fmt.Errorf("sending tx: %w", err)
	}

	// Wait for receipt
	receipt, err := waitForReceipt(ctx, s.client, signedTx.Hash())
	if err != nil {
		return "", fmt.Errorf("waiting for receipt: %w", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return "", fmt.Errorf("transaction reverted: %s", signedTx.Hash().Hex())
	}

	return signedTx.Hash().Hex(), nil
}

// waitForReceipt polls for a transaction receipt with a 2-second interval.
func waitForReceipt(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// retry
		}
	}
}
