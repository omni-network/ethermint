// Copyright 2021 Evmos Foundation
// This file is part of Evmos' Ethermint library.
//
// The Ethermint library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Ethermint library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Ethermint library. If not, see https://github.com/evmos/ethermint/blob/main/LICENSE
package backend

import (
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	rpctypes "github.com/evmos/ethermint/rpc/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/pkg/errors"
	tmrpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

type traceCalls struct {
	From    string `json:"from"`
	Gas     string `json:"gas"`
	GasUsed string `json:"gasUsed"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	To      string `json:"to"`
	Type    string `json:"type"`
	Value   string `json:"value"`
}

type traceType struct {
	Calls   []traceCalls   `json:"calls"`
	From    common.Address `json:"from"`
	Gas     string         `json:"gas"`
	GasUsed string         `json:"gasUsed"`
	Input   string         `json:"input"`
	Output  string         `json:"output"`
	To      common.Address `json:"to"`
	Type    string         `json:"type"`
	Value   string         `json:"value"`
}

// TraceTransaction returns the structured logs created during the execution of EVM
// and returns them as a JSON object.
func (b *Backend) TraceTransaction(hash common.Hash, config *evmtypes.TraceConfig) (interface{}, error) {
	transaction, _, err := b.GetTxByEthHash(hash)
	if err != nil {
		b.logger.Debug("tx not found", "hash", hash)
		return nil, err
	}

	blockNum := rpctypes.BlockNumber(transaction.Height)
	if blockNum == 0 {
		return nil, errors.New("genesis is not traceable")
	}

	resBlock, err := b.TendermintBlockByNumber(blockNum)
	if err != nil {
		return nil, err
	}

	if resBlock == nil {
		// block not found
		return nil, fmt.Errorf("block not found for height %d", blockNum)
	}

	blockRes, err := b.TendermintBlockResultByNumber(&resBlock.Block.Height)
	if err != nil {
		return nil, fmt.Errorf("block result not found for height %d", resBlock.Block.Height)
	}

	msgs := b.EthMsgsFromTendermintBlock(resBlock, blockRes)
	endBlockMsgs := b.EthMsgsFromTendermintEndBlock(blockRes)
	msgs = append(msgs, endBlockMsgs...)

	var predecessors []*evmtypes.MsgEthereumTx
	var msg *evmtypes.MsgEthereumTx
	for _, m := range msgs {
		if m.Hash == hash.Hex() {
			msg = m
			break
		} else {
			predecessors = append(predecessors, m)
		}
	}

	if msg == nil {
		b.logger.Debug("tx not found", "hash", hash)
		return nil, fmt.Errorf("tx not found in block %d", blockNum)
	}

	traceTxRequest := evmtypes.QueryTraceTxRequest{
		Msg:             msg,
		Predecessors:    predecessors,
		BlockNumber:     resBlock.Block.Height,
		BlockTime:       resBlock.Block.Time,
		BlockHash:       common.Bytes2Hex(resBlock.BlockID.Hash),
		ProposerAddress: sdk.ConsAddress(resBlock.Block.ProposerAddress),
		ChainId:         b.chainID.Int64(),
	}

	if config != nil {
		traceTxRequest.TraceConfig = config
	}

	// minus one to get the context of block beginning
	contextHeight := transaction.Height - 1
	if contextHeight < 1 {
		// 0 is a special value in `ContextWithHeight`
		contextHeight = 1
	}

	traceResult, err := b.queryClient.TraceTx(rpctypes.ContextWithHeight(contextHeight), &traceTxRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to trace tx: %w", err)
	}

	// Response format is unknown due to custom tracer config param
	// More information can be found here https://geth.ethereum.org/docs/dapp/tracing-filtered
	var decodedResult interface{}
	err = json.Unmarshal(traceResult.Data, &decodedResult)
	if err != nil {
		return nil, err
	}

	return decodedResult, nil
}

// TraceBlock configures a new tracer according to the provided configuration, and
// executes all the transactions contained within. The return value will be one item
// per transaction, dependent on the requested tracer.
func (b *Backend) TraceBlock(height rpctypes.BlockNumber,
	config *evmtypes.TraceConfig,
	block *tmrpctypes.ResultBlock,
) ([]*evmtypes.TxTraceResult, error) {
	txs := block.Block.Txs
	txsLength := len(txs)

	if txsLength == 0 {
		// If there are no transactions return empty array
		return []*evmtypes.TxTraceResult{}, nil
	}
	blockRes, err := b.TendermintBlockResultByNumber(&block.Block.Height)
	if err != nil {
		b.logger.Debug("block result not found", "height", block.Block.Height, "error", err.Error())
		return nil, nil
	}
	txDecoder := b.clientCtx.TxConfig.TxDecoder()

	var txsMessages []*evmtypes.MsgEthereumTx
	for i, tx := range txs {
		if !rpctypes.TxSuccessOrExceedsBlockGasLimit(blockRes.TxsResults[i]) {
			b.logger.Debug("invalid tx result code", "cosmos-hash", hexutil.Encode(tx.Hash()))
			continue
		}
		decodedTx, err := txDecoder(tx)
		if err != nil {
			b.logger.Error("failed to decode transaction", "hash", txs[i].Hash(), "error", err.Error())
			continue
		}

		for _, msg := range decodedTx.GetMsgs() {
			ethMessage, ok := msg.(*evmtypes.MsgEthereumTx)
			if !ok {
				// Just considers Ethereum transactions
				continue
			}
			txsMessages = append(txsMessages, ethMessage)
		}
	}

	// minus one to get the context at the beginning of the block
	contextHeight := height - 1
	if contextHeight < 1 {
		// 0 is a special value for `ContextWithHeight`.
		contextHeight = 1
	}
	ctxWithHeight := rpctypes.ContextWithHeight(int64(contextHeight))

	traceBlockRequest := &evmtypes.QueryTraceBlockRequest{
		Txs:             txsMessages,
		TraceConfig:     config,
		BlockNumber:     block.Block.Height,
		BlockTime:       block.Block.Time,
		BlockHash:       common.Bytes2Hex(block.BlockID.Hash),
		ProposerAddress: sdk.ConsAddress(block.Block.ProposerAddress),
		ChainId:         b.chainID.Int64(),
	}

	res, err := b.queryClient.TraceBlock(ctxWithHeight, traceBlockRequest)
	if err != nil {
		return nil, err
	}

	decodedResults := make([]*evmtypes.TxTraceResult, txsLength)
	if err := json.Unmarshal(res.Data, &decodedResults); err != nil {
		return nil, err
	}

	return decodedResults, nil
}
