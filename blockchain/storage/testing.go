package storage

import (
	"fmt"
	"math/big"
	"reflect"
	"testing"

	"github.com/juanidrobo/polygon-edge/helper/hex"
	"github.com/juanidrobo/polygon-edge/types"
	"github.com/stretchr/testify/assert"
)

type MockStorage func(t *testing.T) (Storage, func())

var (
	addr1 = types.StringToAddress("1")
	addr2 = types.StringToAddress("2")

	hash1 = types.StringToHash("1")
	hash2 = types.StringToHash("2")
)

// TestStorage tests a set of tests on a storage
func TestStorage(t *testing.T, m MockStorage) {
	t.Helper()

	t.Run("", func(t *testing.T) {
		testCanonicalChain(t, m)
	})
	t.Run("", func(t *testing.T) {
		testDifficulty(t, m)
	})
	t.Run("", func(t *testing.T) {
		testHead(t, m)
	})
	t.Run("", func(t *testing.T) {
		testForks(t, m)
	})
	t.Run("", func(t *testing.T) {
		testHeader(t, m)
	})
	t.Run("", func(t *testing.T) {
		testBody(t, m)
	})
	t.Run("", func(t *testing.T) {
		testWriteCanonicalHeader(t, m)
	})
	t.Run("", func(t *testing.T) {
		testReceipts(t, m)
	})
}

func testCanonicalChain(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Number     uint64
		ParentHash types.Hash
		Hash       types.Hash
	}{
		{
			Number:     1,
			ParentHash: types.StringToHash("111"),
		},
		{
			Number:     1,
			ParentHash: types.StringToHash("222"),
		},
		{
			Number:     2,
			ParentHash: types.StringToHash("111"),
		},
	}

	for _, cc := range cases {
		h := &types.Header{
			Number:     cc.Number,
			ParentHash: cc.ParentHash,
			ExtraData:  []byte{0x1},
		}

		hash := h.Hash

		if err := s.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if err := s.WriteCanonicalHash(cc.Number, hash); err != nil {
			t.Fatal(err)
		}

		data, ok := s.ReadCanonicalHash(cc.Number)
		if !ok {
			t.Fatal("not found")
		}

		if !reflect.DeepEqual(data, hash) {
			t.Fatal("not match")
		}
	}
}

func testDifficulty(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Diff *big.Int
	}{
		{
			Diff: big.NewInt(10),
		},
		{
			Diff: big.NewInt(11),
		},
		{
			Diff: big.NewInt(12),
		},
	}

	for indx, cc := range cases {
		h := &types.Header{
			Number:    uint64(indx),
			ExtraData: []byte{},
		}

		hash := h.Hash

		if err := s.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if err := s.WriteTotalDifficulty(hash, cc.Diff); err != nil {
			t.Fatal(err)
		}

		diff, ok := s.ReadTotalDifficulty(hash)
		if !ok {
			t.Fatal("not found")
		}

		if !reflect.DeepEqual(cc.Diff, diff) {
			t.Fatal("bad")
		}
	}
}

func testHead(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	for i := uint64(0); i < 5; i++ {
		h := &types.Header{
			Number:    i,
			ExtraData: []byte{},
		}
		hash := h.Hash

		if err := s.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if err := s.WriteHeadNumber(i); err != nil {
			t.Fatal(err)
		}

		if err := s.WriteHeadHash(hash); err != nil {
			t.Fatal(err)
		}

		n2, ok := s.ReadHeadNumber()
		if !ok {
			t.Fatal("num not found")
		}

		if n2 != i {
			t.Fatal("bad")
		}

		hash1, ok := s.ReadHeadHash()
		if !ok {
			t.Fatal("hash not found")
		}

		if !reflect.DeepEqual(hash1, hash) {
			t.Fatal("bad")
		}
	}
}

func testForks(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	var cases = []struct {
		Forks []types.Hash
	}{
		{[]types.Hash{types.StringToHash("111"), types.StringToHash("222")}},
		{[]types.Hash{types.StringToHash("111")}},
	}

	for _, cc := range cases {
		if err := s.WriteForks(cc.Forks); err != nil {
			t.Fatal(err)
		}

		forks, err := s.ReadForks()
		assert.NoError(t, err)

		if !reflect.DeepEqual(cc.Forks, forks) {
			t.Fatal("bad")
		}
	}
}

