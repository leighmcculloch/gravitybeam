package main

import (
	"encoding/base64"
	"fmt"

	"github.com/stellar/go/txnbuild"
	"go.etcd.io/bbolt"
)

// TODO: Add a command for periodically clearing out old transactions.

func StoreAndUpdate(db *bbolt.DB, txHash [32]byte, tx *txnbuild.Transaction) (*txnbuild.Transaction, error) {
	err := db.Update(func(dbtx *bbolt.Tx) error {
		b, err := dbtx.CreateBucketIfNotExists([]byte("txs"))
		if err != nil {
			return fmt.Errorf("creating bucket if not exists: %w", err)
		}
		storedTxBytes := b.Get(txHash[:])
		if storedTxBytes != nil {
			storedTxB64 := base64.StdEncoding.EncodeToString(storedTxBytes)
			storedTxGeneric, err := txnbuild.TransactionFromXDR(storedTxB64)
			if err != nil {
				return fmt.Errorf("parsing stored tx %x: %w", txHash, err)
			}
			// TODO: Support all tx types.
			storedTx, ok := storedTxGeneric.Transaction()
			if !ok {
				return fmt.Errorf("unsupported tx type for tx %x: %w", txHash, err)
			}
			sigsSeen := map[string]bool{}
			for _, s := range tx.Signatures() {
				b, err := s.MarshalBinary()
				if !ok {
					return fmt.Errorf("unexpected error marshaling sig %x: %w", txHash, err)
				}
				sigsSeen[string(b)] = true
			}
			for _, s := range storedTx.Signatures() {
				b, err := s.MarshalBinary()
				if !ok {
					return fmt.Errorf("unexpected error sig %x: %w", txHash, err)
				}
				if sigsSeen[string(b)] {
					continue
				}
				tx, err = tx.AddSignatureDecorated(s)
				if err != nil {
					return fmt.Errorf("adding signature to tx %x: %w", txHash, err)
				}
			}
		}
		txBytes, err := tx.MarshalBinary()
		if err != nil {
			return fmt.Errorf("encoding tx %x to XDR: %w", txHash, err)
		}
		err = b.Put(txHash[:], txBytes)
		if err != nil {
			return fmt.Errorf("storing tx %x: %w", txHash, err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store and update tx %x: %w", txHash, err)
	}
	return tx, nil
}
