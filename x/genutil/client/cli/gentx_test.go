package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	rpcclientmock "github.com/tendermint/tendermint/rpc/client/mock"
	coretypes "github.com/tendermint/tendermint/rpc/core/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	testutilmod "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	"github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	stakingcli "github.com/cosmos/cosmos-sdk/x/staking/client/cli"
)

var _ client.TendermintRPC = (*mockTendermintRPC)(nil)

type mockTendermintRPC struct {
	rpcclientmock.Client

	responseQuery abci.ResponseQuery
}

func newMockTendermintRPC(respQuery abci.ResponseQuery) mockTendermintRPC {
	return mockTendermintRPC{responseQuery: respQuery}
}

func (_ mockTendermintRPC) BroadcastTxCommit(_ context.Context, _ tmtypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	return &coretypes.ResultBroadcastTxCommit{}, nil
}

func (m mockTendermintRPC) ABCIQueryWithOptions(
	_ context.Context,
	_ string, _ tmbytes.HexBytes,
	_ rpcclient.ABCIQueryOptions,
) (*coretypes.ResultABCIQuery, error) {
	return &coretypes.ResultABCIQuery{Response: m.responseQuery}, nil
}

type CLITestSuite struct {
	suite.Suite

	kr        keyring.Keyring
	encCfg    testutilmod.TestEncodingConfig
	baseCtx   client.Context
	clientCtx client.Context
}

func TestCLITestSuite(t *testing.T) {
	suite.Run(t, new(CLITestSuite))
}

func (s *CLITestSuite) SetupSuite() {
	s.encCfg = testutilmod.MakeTestEncodingConfig(genutil.AppModuleBasic{})
	s.kr = keyring.NewInMemory(s.encCfg.Codec)
	s.baseCtx = client.Context{}.
		WithKeyring(s.kr).
		WithTxConfig(s.encCfg.TxConfig).
		WithCodec(s.encCfg.Codec).
		WithClient(mockTendermintRPC{Client: rpcclientmock.Client{}}).
		WithAccountRetriever(client.MockAccountRetriever{}).
		WithOutput(io.Discard).
		WithChainID("test-chain")

	var outBuf bytes.Buffer
	ctxGen := func() client.Context {
		bz, _ := s.encCfg.Codec.Marshal(&sdk.TxResponse{})
		c := newMockTendermintRPC(abci.ResponseQuery{
			Value: bz,
		})
		return s.baseCtx.WithClient(c)
	}
	s.clientCtx = ctxGen().WithOutput(&outBuf)

}

func (s *CLITestSuite) TestGenTxCmd() {
	amount := sdk.NewCoin("stake", sdk.NewInt(12))

	tests := []struct {
		name         string
		args         []string
		expCmdOutput string
	}{
		{
			name: "invalid commission rate returns error",
			args: []string{
				fmt.Sprintf("--%s=%s", flags.FlagChainID, s.baseCtx.ChainID),
				fmt.Sprintf("--%s=1", stakingcli.FlagCommissionRate),
				"node0",
				amount.String(),
			},
			expCmdOutput: fmt.Sprintf("--%s=%s --%s=1 %s %s", flags.FlagChainID, s.baseCtx.ChainID, stakingcli.FlagCommissionRate, "node0", amount.String()),
		},
		{
			name: "valid gentx",
			args: []string{
				fmt.Sprintf("--%s=%s", flags.FlagChainID, s.baseCtx.ChainID),
				"node0",
				amount.String(),
			},
			expCmdOutput: fmt.Sprintf("--%s=%s %s %s", flags.FlagChainID, s.baseCtx.ChainID, "node0", amount.String()),
		},
		{
			name: "invalid pubkey",
			args: []string{
				fmt.Sprintf("--%s=%s", flags.FlagChainID, "test-chain-1"),
				fmt.Sprintf("--%s={\"key\":\"BOIkjkFruMpfOFC9oNPhiJGfmY2pHF/gwHdLDLnrnS0=\"}", stakingcli.FlagPubKey),
				"node0",
				amount.String(),
			},
			expCmdOutput: fmt.Sprintf("--%s=test-chain-1 --%s={\"key\":\"BOIkjkFruMpfOFC9oNPhiJGfmY2pHF/gwHdLDLnrnS0=\"} %s %s ", flags.FlagChainID, stakingcli.FlagPubKey, "node0", amount.String()),
		},
		{
			name: "valid pubkey flag",
			args: []string{
				fmt.Sprintf("--%s=%s", flags.FlagChainID, "test-chain-1"),
				fmt.Sprintf("--%s={\"@type\":\"/cosmos.crypto.ed25519.PubKey\",\"key\":\"BOIkjkFruMpfOFC9oNPhiJGfmY2pHF/gwHdLDLnrnS0=\"}", stakingcli.FlagPubKey),
				"node0",
				amount.String(),
			},
			expCmdOutput: fmt.Sprintf("--%s=test-chain-1 --%s={\"@type\":\"/cosmos.crypto.ed25519.PubKey\",\"key\":\"BOIkjkFruMpfOFC9oNPhiJGfmY2pHF/gwHdLDLnrnS0=\"} %s %s ", flags.FlagChainID, stakingcli.FlagPubKey, "node0", amount.String()),
		},
	}

	for _, tc := range tests {
		tc := tc

		dir := s.T().TempDir()
		genTxFile := filepath.Join(dir, "myTx")
		tc.args = append(tc.args, fmt.Sprintf("--%s=%s", flags.FlagOutputDocument, genTxFile))

		s.Run(tc.name, func() {

			clientCtx := s.clientCtx
			ctx := svrcmd.CreateExecuteContext(context.Background())

			cmd := cli.GenTxCmd(
				module.NewBasicManager(),
				clientCtx.TxConfig,
				banktypes.GenesisBalancesIterator{},
				clientCtx.HomeDir,
			)
			cmd.SetContext(ctx)
			cmd.SetArgs(tc.args)

			s.Require().NoError(client.SetCmdClientContextHandler(clientCtx, cmd))

			if len(tc.args) != 0 {
				s.Require().Contains(fmt.Sprint(cmd), tc.expCmdOutput)
			}
		})
	}
}
