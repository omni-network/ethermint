package omni

import (
	"context"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/evmos/ethermint/rpc/backend"
	rpctypes "github.com/evmos/ethermint/rpc/types"
)

type PublicAPI struct {
	ctx     context.Context
	logger  log.Logger
	backend backend.EVMBackend
}

func NewPublicAPI(logger log.Logger, backend backend.EVMBackend) *PublicAPI {
	api := &PublicAPI{
		ctx:     context.Background(),
		logger:  logger.With("api", "omni-rpc"),
		backend: backend,
	}

	return api
}

func (e *PublicAPI) GetEvmStoreRoot(blockNum rpctypes.BlockNumber) (*hexutil.Bytes, error) {
	e.logger.Debug("omni_getEvmStoreRoot")
	return e.backend.GetEvmStoreRoot(blockNum)
}
