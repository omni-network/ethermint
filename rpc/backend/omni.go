package backend

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	evmtypes "github.com/evmos/ethermint/x/evm/types"
	rpctypes "github.com/evmos/ethermint/rpc/types"
)

func (b *Backend) GetEvmStoreRoot(blockNum rpctypes.BlockNumber) (*hexutil.Bytes, error) {
	height := blockNum.Int64()
	return QueryEvmStoreRoot(b.clientCtx, b.queryClient, height)
}

func QueryEvmStoreRoot(
	clientCtx client.Context,
	queryClient *rpctypes.QueryClient,
	height int64,
) (*hexutil.Bytes, error) {
	clientCtx = clientCtx.WithHeight(height)

	// hack to get root of evm store - get storage proof at address 0 slot 0
	// First proof will be a non-exist proof within the evm store. We want
	// the root of this proof.
	hexKey := common.HexToHash("0x0")
	address := common.HexToAddress("0x0")
	_, proof, err := queryClient.GetProof(
		clientCtx,
		evmtypes.StoreKey,
		evmtypes.StateKey(address, hexKey.Bytes()),
	)

	// The first proof op is an iavl proof of storage against some
	// evm store root. the second proof op is a simple merkle proof
	// of the evm store root inclusion within some multistore. We're
	// interested in the evm store root.
	root, err := GetProofOpRoot(&proof.Ops[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get evm store root: %w", err)
	}

	var ret hexutil.Bytes = root
	return &ret, nil
}
