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
package evm

import (
	"bytes"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	abci "github.com/tendermint/tendermint/abci/types"

	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/keeper"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/evmos/ethermint/x/evm/types"
)

// InitGenesis initializes genesis state based on exported genesis
func InitGenesis(
	ctx sdk.Context,
	k *keeper.Keeper,
	accountKeeper types.AccountKeeper,
	data types.GenesisState,
) []abci.ValidatorUpdate {
	k.WithChainID(ctx)

	k.Logger(ctx).Info("Intializing EVM state from genesis file")

	err := k.SetParams(ctx, data.Params)
	if err != nil {
		panic(fmt.Errorf("error setting params %s", err))
	}

	// ensure evm module account is set
	if addr := accountKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic("the EVM module account has not been set")
	}

	predeploys := map[string]bool{
		"0x1212400000000000000000000000000000000001": true,
		"0x1212400000000000000000000000000000000002": true,
		"0x1212400000000000000000000000000000000003": true,
		"0x1212500000000000000000000000000000000001": true,
		"0x1820a4B7618BdE71Dce8cdc73aAB6C95905faD24": true,
		"0x4e59b44847b379578588920cA78FbF26c0B4956C": true,
	}

	for _, account := range data.Accounts {
		address := common.HexToAddress(account.Address)
		accAddress := sdk.AccAddress(address.Bytes())

		// this does not like the 0x prefix and hides errors
		// code := common.Hex2Bytes(account.Code)
		code, err := hexutil.Decode(account.Code)
		if err != nil {
			panic(fmt.Errorf("error decoding code %s", err))
		}

		codeHash := crypto.Keccak256Hash(code)

		// check if predploy
		// TODO: put this in genesis params. could look like this
		// if (!k.IsPredploy(ctx, accAddress)) {
		// 	....
		//}
		// for now, hard code omni predeploys
		// unclear why their setup doesn't really allow for predploys ( by
		// enforcing that the count exists, and matches an ethereum account)
		_, isPredeploy := predeploys[account.Address]
		if !isPredeploy {
			// check that the EVM balance the matches the account balance
			acc := accountKeeper.GetAccount(ctx, accAddress)
			if acc == nil {
				panic(fmt.Errorf("account not found for address %s", account.Address))
			}

			ethAcct, ok := acc.(ethermint.EthAccountI)
			if !ok {
				panic(
					fmt.Errorf("account %s must be an EthAccount interface, got %T",
						account.Address, acc,
					),
				)
			}

			// we ignore the empty Code hash checking, see ethermint PR#1234
			if len(account.Code) != 0 && !bytes.Equal(ethAcct.GetCodeHash().Bytes(), codeHash.Bytes()) {
				s := "the evm state code doesn't match with the codehash\n"
				panic(fmt.Sprintf("%s account: %s , evm state codehash: %v, ethAccount codehash: %v, evm state code: %s\n",
					s, account.Address, codeHash, ethAcct.GetCodeHash(), account.Code))
			}
		} else {
			k.SetAccount(ctx, address, statedb.Account{
				Nonce:    0,
				Balance:  new(big.Int).SetUint64(0),
				CodeHash: codeHash.Bytes(),
			})
		}

		k.SetCode(ctx, codeHash.Bytes(), code)

		for _, storage := range account.Storage {
			k.SetState(ctx, address, common.HexToHash(storage.Key), common.HexToHash(storage.Value).Bytes())
		}
	}

	return []abci.ValidatorUpdate{}
}

// ExportGenesis exports genesis state of the EVM module
func ExportGenesis(ctx sdk.Context, k *keeper.Keeper, ak types.AccountKeeper) *types.GenesisState {
	var ethGenAccounts []types.GenesisAccount
	ak.IterateAccounts(ctx, func(account authtypes.AccountI) bool {
		ethAccount, ok := account.(ethermint.EthAccountI)
		if !ok {
			// ignore non EthAccounts
			return false
		}

		addr := ethAccount.EthAddress()

		storage := k.GetAccountStorage(ctx, addr)

		genAccount := types.GenesisAccount{
			Address: addr.String(),
			Code:    common.Bytes2Hex(k.GetCode(ctx, ethAccount.GetCodeHash())),
			Storage: storage,
		}

		ethGenAccounts = append(ethGenAccounts, genAccount)
		return false
	})

	return &types.GenesisState{
		Accounts: ethGenAccounts,
		Params:   k.GetParams(ctx),
	}
}
