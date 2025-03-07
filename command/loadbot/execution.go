package loadbot

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juanidrobo/polygon-edge/command/loadbot/generator"
	"github.com/juanidrobo/polygon-edge/helper/tests"
	txpoolOp "github.com/juanidrobo/polygon-edge/txpool/proto"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/umbracle/go-web3/jsonrpc"

	"github.com/juanidrobo/polygon-edge/types"
	"github.com/umbracle/go-web3"
)

const (
	maxReceiptWait = 5 * time.Minute
	minReceiptWait = 1 * time.Minute

	defaultFastestTurnAround = time.Hour * 24
	defaultSlowestTurnAround = time.Duration(0)

	defaultGasLimit = 5242880 // 0x500000
)

type Mode string

const (
	transfer Mode = "transfer"
	deploy   Mode = "deploy"
	erc20    Mode = "erc20"
	erc721   Mode = "erc721"
)

type Account struct {
	Address    types.Address
	PrivateKey *ecdsa.PrivateKey
}

type Configuration struct {
	TPS              uint64
	Sender           types.Address
	Receiver         types.Address
	Value            *big.Int
	Count            uint64
	JSONRPC          string
	GRPC             string
	MaxConns         int
	GeneratorMode    Mode
	ChainID          uint64
	GasPrice         *big.Int
	GasLimit         *big.Int
	ContractArtifact *generator.ContractArtifact
	ConstructorArgs  []byte // smart contract constructor args
	MaxWait          uint64 // max wait time for receipts in minutes
}

type metadata struct {
	// turn around time for the transaction
	turnAroundTime time.Duration

	// block where it was sealed
	blockNumber uint64
}

type GasMetrics struct {
	GasUsed     uint64
	GasLimit    uint64
	Utilization float64
}

type BlockGasMetrics struct {
	Blocks        map[uint64]GasMetrics
	BlockGasMutex *sync.Mutex
}

type ContractMetricsData struct {
	FailedContractTransactionsCount uint64
	ContractDeploymentDuration      ExecDuration
	ContractAddress                 web3.Address
	ContractGasMetrics              *BlockGasMetrics
}

type Metrics struct {
	TotalTransactionsSentCount uint64
	FailedTransactionsCount    uint64
	TransactionDuration        ExecDuration
	ContractMetrics            ContractMetricsData
	GasMetrics                 *BlockGasMetrics
}

type Loadbot struct {
	cfg       *Configuration
	metrics   *Metrics
	generator generator.TransactionGenerator
}

func NewLoadbot(cfg *Configuration) *Loadbot {
	return &Loadbot{
		cfg: cfg,
		metrics: &Metrics{
			TotalTransactionsSentCount: 0,
			FailedTransactionsCount:    0,
			TransactionDuration: ExecDuration{
				blockTransactions: make(map[uint64]uint64),
			},
			ContractMetrics: ContractMetricsData{
				ContractDeploymentDuration: ExecDuration{
					blockTransactions: make(map[uint64]uint64),
				},
				ContractGasMetrics: &BlockGasMetrics{
					Blocks:        make(map[uint64]GasMetrics),
					BlockGasMutex: &sync.Mutex{},
				},
			},
			GasMetrics: &BlockGasMetrics{
				Blocks:        make(map[uint64]GasMetrics),
				BlockGasMutex: &sync.Mutex{},
			},
		},
	}
}

func (l *Loadbot) GetMetrics() *Metrics {
	return l.metrics
}

func (l *Loadbot) GetGenerator() generator.TransactionGenerator {
	return l.generator
}

