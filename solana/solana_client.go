package solana

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/everestp/deping-client-service/config/env"
	solgo "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Client struct {
	RPC       *rpc.Client
	ProgramID solgo.PublicKey
}

func NewSolanaClient(rpcURL string, programID string) (*Client, error) {
	progID, err := solgo.PublicKeyFromBase58(programID)
	if err != nil {
		return nil, fmt.Errorf("invalid program ID: %w", err)
	}

	return &Client{
		RPC:       rpc.New(rpcURL),
		ProgramID: progID,
	}, nil
}

// =========================
// TX STATUS
// =========================

func (c *Client) GetTransactionStatus(ctx context.Context, sig solgo.Signature) (string, error) {
	out, err := c.RPC.GetSignatureStatuses(ctx, true, sig)
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

func (c *Client) GetTransferInfo(ctx context.Context, sig string) (*TxInfo, error) {
	// 1. Fetch transaction metadata from RPC
	tx, err := c.RPC.GetTransaction(
		ctx,
		solgo.MustSignatureFromBase58(sig),
		&rpc.GetTransactionOpts{
			Encoding: solgo.EncodingBase64,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("rpc GetTransaction call failed for sig %s: %w", sig, err)
	}

	if tx.Meta == nil || tx.Transaction == nil {
		return nil, fmt.Errorf("transaction or meta field missing from RPC details")
	}

	_, err = tx.Transaction.GetTransaction()
	if err != nil {
		return nil, fmt.Errorf("failed parsing layout transaction structure: %w", err)
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

	foundSender := false
	foundReceiver := false

	// =========================================================
	// OPTIMIZED SPL TOKEN BALANCE DIFF LOOKUP MATRIX
	// =========================================================
	for _, pre := range tx.Meta.PreTokenBalances {
		if pre.Mint != mint {
			continue
		}

		// Use the boolean flag 'found' from our updated pointer helper
		post, found := findPost(tx.Meta.PostTokenBalances, pre.AccountIndex)

		var postAmt int64
		if !found {
			postAmt = 0
		} else {
			postAmt = parse(post.UiTokenAmount.Amount)
		}

		preAmt := parse(pre.UiTokenAmount.Amount)
		diff := postAmt - preAmt

		// Balance decreased -> This is the SENDER
		if diff < 0 {
			sender = pre.Owner.String()
			amount = uint64(-diff)
			foundSender = true
		}

		// Balance increased -> This is the RECEIVER
		if diff > 0 {
			receiver = pre.Owner.String()
			foundReceiver = true
		}
	}

	// 2. Added debugging assertions to fail quickly if token parsing is weird
	if !foundSender || !foundReceiver {
		return nil, fmt.Errorf("on-chain validation mismatch: parsed sender_found=%t (%s), receiver_found=%t (%s). Verify DepingMintAddress matches transaction layout",
			foundSender, sender, foundReceiver, receiver)
	}

	if amount == 0 {
		return nil, fmt.Errorf("transaction parsing finalized with an invalid payload transfer value of 0 tokens")
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

// FIXED: Returns a pointer along with a boolean flag representing successful indexing match
func findPost(list []rpc.TokenBalance, index uint16) (*rpc.TokenBalance, bool) {
	for _, p := range list {
		if p.AccountIndex == index {
			return &p, true
		}
	}
	return nil, false
}

func parse(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func (c *Client) DeriveNodePDA(ownerPubKey solgo.PublicKey, email string) (solgo.PublicKey, uint8, error) {
	emailHash := sha256.Sum256([]byte(email))

	seeds := [][]byte{
		[]byte("node"),
		ownerPubKey.Bytes(),
		emailHash[:],
	}

	return solgo.FindProgramAddress(seeds, c.ProgramID)
}