func testHeader(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	header := &types.Header{
		Number:     5,
		Difficulty: 17179869184,
		ParentHash: types.StringToHash("11"),
		Timestamp:  10,
		// if not set it will fail
		ExtraData: hex.MustDecodeHex("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
	}
	header.ComputeHash()

	if err := s.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	header1, err := s.ReadHeader(header.Hash)
	assert.NoError(t, err)

	if !reflect.DeepEqual(header, header1) {
		t.Fatal("bad")
	}
}

func testBody(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	header := &types.Header{
		Number:     5,
		Difficulty: 10,
		ParentHash: types.StringToHash("11"),
		Timestamp:  10,
		ExtraData:  []byte{}, // if not set it will fail
	}
	if err := s.WriteHeader(header); err != nil {
		panic(err)
	}

	addr1 := types.StringToAddress("11")
	t0 := &types.Transaction{
		Nonce:    0,
		To:       &addr1,
		Value:    big.NewInt(1),
		Gas:      11,
		GasPrice: big.NewInt(11),
		Input:    []byte{1, 2},
		V:        big.NewInt(1),
	}
	t0.ComputeHash()

	addr2 := types.StringToAddress("22")
	t1 := &types.Transaction{
		Nonce:    0,
		To:       &addr2,
		Value:    big.NewInt(1),
		Gas:      22,
		GasPrice: big.NewInt(11),
		Input:    []byte{4, 5},
		V:        big.NewInt(2),
	}
	t1.ComputeHash()

	block := types.Block{
		Header:       header,
		Transactions: []*types.Transaction{t0, t1},
	}

	body0 := block.Body()
	if err := s.WriteBody(header.Hash, body0); err != nil {
		panic(err)
	}

	body1, err := s.ReadBody(header.Hash)
	assert.NoError(t, err)

	// NOTE: reflect.DeepEqual does not seem to work, check the hash of the transactions
	tx0, tx1 := body0.Transactions, body1.Transactions
	if len(tx0) != len(tx1) {
		t.Fatal("lengths are different")
	}

	for indx, i := range tx0 {
		if i.Hash != tx1[indx].Hash {
			t.Fatal("tx not correct")
		}
	}
}

func testReceipts(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	h := &types.Header{
		Difficulty: 133,
		Number:     11,
		ExtraData:  []byte{},
	}
	if err := s.WriteHeader(h); err != nil {
		t.Fatal(err)
	}

	txn := &types.Transaction{
		Nonce:    1000,
		Gas:      50,
		GasPrice: new(big.Int).SetUint64(100),
		V:        big.NewInt(11),
	}
	body := &types.Body{
		Transactions: []*types.Transaction{txn},
	}

	if err := s.WriteBody(h.Hash, body); err != nil {
		t.Fatal(err)
	}

	r0 := &types.Receipt{
		Root:              types.StringToHash("1"),
		CumulativeGasUsed: 10,
		TxHash:            txn.Hash,
		LogsBloom:         types.Bloom{0x1},
		Logs: []*types.Log{
			{
				Address: addr1,
				Topics:  []types.Hash{hash1, hash2},
				Data:    []byte{0x1, 0x2},
			},
			{
				Address: addr2,
				Topics:  []types.Hash{hash1},
			},
		},
	}
	r1 := &types.Receipt{
		Root:              types.StringToHash("1"),
		CumulativeGasUsed: 10,
		TxHash:            txn.Hash,
		LogsBloom:         types.Bloom{0x1},
		GasUsed:           10,
		ContractAddress:   types.Address{0x1},
		Logs: []*types.Log{
			{
				Address: addr2,
				Topics:  []types.Hash{hash1},
			},
		},
	}

	receipts := []*types.Receipt{r0, r1}

	if err := s.WriteReceipts(h.Hash, receipts); err != nil {
		t.Fatal(err)
	}

	found, err := s.ReadReceipts(h.Hash)

	if err != nil {
		t.Fatal(err)
	}

	assert.True(t, reflect.DeepEqual(receipts, found))
}

func testWriteCanonicalHeader(t *testing.T, m MockStorage) {
	t.Helper()

	s, closeFn := m(t)
	defer closeFn()

	h := &types.Header{
		Number:    100,
		ExtraData: []byte{0x1},
	}
	h.ComputeHash()

	diff := new(big.Int).SetUint64(100)

	if err := s.WriteCanonicalHeader(h, diff); err != nil {
		t.Fatal(err)
	}

	hh, err := s.ReadHeader(h.Hash)
	assert.NoError(t, err)

	if !reflect.DeepEqual(h, hh) {
		fmt.Println("-- valid --")
		fmt.Println(h)
		fmt.Println("-- found --")
		fmt.Println(hh)

		t.Fatal("bad header")
	}

	headHash, ok := s.ReadHeadHash()
	if !ok {
		t.Fatal("not found head hash")
	}

	if headHash != h.Hash {
		t.Fatal("head hash not correct")
	}

	headNum, ok := s.ReadHeadNumber()
	if !ok {
		t.Fatal("not found head num")
	}

	if headNum != h.Number {
		t.Fatal("head num not correct")
	}

	canHash, ok := s.ReadCanonicalHash(h.Number)
	if !ok {
		t.Fatal("not found can hash")
	}

	if canHash != h.Hash {
		t.Fatal("canonical hash not correct")
	}
}
