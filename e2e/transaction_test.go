package e2e

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/juanidrobo/polygon-edge/contracts/abis"
	"github.com/juanidrobo/polygon-edge/crypto"
	"github.com/juanidrobo/polygon-edge/e2e/framework"
	"github.com/juanidrobo/polygon-edge/helper/tests"
	txpoolOp "github.com/juanidrobo/polygon-edge/txpool/proto"
	"github.com/juanidrobo/polygon-edge/types"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/stretchr/testify/assert"
	"github.com/umbracle/go-web3"
	"github.com/umbracle/go-web3/jsonrpc"
)

func TestSignedTransaction(t *testing.T) {
	senderKey, senderAddr := tests.GenerateKeyAndAddr(t)
	_, receiverAddr := tests.GenerateKeyAndAddr(t)

	preminedAmount := framework.EthToWei(10)
	ibftManager := framework.NewIBFTServersManager(
		t,
		IBFTMinNodes,
		IBFTDirPrefix,
		func(i int, config *framework.TestServerConfig) {
			config.Premine(senderAddr, preminedAmount)
			config.SetSeal(true)
		})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ibftManager.StartServers(ctx)

	srv := ibftManager.GetServer(0)
	clt := srv.JSONRPC()

	// check there is enough balance
	balance, err := clt.Eth().GetBalance(web3.Address(senderAddr), web3.Latest)
	assert.NoError(t, err)
	assert.Equal(t, preminedAmount, balance)

	for i := 0; i < 5; i++ {
		txn := &framework.PreparedTransaction{
			From:     senderAddr,
			To:       &receiverAddr,
			GasPrice: big.NewInt(10000),
			Gas:      1000000,
			Value:    big.NewInt(10000),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		receipt, err := srv.SendRawTx(ctx, txn, senderKey)
		assert.NoError(t, err)
		assert.NotNil(t, receipt)
		assert.NotNil(t, receipt.TransactionHash)
	}
}

func TestPreminedBalance(t *testing.T) {
	preminedAccounts := []struct {
		address types.Address
		balance *big.Int
	}{
		{types.StringToAddress("1"), big.NewInt(0)},
		{types.StringToAddress("2"), big.NewInt(20)},
	}

	testTable := []struct {
		name    string
		address types.Address
		balance *big.Int
	}{
		{
			"Account with 0 balance",
			preminedAccounts[0].address,
			preminedAccounts[0].balance,
		},
		{
			"Account with valid balance",
			preminedAccounts[1].address,
			preminedAccounts[1].balance,
		},
		{
			"Account not in genesis",
			types.StringToAddress("3"),
			big.NewInt(0),
		},
	}

	srvs := framework.NewTestServers(t, 1, func(config *framework.TestServerConfig) {
		config.SetConsensus(framework.ConsensusDev)
		for _, acc := range preminedAccounts {
			config.Premine(acc.address, acc.balance)
		}
	})
	srv := srvs[0]

	rpcClient := srv.JSONRPC()

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			balance, err := rpcClient.Eth().GetBalance(web3.Address(testCase.address), web3.Latest)
			assert.NoError(t, err)
			assert.Equal(t, testCase.balance, balance)
		})
	}
}

