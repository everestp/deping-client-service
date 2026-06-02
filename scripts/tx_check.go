package main

import (
	"context"
	"fmt"
	"log"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

func main() {
	// Replace with your RPC
	client := rpc.New("https://api.mainnet-beta.solana.com")

	// Replace with your tx signature
	txSig := "PASTE_TX_SIGNATURE_HERE"

	tx, err := client.GetTransaction(
		context.Background(),
		solana.MustSignatureFromBase58(txSig),
		&rpc.GetTransactionOpts{
			Encoding: solana.EncodingBase64,
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("============== TX INFO ==============")

	if tx.BlockTime != nil {
		fmt.Println("Timestamp:", int64(*tx.BlockTime))
	}

	fmt.Println("Slot:", tx.Slot)

	parsedTx, err := tx.Transaction.GetTransaction()
	if err != nil {
		log.Fatal(err)
	}

	keys := parsedTx.Message.AccountKeys

	fmt.Println("\nAccounts:")
	for i, k := range keys {
		fmt.Printf("[%d] %s\n", i, k.String())
	}

	fmt.Println("\nBalance Changes:")

	pre := tx.Meta.PreBalances
	post := tx.Meta.PostBalances

	for i := range pre {
		diff := int64(post[i]) - int64(pre[i])

		if diff != 0 {
			fmt.Printf(
				"Account: %s\nChange: %.9f SOL (%d lamports)\n\n",
				keys[i].String(),
				float64(diff)/1e9,
				diff,
			)
		}
	}

	fmt.Println("=====================================")
}
