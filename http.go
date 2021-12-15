package main

import (
	"encoding/hex"
	"net/http"

	"github.com/go-chi/render"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stellar/go/clients/horizonclient"
	supportlog "github.com/stellar/go/support/log"
	"github.com/stellar/go/txnbuild"
	"go.etcd.io/bbolt"
)

type TransactionHandler struct {
	NetworkPassphrase string
	HorizonClient     horizonclient.ClientInterface
	Logger            *supportlog.Entry
	DB                *bbolt.DB
	Topic             *pubsub.Topic
}

type TransactionRequest struct {
	Transaction *txnbuild.Transaction `json:"xdr"`
}

type TransactionResponse struct {
	Accepted bool `json:"accepted"`
}

func (h *TransactionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.Logger.Ctx(ctx)

	req := TransactionRequest{}
	err := render.DecodeJSON(r.Body, &req)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		logger.WithStack(err).Error(err)
		return
	}

	tx := req.Transaction

	hash, err := tx.Hash(h.NetworkPassphrase)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		logger.WithStack(err).Error(err)
		return
	}
	logger = logger.WithField("tx", hex.EncodeToString(hash[:]))
	logger.Infof("tx received: sig count: %d", len(tx.Signatures()))

	txBytes, err := tx.MarshalBinary()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		logger.WithStack(err).Error(err)
		return
	}

	err = h.Topic.Publish(r.Context(), txBytes)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		logger.WithStack(err).Error(err)
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, TransactionResponse{Accepted: true})
}
