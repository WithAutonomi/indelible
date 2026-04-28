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

// GetBalances queries the ERC-20 token balance and native gas balance for an address.
// Returns (tokenBalance, gasBalance) as decimal strings in atto/wei.
func (s *Signer) GetBalances(ctx context.Context, walletAddress, tokenAddress string) (string, string, error) {
	addr := common.HexToAddress(walletAddress)
	tokenAddr := common.HexToAddress(tokenAddress)

	// Token balance (ERC-20 balanceOf)
	balData, err := packBalanceOf(addr)
	if err != nil {
		return "", "", fmt.Errorf("encoding balanceOf: %w", err)
	}
	result, err := s.client.CallContract(ctx, toCallMsg(addr, tokenAddr, balData), nil)
	if err != nil {
		return "", "", fmt.Errorf("querying token balance: %w", err)
	}
	tokenBalance := new(big.Int).SetBytes(result).String()

	// Gas balance (native ETH/ARB)
	gasBalance, err := s.client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return "", "", fmt.Errorf("querying gas balance: %w", err)
	}

	return tokenBalance, gasBalance.String(), nil
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
	hash, _, err := s.sendTxWithReceipt(ctx, privateKey, from, to, data)
	return hash, err
}

// PayForMerkleTree signs and submits the EVM merkle batch payment transaction.
// Returns the winner pool hash (hex with 0x) and total amount paid (decimal string).
//
// The flow:
// 1. Parse the private key
// 2. Convert SDK PoolCommitmentEntry data into ABI-compatible structs
// 3. Ensure token allowance for the merkle payments contract
// 4. Call payForMerkleTree on the merkle payment vault
// 5. Parse MerklePaymentMade event from receipt to extract winner pool hash
func (s *Signer) PayForMerkleTree(
	ctx context.Context,
	privateKeyHex string,
	depth int,
	poolCommitments []antd.PoolCommitmentEntry,
	merklePaymentTimestamp uint64,
	tokenAddress string,
	merklePaymentsAddress string,
) (winnerPoolHash string, totalAmount string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse private key
	privateKeyHex = strings.TrimPrefix(privateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return "", "", fmt.Errorf("invalid private key: %w", err)
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	tokenAddr := common.HexToAddress(tokenAddress)
	merkleAddr := common.HexToAddress(merklePaymentsAddress)

	// Convert SDK types to ABI structs
	commitments := make([]MerklePoolCommitment, len(poolCommitments))
	for i, pc := range poolCommitments {
		poolHash, err := hexTo32Bytes(pc.PoolHash)
		if err != nil {
			return "", "", fmt.Errorf("invalid pool_hash %s: %w", pc.PoolHash, err)
		}
		commitments[i].PoolHash = poolHash

		if len(pc.Candidates) != 16 {
			return "", "", fmt.Errorf("pool %d: expected 16 candidates, got %d", i, len(pc.Candidates))
		}
		for j, c := range pc.Candidates {
			amt, ok := new(big.Int).SetString(c.Amount, 10)
			if !ok {
				return "", "", fmt.Errorf("invalid candidate amount: %s", c.Amount)
			}
			commitments[i].Candidates[j] = MerkleCandidateNode{
				RewardsAddress: common.HexToAddress(c.RewardsAddress),
				Amount:         amt,
			}
		}
	}

	// Ensure token allowance for merkle payments contract
	// We don't know the exact cost upfront (contract determines it), so approve max
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if err := s.ensureAllowance(ctx, privateKey, fromAddress, tokenAddr, merkleAddr, maxUint256); err != nil {
		return "", "", fmt.Errorf("token approval: %w", err)
	}

	// ABI-encode payForMerkleTree(depth, commitments, timestamp)
	calldata, err := packPayForMerkleTree(uint8(depth), commitments, merklePaymentTimestamp)
	if err != nil {
		return "", "", fmt.Errorf("encoding payForMerkleTree: %w", err)
	}

	// Send transaction and wait for receipt
	txHashStr, receipt, err := s.sendTxWithReceipt(ctx, privateKey, fromAddress, merkleAddr, calldata)
	if err != nil {
		return "", "", fmt.Errorf("payForMerkleTree tx failed: %w", err)
	}
	_ = txHashStr

	// Parse MerklePaymentMade event from receipt logs
	// The winnerPoolHash is indexed (Topics[1])
	eventSig := merklePaymentMadeEvent.ID
	for _, log := range receipt.Logs {
		if len(log.Topics) >= 2 && log.Topics[0] == eventSig {
			winnerHash := log.Topics[1]
			// Parse non-indexed fields for totalAmount
			data, err := merklePaymentMadeEvent.Inputs.NonIndexed().Unpack(log.Data)
			if err != nil {
				return "", "", fmt.Errorf("parsing MerklePaymentMade event: %w", err)
			}
			// data[0] = depth (uint8), data[1] = totalAmount (uint256), data[2] = timestamp (uint64)
			paid, ok := data[1].(*big.Int)
			if !ok {
				return "", "", fmt.Errorf("unexpected totalAmount type in event")
			}
			return winnerHash.Hex(), paid.String(), nil
		}
	}

	return "", "", fmt.Errorf("MerklePaymentMade event not found in receipt")
}

// sendTxWithReceipt builds, signs, submits a transaction, and returns both the hash and receipt.
func (s *Signer) sendTxWithReceipt(
	ctx context.Context,
	privateKey *ecdsa.PrivateKey,
	from common.Address,
	to common.Address,
	data []byte,
) (string, *types.Receipt, error) {
	nonce, err := s.client.PendingNonceAt(ctx, from)
	if err != nil {
		return "", nil, fmt.Errorf("getting nonce: %w", err)
	}

	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("getting gas price: %w", err)
	}
	// Arbitrum One's base fee fluctuates between SuggestGasPrice and the
	// time the tx hits the mempool. Using the raw suggestion routinely loses
	// races: the node returns "max fee per gas less than block base fee"
	// when our tx arrives a few hundred ms later. For legacy txs the
	// gasPrice field is interpreted as both maxFeePerGas and the tip, so
	// doubling the cap costs nothing on average (the protocol still pays
	// min(cap, baseFee + tip)) but gives ample headroom against base-fee
	// drift. Reproduced 2026-04-28 against arbitrum-one mainnet:
	// maxFeePerGas 20168000 vs baseFee 20220000.
	gasPrice = new(big.Int).Mul(gasPrice, big.NewInt(2))

	msg := toCallMsg(from, to, data)
	gasLimit, err := s.client.EstimateGas(ctx, msg)
	if err != nil {
		return "", nil, fmt.Errorf("estimating gas: %w", err)
	}
	gasLimit = gasLimit * 120 / 100

	tx := types.NewTransaction(nonce, to, big.NewInt(0), gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(s.chainID), privateKey)
	if err != nil {
		return "", nil, fmt.Errorf("signing tx: %w", err)
	}

	if err := s.client.SendTransaction(ctx, signedTx); err != nil {
		return "", nil, fmt.Errorf("sending tx: %w", err)
	}

	receipt, err := waitForReceipt(ctx, s.client, signedTx.Hash())
	if err != nil {
		return "", nil, fmt.Errorf("waiting for receipt: %w", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return "", nil, fmt.Errorf("transaction reverted: %s", signedTx.Hash().Hex())
	}

	return signedTx.Hash().Hex(), receipt, nil
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