func (l *Loadbot) Run() error {
	sender, err := extractSenderAccount(l.cfg.Sender)
	if err != nil {
		return fmt.Errorf("failed to extract sender account: %w", err)
	}

	jsonClient, err := createJSONRPCClient(l.cfg.JSONRPC, l.cfg.MaxConns)
	if err != nil {
		return fmt.Errorf("an error has occurred while creating JSON-RPC client: %w", err)
	}

	grpcClient, err := createGRPCClient(l.cfg.GRPC)
	if err != nil {
		return fmt.Errorf("an error has occurred while creating JSON-RPC client: %w", err)
	}

	defer func(client *jsonrpc.Client) {
		_ = client.Close()
	}(jsonClient)

	nonce, err := getInitialSenderNonce(jsonClient, sender.Address)
	if err != nil {
		return fmt.Errorf("unable to get initial sender nonce: %w", err)
	}

	gasPrice := l.cfg.GasPrice
	if gasPrice == nil {
		// No gas price specified, query the network for an estimation
		avgGasPrice, err := getAverageGasPrice(jsonClient)
		if err != nil {
			return fmt.Errorf("unable to get average gas price: %w", err)
		}

		gasPrice = new(big.Int).SetUint64(avgGasPrice)
	}

	// Set up the transaction generator
	generatorParams := &generator.GeneratorParams{
		Nonce:            nonce,
		ChainID:          l.cfg.ChainID,
		SenderAddress:    sender.Address,
		RecieverAddress:  l.cfg.Receiver,
		SenderKey:        sender.PrivateKey,
		GasPrice:         gasPrice,
		Value:            l.cfg.Value,
		ContractArtifact: l.cfg.ContractArtifact,
		ConstructorArgs:  l.cfg.ConstructorArgs,
	}

	var (
		txnGenerator      generator.TransactionGenerator
		tokenTxnGenerator generator.ContractTxnGenerator
		genErr            error
	)

	switch l.cfg.GeneratorMode {
	case transfer:
		txnGenerator, genErr = generator.NewTransferGenerator(generatorParams)
	case deploy:
		txnGenerator, genErr = generator.NewDeployGenerator(generatorParams)
	case erc20:
		tokenTxnGenerator, genErr = generator.NewERC20Generator(generatorParams)
	case erc721:
		tokenTxnGenerator, genErr = generator.NewERC721Generator(generatorParams)
	}

	if genErr != nil {
		return fmt.Errorf("unable to start generator, %w", genErr)
	}

	switch l.cfg.GeneratorMode {
	case erc20, erc721:
		l.generator = tokenTxnGenerator
	default:
		l.generator = txnGenerator
	}

	if err := l.updateGasEstimate(jsonClient); err != nil {
		return fmt.Errorf("could not update gas estimate, %w", err)
	}

	ticker := time.NewTicker(1 * time.Second / time.Duration(l.cfg.TPS))
	defer ticker.Stop()

	var receiptTimeout time.Duration
	// if max-wait flag is not set it will be calculated dynamically
	if l.cfg.MaxWait == 0 {
		receiptTimeout = calcMaxTimeout(l.cfg.Count, l.cfg.TPS)
	} else {
		receiptTimeout = time.Duration(l.cfg.MaxWait) * time.Minute
	}

	startTime := time.Now()

	if l.isTokenTransferMode() {
		if err := l.deployContract(grpcClient, jsonClient, receiptTimeout); err != nil {
			return fmt.Errorf("unable to deploy smart contract, %w", err)
		}
	}

	var wg sync.WaitGroup

	for i := uint64(0); i < l.cfg.Count; i++ {
		<-ticker.C

		l.metrics.TotalTransactionsSentCount += 1

		wg.Add(1)

		go func(index uint64) {
			defer wg.Done()

			// Start the performance timer
			start := time.Now()

			// Execute the transaction
			txHash, err := l.executeTxn(grpcClient)
			if err != nil {
				l.generator.MarkFailedTxn(&generator.FailedTxnInfo{
					Index:  index,
					TxHash: txHash.String(),
					Error: &generator.TxnError{
						Error:     err,
						ErrorType: generator.AddErrorType,
					},
				})
				atomic.AddUint64(&l.metrics.FailedTransactionsCount, 1)

				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), receiptTimeout)
			defer cancel()

			receipt, err := tests.WaitForReceipt(ctx, jsonClient.Eth(), txHash)
			if err != nil {
				l.generator.MarkFailedTxn(&generator.FailedTxnInfo{
					Index:  index,
					TxHash: txHash.String(),
					Error: &generator.TxnError{
						Error:     err,
						ErrorType: generator.ReceiptErrorType,
					},
				})
				atomic.AddUint64(&l.metrics.FailedTransactionsCount, 1)

				return
			}

			l.initGasMetricsBlocksMap(receipt.BlockNumber)

			// Stop the performance timer
			end := time.Now()

			l.metrics.TransactionDuration.reportTurnAroundTime(
				txHash,
				&metadata{
					turnAroundTime: end.Sub(start),
					blockNumber:    receipt.BlockNumber,
				},
			)
		}(i)
	}

	wg.Wait()

	endTime := time.Now()

	if err := l.calculateGasMetrics(jsonClient, l.metrics.GasMetrics); err != nil {
		return fmt.Errorf("unable to calculate block gas metrics: %w", err)
	}

	// Calculate the turn around metrics now that the loadbot is done
	l.metrics.TransactionDuration.calcTurnAroundMetrics()
	l.metrics.TransactionDuration.TotalExecTime = endTime.Sub(startTime)

	return nil
}

func (l *Loadbot) executeTxn(
	client txpoolOp.TxnPoolOperatorClient,
) (web3.Hash, error) {
	txn, err := l.generator.GenerateTransaction()
	if err != nil {
		return web3.Hash{}, err
	}

	addReq := &txpoolOp.AddTxnReq{
		Raw: &any.Any{
			Value: txn.MarshalRLP(),
		},
		From: types.ZeroAddress.String(),
	}

	addRes, addErr := client.AddTxn(context.Background(), addReq)
	if addErr != nil {
		return web3.Hash{}, fmt.Errorf("unable to add transaction, %w", addErr)
	}

	return web3.Hash(types.StringToHash(addRes.TxHash)), nil
}
