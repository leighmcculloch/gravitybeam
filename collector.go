package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stellar/go/clients/horizonclient"
	supportlog "github.com/stellar/go/support/log"
	"github.com/stellar/go/txnbuild"
	"go.etcd.io/bbolt"
)

type TransactionCollector struct {
	NetworkPassphrase string
	Logger            *supportlog.Entry
	DB                *bbolt.DB
	HorizonClient     horizonclient.ClientInterface
	Topic             *pubsub.Topic
}

func (c *TransactionCollector) Collect() {
	err := c.collect()
	if err != nil {
		c.Logger.Error(err)
	}
}

func (c *TransactionCollector) collect() error {
	sub, err := c.Topic.Subscribe()
	if err != nil {
		return err
	}
	ctx := context.Background()
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return err
		}

		txBytes := msg.Data
		txB64 := base64.StdEncoding.EncodeToString(txBytes)
		txGeneric, err := txnbuild.TransactionFromXDR(txB64)
		if err != nil {
			return err
		}
		// TODO: Support all tx types.
		tx, ok := txGeneric.Transaction()
		if !ok {
			return fmt.Errorf("unsupported tx type")
		}

		hash, err := tx.Hash(c.NetworkPassphrase)
		if err != nil {
			return err
		}
		logger := c.Logger.WithField("tx", hex.EncodeToString(hash[:]))
		logger.Infof("tx received: sig count: %d", len(tx.Signatures()))

		tx, err = StoreAndUpdate(c.DB, hash, tx)
		if err != nil {
			return err
		}
		logger.Infof("tx stored and updated: sig count: %d", len(tx.Signatures()))

		tx, err = AuthorizedTransaction(c.HorizonClient, hash, tx)
		if errors.Is(err, ErrNotAuthorized) {
			logger.Infof("tx not yet authorized")
			continue
		} else if err != nil {
			return err
		}
		logger.Infof("tx authorized: sig count: %d", len(tx.Signatures()))

		// Submit transaction.
		go func() {
			logger.Infof("tx submitting: sig count: %d", len(tx.Signatures()))
			txResp, err := c.HorizonClient.SubmitTransaction(tx)
			if err != nil {
				logger.Error(err)
				return
			}
			logger.Infof("tx submitted: successful: %t", txResp.Successful)
		}()
	}
}
