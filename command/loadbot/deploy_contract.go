package loadbot

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/juanidrobo/polygon-edge/command/loadbot/generator"
	"github.com/juanidrobo/polygon-edge/helper/tests"
	txpoolOp "github.com/juanidrobo/polygon-edge/txpool/proto"
	"github.com/juanidrobo/polygon-edge/types"

	"github.com/umbracle/go-web3/jsonrpc"
)

func (l *Loadbot) deployContract(
	grpcClient txpoolOp.TxnPoolOperatorClient,
	jsonClient *jsonrpc.Client,
	receiptTimeout time.Duration) error {
	start := time.Now()

	_, ok := l.generator.(generator.ContractTxnGenerator)
	if !ok {
		return fmt.Errorf("invalid generator type, it needs to be a generator.ContractTxnGenerator interface")
	}

	// deploy SC
	txHash, err := l.executeTxn(grpcClient)
	if err != nil {
		//nolint:forcetypeassert
		l.generator.(generator.ContractTxnGenerator).MarkFailedContractTxn(&generator.FailedContractTxnInfo{
			TxHash: txHash.String(),
			Error: &generator.TxnError{
				Error:     err,
				ErrorType: generator.AddErrorType,
			},
		})
		atomic.AddUint64(&l.metrics.ContractMetrics.FailedContractTransactionsCount, 1)

		return fmt.Errorf("could not execute transaction, %w", err)
	}

	// set timeout
	ctx, cancel := context.WithTimeout(context.Background(), receiptTimeout)
	defer cancel()

	// and wait for receipt
	receipt, err := tests.WaitForReceipt(ctx, jsonClient.Eth(), txHash)

	if err != nil {
		//nolint:forcetypeassert
		l.generator.(generator.ContractTxnGenerator).MarkFailedContractTxn(&generator.FailedContractTxnInfo{
			TxHash: txHash.String(),
			Error: &generator.TxnError{
				Error:     err,
				ErrorType: generator.ReceiptErrorType,
			},
		})
		atomic.AddUint64(&l.metrics.ContractMetrics.FailedContractTransactionsCount, 1)

		return fmt.Errorf("could not get the receipt, %w", err)
	}

	end := time.Now()
	// initialize gas metrics map with block nuber as index
	l.metrics.ContractMetrics.ContractGasMetrics.Blocks[receipt.BlockNumber] = GasMetrics{}
	// fetch contract address
	l.metrics.ContractMetrics.ContractAddress = receipt.ContractAddress
	// set contract address in order to get new example txn and gas estimate
	//nolint:forcetypeassert
	l.generator.(generator.ContractTxnGenerator).SetContractAddress(types.StringToAddress(
		receipt.ContractAddress.String(),
	))

	// we're done with SC deployment
	// we defined SC address and
	// now get new gas estimates for CS token transfers
	if err := l.updateGasEstimate(jsonClient); err != nil {
		return fmt.Errorf("unable to get gas estimate, %w", err)
	}

	// record contract deployment metrics
	l.metrics.ContractMetrics.ContractDeploymentDuration.reportTurnAroundTime(
		txHash,
		&metadata{
			turnAroundTime: end.Sub(start),
			blockNumber:    receipt.BlockNumber,
		},
	)
	// calculate contract deployment metrics
	if err := l.calculateGasMetrics(jsonClient, l.metrics.ContractMetrics.ContractGasMetrics); err != nil {
		return fmt.Errorf("unable to calculate contract block gas metrics: %w", err)
	}

	l.metrics.ContractMetrics.ContractDeploymentDuration.calcTurnAroundMetrics()
	l.metrics.ContractMetrics.ContractDeploymentDuration.TotalExecTime = end.Sub(start)

	return nil
}
