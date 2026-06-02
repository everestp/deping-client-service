package solana

import (
	"context"
	"fmt"
	"strconv"

	"github.com/everestp/deping-client-service/config/env"
	solgo "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

)

type Client struct {
	rpcClient *rpc.Client
}

func NewSolanaClient(rpcURL string) *Client {
	return &Client{rpcClient: rpc.New(rpcURL)}
}

// =========================
// TX STATUS
// =========================

func (c *Client) GetTransactionStatus(ctx context.Context, sig solgo.Signature) (string, error) {

	out, err := c.rpcClient.GetSignatureStatuses(
		ctx,
		true,
		sig,
	)
	if err != nil {
		return "", err
	}

	if out == nil || len(out.Value) == 0 || out.Value[0] == nil {
		return "not_found", nil
	}

	status := out.Value[0]

	switch status.ConfirmationStatus {
	case rpc.ConfirmationStatusFinalized:
		return "finalized", nil
	case rpc.ConfirmationStatusConfirmed:
		return "confirmed", nil
	case rpc.ConfirmationStatusProcessed:
		return "processed", nil
	default:
		return "pending", nil
	}
}

// =========================
// TX INFO (SPL TOKEN)
// =========================

type TxInfo struct {
	Mint      string
	Amount    uint64
	Timestamp int64
	Sender    string
	Receiver  string
}

func (c *Client) GetTransferInfo(
	ctx context.Context,
	sig string,
) (*TxInfo, error) {

	tx, err := c.rpcClient.GetTransaction(
		ctx,
		solgo.MustSignatureFromBase58(sig),
		&rpc.GetTransactionOpts{
			Encoding: solgo.EncodingBase64,
		},
	)
	if err != nil {
		return nil, err
	}

	if tx.Meta == nil || tx.Transaction == nil {
		return nil, fmt.Errorf("transaction data missing")
	}

	// parse tx (not strictly needed for SPL diff logic)
	_, err = tx.Transaction.GetTransaction()
	if err != nil {
		return nil, err
	}

	mint := solgo.MustPublicKeyFromBase58(env.Get().DepingMintAddress)

	var (
		sender    string
		receiver  string
		amount    uint64
		blockTime int64
	)

	if tx.BlockTime != nil {
		blockTime = int64(*tx.BlockTime)
	}

	// =========================
	// SPL TOKEN BALANCE DIFF
	// =========================
	for _, pre := range tx.Meta.PreTokenBalances {

if pre.Mint != mint {
			continue
		}


			post := findPost(tx.Meta.PostTokenBalances, pre.AccountIndex)

		preAmt := parse(pre.UiTokenAmount.Amount)
		postAmt := parse(post.UiTokenAmount.Amount)


		diff := postAmt - preAmt

		if diff < 0 {
			sender = pre.Owner.String()
			amount = uint64(-diff)
		}

		if diff > 0 {
			receiver = pre.Owner.String()
		}
	}

	return &TxInfo{
		Mint:      mint.String(),
		Amount:    amount,
		Timestamp: blockTime,
		Sender:    sender,
		Receiver:  receiver,
	}, nil
}

// =========================
// HELPERS
// =========================



func findPost(list []rpc.TokenBalance, index uint16) rpc.TokenBalance {
	for _, p := range list {
		if p.AccountIndex == index {
			return p
		}
	}

	return rpc.TokenBalance{
		UiTokenAmount: &rpc.UiTokenAmount{
			Amount: "0",
		},
	}
}

func parse(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
