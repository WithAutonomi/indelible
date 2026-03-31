package evm

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Contract ABIs for Autonomi storage payments.
// Sourced from evmlib's IPaymentVaultV6.json.

var payForQuotesABI abi.ABI
var erc20ABI abi.ABI

func init() {
	var err error

	// payForQuotes(DataPayment[] _payments)
	// where DataPayment is (address rewardsAddress, uint256 amount, bytes32 quoteHash)
	payForQuotesABI, err = abi.JSON(strings.NewReader(`[{
		"name": "payForQuotes",
		"type": "function",
		"inputs": [{
			"name": "_payments",
			"type": "tuple[]",
			"components": [
				{"name": "rewardsAddress", "type": "address"},
				{"name": "amount", "type": "uint256"},
				{"name": "quoteHash", "type": "bytes32"}
			]
		}],
		"outputs": []
	}]`))
	if err != nil {
		panic("invalid payForQuotes ABI: " + err.Error())
	}

	// ERC-20 approve + allowance + balanceOf
	erc20ABI, err = abi.JSON(strings.NewReader(`[{
		"name": "approve",
		"type": "function",
		"inputs": [
			{"name": "spender", "type": "address"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": [{"name": "", "type": "bool"}]
	},{
		"name": "allowance",
		"type": "function",
		"inputs": [
			{"name": "owner", "type": "address"},
			{"name": "spender", "type": "address"}
		],
		"outputs": [{"name": "", "type": "uint256"}]
	},{
		"name": "balanceOf",
		"type": "function",
		"inputs": [
			{"name": "account", "type": "address"}
		],
		"outputs": [{"name": "", "type": "uint256"}]
	}]`))
	if err != nil {
		panic("invalid ERC-20 ABI: " + err.Error())
	}
}

// DataPayment matches the Solidity struct IPaymentVault.DataPayment.
type DataPayment struct {
	RewardsAddress common.Address
	Amount         *big.Int
	QuoteHash      [32]byte
}

// packPayForQuotes encodes the calldata for payForQuotes(DataPayment[]).
func packPayForQuotes(payments []DataPayment) ([]byte, error) {
	return payForQuotesABI.Pack("payForQuotes", payments)
}

// packApprove encodes the calldata for ERC-20 approve.
func packApprove(spender common.Address, amount *big.Int) ([]byte, error) {
	return erc20ABI.Pack("approve", spender, amount)
}

// packAllowance encodes the calldata for ERC-20 allowance.
func packAllowance(owner, spender common.Address) ([]byte, error) {
	return erc20ABI.Pack("allowance", owner, spender)
}

// packBalanceOf encodes the calldata for ERC-20 balanceOf.
func packBalanceOf(account common.Address) ([]byte, error) {
	return erc20ABI.Pack("balanceOf", account)
}

// toCallMsg builds an ethereum.CallMsg for eth_call or gas estimation.
func toCallMsg(from, to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{
		From: from,
		To:   &to,
		Data: data,
	}
}

// hexTo32Bytes parses a hex string (with optional 0x prefix) into a [32]byte.
func hexTo32Bytes(s string) ([32]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return [32]byte{}, fmt.Errorf("hex decode: %w", err)
	}
	if len(b) != 32 {
		return [32]byte{}, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out, nil
}