func TestEthTransfer(t *testing.T) {
	accountBalances := []*big.Int{
		framework.EthToWei(50), // 50 ETH
		big.NewInt(0),
		framework.EthToWei(10), // 10 ETH

	}

	validAccounts := make([]testAccount, len(accountBalances))

	for indx := 0; indx < len(accountBalances); indx++ {
		key, addr := tests.GenerateKeyAndAddr(t)

		validAccounts[indx] = testAccount{
			address: addr,
			key:     key,
			balance: accountBalances[indx],
		}
	}

	testTable := []struct {
		name          string
		sender        types.Address
		senderKey     *ecdsa.PrivateKey
		recipient     types.Address
		amount        *big.Int
		shouldSucceed bool
	}{
		{
			// ACC #1 -> ACC #3
			"Valid ETH transfer #1",
			validAccounts[0].address,
			validAccounts[0].key,
			validAccounts[2].address,
			framework.EthToWei(10),
			true,
		},
		{
			// ACC #2 -> ACC #3
			"Invalid ETH transfer",
			validAccounts[1].address,
			validAccounts[1].key,
			validAccounts[2].address,
			framework.EthToWei(100),
			false,
		},
		{
			// ACC #3 -> ACC #2
			"Valid ETH transfer #2",
			validAccounts[2].address,
			validAccounts[2].key,
			validAccounts[1].address,
			framework.EthToWei(5),
			true,
		},
	}

	srvs := framework.NewTestServers(t, 1, func(config *framework.TestServerConfig) {
		config.SetConsensus(framework.ConsensusDev)
		config.SetSeal(true)
		for _, acc := range validAccounts {
			config.Premine(acc.address, acc.balance)
		}
	})
	srv := srvs[0]

	rpcClient := srv.JSONRPC()

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			// Fetch the balances before sending
			balanceSender, err := rpcClient.Eth().GetBalance(
				web3.Address(testCase.sender),
				web3.Latest,
			)
			assert.NoError(t, err)

			balanceReceiver, err := rpcClient.Eth().GetBalance(
				web3.Address(testCase.recipient),
				web3.Latest,
			)
			assert.NoError(t, err)

			// Set the preSend balances
			previousSenderBalance := balanceSender
			previousReceiverBalance := balanceReceiver

			// Do the transfer
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			txn := &framework.PreparedTransaction{
				From:     testCase.sender,
				To:       &testCase.recipient,
				GasPrice: big.NewInt(1048576),
				Gas:      1000000,
				Value:    testCase.amount,
			}

			receipt, err := srv.SendRawTx(ctx, txn, testCase.senderKey)

			if testCase.shouldSucceed {
				assert.NoError(t, err)
				assert.NotNil(t, receipt)
			} else { // When an invalid transaction is supplied, there should be no receipt.
				assert.Error(t, err)
				assert.Nil(t, receipt)
			}

			// Fetch the balances after sending
			balanceSender, err = rpcClient.Eth().GetBalance(
				web3.Address(testCase.sender),
				web3.Latest,
			)
			assert.NoError(t, err)

			balanceReceiver, err = rpcClient.Eth().GetBalance(
				web3.Address(testCase.recipient),
				web3.Latest,
			)
			assert.NoError(t, err)

			expectedSenderBalance := previousSenderBalance
			expectedReceiverBalance := previousReceiverBalance
			if testCase.shouldSucceed {
				fee := new(big.Int).Mul(
					big.NewInt(int64(receipt.GasUsed)),
					txn.GasPrice,
				)

				expectedSenderBalance = previousSenderBalance.Sub(
					previousSenderBalance,
					new(big.Int).Add(testCase.amount, fee),
				)

				expectedReceiverBalance = previousReceiverBalance.Add(
					previousReceiverBalance,
					testCase.amount,
				)
			}

			// Check the balances
			assert.Equalf(t,
				expectedSenderBalance,
				balanceSender,
				"Sender balance incorrect")
			assert.Equalf(t,
				expectedReceiverBalance,
				balanceReceiver,
				"Receiver balance incorrect")
		})
	}
}

// getCount is a helper function for the stress test SC
func getCount(
	from types.Address,
	contractAddress web3.Address,
	rpcClient *jsonrpc.Client,
) (*big.Int, error) {
	stressTestMethod, ok := abis.StressTestABI.Methods["getCount"]
	if !ok {
		return nil, errors.New("getCount method doesn't exist in StessTest contract ABI")
	}

	selector := stressTestMethod.ID()
	response, err := rpcClient.Eth().Call(
		&web3.CallMsg{
			From:     web3.Address(from),
			To:       &contractAddress,
			Data:     selector,
			GasPrice: 100000000,
			Value:    big.NewInt(0),
		},
		web3.Latest,
	)

	if err != nil {
		return nil, fmt.Errorf("unable to call StressTest contract method, %w", err)
	}

	if response == "0x" {
		response = "0x0"
	}

	bigResponse, decodeErr := types.ParseUint256orHex(&response)

	if decodeErr != nil {
		return nil, fmt.Errorf("wnable to decode hex response, %w", decodeErr)
	}

	return bigResponse, nil
}

