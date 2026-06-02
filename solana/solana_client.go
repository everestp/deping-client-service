package solana

import (
	"context"

	"fmt"
	"strconv"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type Client struct {
	rpcClient *rpc.Client
}

func NewClient(rpcURL string) *Client {
	return &Client{rpcClient: rpc.New(rpcURL)}
}

// GetTransactionStatus returns status using a safe 2-argument RPC call
func (c *Client) GetTransactionStatus(ctx context.Context, sig solana.Signature) (string, error) {
	out, err := c.rpcClient.GetSignatureStatuses(
    ctx,
    true,
    sig,
)
	if err != nil {
		return "", err
	}
	if out == nil || out.Value == nil || len(out.Value) == 0 || out.Value[0] == nil {
		return "not_found", nil
	}

	status := out.Value[0]
	switch status.ConfirmationStatus {
	case rpc.ConfirmationStatusFinalized: return "finalized", nil
	case rpc.ConfirmationStatusConfirmed: return "confirmed", nil
	case rpc.ConfirmationStatusProcessed: return "processed", nil
	default: return "pending", nil
	}
}





type TransferInfo struct {
    Amount    uint64
    Receiver  string
    Timestamp int64
    Mint      string
}

func (c *Client) GetTransactionInfo(ctx context.Context, sig solana.Signature, mint solana.PublicKey) (*TransferInfo, error) {
    out, err := c.rpcClient.GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
        Encoding: solana.EncodingJSONParsed,
		Commitment: rpc.CommitmentConfirmed,
    })
if err != nil {
        // Log the actual error to see if it's a network issue or missing index
        fmt.Printf("RPC Error for %s: %v\n", sig.String(), err)
        return nil, fmt.Errorf("transaction not found")
    }
	if out == nil || out.Meta == nil {
        return nil, fmt.Errorf("transaction data empty")
    }

    var ts int64
    if out.BlockTime != nil {
        ts = int64(*out.BlockTime)
    }

    // Capture the first relevant token balance for the specified mint
    for _, balance := range out.Meta.PostTokenBalances {
        if balance.Mint.Equals(mint) {
            amount, _ := strconv.ParseUint(balance.UiTokenAmount.Amount, 10, 64)
            return &TransferInfo{
                Amount:    amount,
                Receiver:  balance.Owner.String(),
                Timestamp: ts,
            }, nil
        }
    }
    return nil, fmt.Errorf("no token balance info found for this mint")
}



