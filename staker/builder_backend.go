// Copyright 2021-2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

package staker

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/offchainlabs/nitro/arbutil"
)

// ValidatorTxBuilder combines any transactions sent to it via SendTransaction into one batch,
// which is then sent to the validator wallet.
// This lets the validator make multiple atomic transactions.
// This inherits from an eth client so it can be used as an L1Interface,
// where it transparently intercepts calls to SendTransaction and queues them for the next batch.
type ValidatorTxBuilder struct {
	arbutil.L1Interface
	transactions []*types.Transaction
	builderAuth  *bind.TransactOpts
	isAuthFake   bool
	wallet       ValidatorWalletInterface
}

func NewValidatorTxBuilder(wallet ValidatorWalletInterface) (*ValidatorTxBuilder, error) {
	randKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	builderAuth := wallet.AuthIfEoa()
	var isAuthFake bool
	if builderAuth == nil {
		// Make a fake auth so we have txs to give to the smart contract wallet
		builderAuth, err = bind.NewKeyedTransactorWithChainID(randKey, big.NewInt(9999999))
		if err != nil {
			return nil, err
		}
		isAuthFake = true
	}
	return &ValidatorTxBuilder{
		builderAuth: builderAuth,
		wallet:      wallet,
		L1Interface: wallet.L1Client(),
		isAuthFake:  isAuthFake,
	}, nil
}

func (b *ValidatorTxBuilder) BuildingTransactionCount() int {
	return len(b.transactions)
}

func (b *ValidatorTxBuilder) ClearTransactions() {
	b.transactions = nil
}

func (b *ValidatorTxBuilder) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	if len(b.transactions) == 0 && !b.isAuthFake {
		return b.L1Interface.EstimateGas(ctx, call)
	}
	return 0, nil
}

func (b *ValidatorTxBuilder) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	b.transactions = append(b.transactions, tx)
	err := b.wallet.TestTransactions(ctx, b.transactions)
	if err != nil {
		// Remove the bad tx
		b.transactions = b.transactions[:len(b.transactions)-1]
		return err
	}
	return nil
}

// While this is not currently required, it's recommended not to reuse the returned auth for multiple transactions,
// as for an EOA this has the nonce in it. However, the EOA wwallet currently will only publish the first created tx,
// which is why that doesn't really matter.
func (b *ValidatorTxBuilder) AuthWithAmount(ctx context.Context, amount *big.Int) (*bind.TransactOpts, error) {
	nonce, err := b.NonceAt(ctx, b.builderAuth.From, nil)
	if err != nil {
		return nil, err
	}
	return &bind.TransactOpts{
		From:     b.builderAuth.From,
		Nonce:    new(big.Int).SetUint64(nonce),
		Signer:   b.builderAuth.Signer,
		Value:    amount,
		GasPrice: b.builderAuth.GasPrice,
		GasLimit: b.builderAuth.GasLimit,
		Context:  ctx,
	}, nil
}

// Auth is the same as AuthWithAmount with a 0 amount specified.
// See AuthWithAmount docs for important details.
func (b *ValidatorTxBuilder) Auth(ctx context.Context) (*bind.TransactOpts, error) {
	return b.AuthWithAmount(ctx, common.Big0)
}