// addStressTestTxns adds numTransactions that call the
// passed in StressTest smart contract method
func addStressTestTxns(
	t *testing.T,
	srv *framework.TestServer,
	numTransactions int,
	contractAddr types.Address,
	senderKey *ecdsa.PrivateKey,
) {
	t.Helper()

	currentNonce := 1 // 1 because the first transaction was deployment
	clt := srv.TxnPoolOperator()

	for i := 0; i < numTransactions; i++ {
		var msg *txpoolOp.AddTxnReq

		setNameTxn := generateStressTestTx(
			t,
			uint64(currentNonce),
			contractAddr,
			senderKey,
		)
		currentNonce++

		msg = &txpoolOp.AddTxnReq{
			Raw: &any.Any{
				Value: setNameTxn.MarshalRLP(),
			},
			From: types.ZeroAddress.String(),
		}

		_, addErr := clt.AddTxn(context.Background(), msg)
		if addErr != nil {
			t.Fatalf("Unable to add txn #%d, %v", i, addErr)
		}
	}
}

// Test scenario (Dev mode):
// Deploy the StressTest smart contract and send ~50 transactions
// that modify it's state, and make sure that all
// transactions were correctly executed
func Test_TransactionDevLoop(t *testing.T) {
	senderKey, sender := tests.GenerateKeyAndAddr(t)
	defaultBalance := framework.EthToWei(100)

	// Set up the test server
	srvs := framework.NewTestServers(t, 1, func(config *framework.TestServerConfig) {
		config.SetConsensus(framework.ConsensusDev)
		config.SetSeal(true)
		config.Premine(sender, defaultBalance)
		config.SetBlockLimit(20000000)
	})
	srv := srvs[0]
	client := srv.JSONRPC()

	// Deploy the stress test contract
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	contractAddr, err := srv.DeployContract(ctx, stressTestBytecode, senderKey)

	if err != nil {
		t.Fatal(err)
	}

	count, countErr := getCount(sender, contractAddr, client)
	if countErr != nil {
		t.Fatalf("Unable to call count method, %v", countErr)
	}

	// Check that the count is 0 before running the test
	assert.Equalf(t, "0", count.String(), "Count doesn't match")

	// Send ~50 transactions
	numTransactions := 50

	// Add stress test transactions
	addStressTestTxns(
		t,
		srv,
		numTransactions,
		types.StringToAddress(contractAddr.String()),
		senderKey,
	)

	// Wait for the final tx to be mined
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer retryCancel()

	_, err = tests.WaitForNonce(
		retryCtx,
		client.Eth(),
		web3.BytesToAddress(sender.Bytes()),
		1+uint64(numTransactions), // contract nonce is 1 (EIP-161)
	)
	assert.NoError(t, err)

	count, countErr = getCount(sender, contractAddr, client)
	if countErr != nil {
		t.Fatalf("Unable to call count method, %v", countErr)
	}

	// Check that the count is 0 before running the test
	assert.Equalf(t, strconv.Itoa(numTransactions), count.String(), "Count doesn't match")
}

// generateStressTestTx generates a transaction for the
// IBFT_Loop and Dev_Loop stress tests
func generateStressTestTx(
	t *testing.T,
	currentNonce uint64,
	contractAddr types.Address,
	senderKey *ecdsa.PrivateKey,
) *types.Transaction {
	t.Helper()

	bigGasPrice := big.NewInt(framework.DefaultGasPrice)
	signer := crypto.NewEIP155Signer(100)

	setNameMethod, ok := abis.StressTestABI.Methods["setName"]
	if !ok {
		t.Fatalf("Unable to get setName method")
	}

	encodedInput, encodeErr := setNameMethod.Inputs.Encode(
		map[string]interface{}{
			"sName": fmt.Sprintf("Name #%d", currentNonce),
		},
	)
	if encodeErr != nil {
		t.Fatalf("Unable to encode inputs, %v", encodeErr)
	}

	signedTx, signErr := signer.SignTx(&types.Transaction{
		Nonce:    currentNonce,
		From:     types.ZeroAddress,
		To:       &contractAddr,
		GasPrice: bigGasPrice,
		Gas:      framework.DefaultGasLimit,
		Value:    big.NewInt(0),
		V:        big.NewInt(1), // it is necessary to encode in rlp,
		Input:    append(setNameMethod.ID(), encodedInput...),
	}, senderKey)

	if signErr != nil {
		t.Fatalf("Unable to sign transaction, %v", signErr)
	}

	return signedTx
}

