package e2e

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/juanidrobo/polygon-edge/crypto"
	"github.com/juanidrobo/polygon-edge/e2e/framework"
	"github.com/juanidrobo/polygon-edge/helper/tests"
	"github.com/juanidrobo/polygon-edge/types"
	"github.com/stretchr/testify/assert"
)

func TestBroadcast(t *testing.T) {
	testCases := []struct {
		name     string
		numNodes int
		// Number of nodes that connects to left node
		numConnectedNodes int
	}{
		{
			name:              "tx should not reach to last node",
			numNodes:          10,
			numConnectedNodes: 5,
		},
		{
			name:              "tx should reach to last node",
			numNodes:          10,
			numConnectedNodes: 10,
		},
	}

	signer := &crypto.FrontierSigner{}
	senderKey, senderAddr := tests.GenerateKeyAndAddr(t)
	_, receiverAddr := tests.GenerateKeyAndAddr(t)

	conf := func(config *framework.TestServerConfig) {
		config.SetConsensus(framework.ConsensusDummy)
		config.Premine(senderAddr, framework.EthToWei(10))
		config.SetSeal(true)
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			srvs := framework.NewTestServers(t, tt.numNodes, conf)
			framework.MultiJoinSerial(t, srvs[0:tt.numConnectedNodes])

			// Check the connections
			for i, srv := range srvs {
				// Required number of connections
				numRequiredConnections := 0
				if i < tt.numConnectedNodes {
					if i == 0 || i == tt.numConnectedNodes-1 {
						numRequiredConnections = 1
					} else {
						numRequiredConnections = 2
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_, err := framework.WaitUntilPeerConnects(ctx, srv, numRequiredConnections)
				if err != nil {
					t.Fatal(err)
				}
			}

			// wait until gossip protocol build mesh network
			// (https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.0.md)
			time.Sleep(time.Second * 2)

			tx, err := signer.SignTx(&types.Transaction{
				Nonce:    0,
				From:     senderAddr,
				To:       &receiverAddr,
				Value:    framework.EthToWei(1),
				Gas:      1000000,
				GasPrice: big.NewInt(10000),
				Input:    []byte{},
			}, senderKey)
			if err != nil {
				t.Fatalf("failed to sign transaction, err=%+v", err)
			}

			_, err = srvs[0].JSONRPC().Eth().SendRawTransaction(tx.MarshalRLP())
			if err != nil {
				t.Fatalf("failed to send transaction, err=%+v", err)
			}

			for i, srv := range srvs {
				shouldHaveTxPool := false
				subTestName := fmt.Sprintf("node %d shouldn't have tx in txpool", i)
				if i < tt.numConnectedNodes {
					shouldHaveTxPool = true
					subTestName = fmt.Sprintf("node %d should have tx in txpool", i)
				}

				t.Run(subTestName, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					res, err := framework.WaitUntilTxPoolFilled(ctx, srv, 1)

					if shouldHaveTxPool {
						assert.NoError(t, err)
						assert.Equal(t, uint64(1), res.Length)
					} else {
						assert.ErrorIs(t, err, tests.ErrTimeout)
					}
				})
			}
		})
	}
}