// addStressTxnsWithHashes adds numTransactions that call the
// passed in StressTest smart contract method, but saves their transaction
// hashes
func addStressTxnsWithHashes(
	t *testing.T,
	srv *framework.TestServer,
	numTransactions int,
	contractAddr types.Address,
	senderKey *ecdsa.PrivateKey,
) []web3.Hash {
	t.Helper()

	currentNonce := 1 // 1 because the first transaction was deployment

	txHashes := make([]web3.Hash, 0)

	for i := 0; i < numTransactions; i++ {
		setNameTxn := generateStressTestTx(
			t,
			uint64(currentNonce),
			contractAddr,
			senderKey,
		)
		currentNonce++

		if txHash, err := srv.JSONRPC().Eth().SendRawTransaction(setNameTxn.MarshalRLP()); err == nil {
			txHashes = append(txHashes, txHash)
		}
	}

	return txHashes
}

// Test scenario (IBFT):
// Deploy the StressTest smart contract and send ~50 transactions
// that modify it's state, and make sure that all
// transactions were correctly executed
func Test_TransactionIBFTLoop(t *testing.T) {
	senderKey, sender := tests.GenerateKeyAndAddr(t)
	defaultBalance := framework.EthToWei(100)

	// Set up the test server
	ibftManager := framework.NewIBFTServersManager(
		t,
		IBFTMinNodes,
		IBFTDirPrefix,
		func(i int, config *framework.TestServerConfig) {
			config.Premine(sender, defaultBalance)
			config.SetSeal(true)
			config.SetBlockLimit(20000000)
		})

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ibftManager.StartServers(ctx)

	srv := ibftManager.GetServer(0)
	client := srv.JSONRPC()

	// Deploy the stress test contract
	deployCtx, deployCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deployCancel()

	buf, err := hex.DecodeString(stressTestBytecode)
	if err != nil {
		t.Fatalf("Unable to decode bytecode, %v", err)
	}

	deployTx := &framework.PreparedTransaction{
		From:     sender,
		GasPrice: big.NewInt(framework.DefaultGasPrice),
		Gas:      framework.DefaultGasLimit,
		Value:    big.NewInt(0),
		Input:    buf,
	}
	receipt, err := srv.SendRawTx(deployCtx, deployTx, senderKey)
	assert.NoError(t, err)
	assert.NotNil(t, receipt)

	contractAddr := receipt.ContractAddress

	if err != nil {
		t.Fatalf("Unable to send transaction, %v", err)
	}

	count, countErr := getCount(sender, contractAddr, client)
	if countErr != nil {
		t.Fatalf("Unable to call count method, %v", countErr)
	}

	// Check that the count is 0 before running the test
	assert.Equalf(t, "0", count.String(), "Count doesn't match")

	// Send ~50 transactions
	numTransactions := 50

	var wg sync.WaitGroup

	wg.Add(numTransactions)

	// Add stress test transactions
	txHashes := addStressTxnsWithHashes(
		t,
		srv,
		numTransactions,
		types.StringToAddress(contractAddr.String()),
		senderKey,
	)
	if len(txHashes) != numTransactions {
		t.Fatalf(
			"Invalid number of txns sent [sent %d, expected %d]",
			len(txHashes),
			numTransactions,
		)
	}

	// For each transaction hash, wait for it to get included into a block
	for index, txHash := range txHashes {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Minute*3)

		receipt, receiptErr := tests.WaitForReceipt(waitCtx, client.Eth(), txHash)
		if receipt == nil {
			t.Fatalf("Unable to get receipt for hash index [%d]", index)
		} else if receiptErr != nil {
			t.Fatalf("Unable to get receipt for hash index [%d], %v", index, receiptErr)
		}

		waitCancel()
		wg.Done()
	}

	wg.Wait()

	statusCtx, statusCancel := context.WithTimeout(context.Background(), time.Second*30)
	defer statusCancel()

	resp, err := tests.WaitUntilTxPoolEmpty(statusCtx, srv.TxnPoolOperator())

	if err != nil {
		t.Fatalf("Unable to get txpool status, %v", err)
	}

	assert.Equal(t, 0, int(resp.Length))

	count, countErr = getCount(sender, contractAddr, client)
	if countErr != nil {
		t.Fatalf("Unable to call count method, %v", countErr)
	}

	// Check that the count is correct
	assert.Equalf(t, strconv.Itoa(numTransactions), count.String(), "Count doesn't match")
}
