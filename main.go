package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	ff "github.com/peterbourgon/ff/v3"
	"github.com/sirupsen/logrus"
	"github.com/stellar/go/clients/horizonclient"
	supporthttp "github.com/stellar/go/support/http"
	supportlog "github.com/stellar/go/support/log"
	"go.etcd.io/bbolt"
)

func main() {
	logger := supportlog.New()
	logger.SetLevel(logrus.InfoLevel)
	err := run(os.Args[1:], logger)
	if err != nil {
		logger.WithStack(err).Error(err)
		os.Exit(1)
	}
}

func run(args []string, logger *supportlog.Entry) error {
	fs := flag.NewFlagSet("gravitybeam", flag.ExitOnError)

	portHTTP := "0"
	portP2P := "0"
	horizonURL := "https://horizon-testnet.stellar.org"
	dbPath := "gravitybeam.db"
	peers := ""

	fs.StringVar(&portHTTP, "port-http", portHTTP, "Port to accept HTTP requests on (also via PORT_HTTP)")
	fs.StringVar(&portP2P, "port-p2p", portP2P, "Port to accept P2P requests on (also via PORT_P2P)")
	fs.StringVar(&horizonURL, "horizon", horizonURL, "Horizon URL (also via HORIZON_URL)")
	fs.StringVar(&dbPath, "db", dbPath, "File path to the db to write to and read from (also via DB_PATH)")
	fs.StringVar(&peers, "peers", peers, "Comma-separated list of addresses of peers to connect to on start (also via PEERS)")

	err := ff.Parse(fs, args, ff.WithEnvVarNoPrefix())
	if err != nil {
		return err
	}

	logger.Info("Starting...")

	horizonClient := &horizonclient.Client{HorizonURL: horizonURL}
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return err
	}
	defer db.Close()

	networkDetails, err := horizonClient.Root()
	if err != nil {
		return err
	}

	host, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/" + portP2P))
	if err != nil {
		return err
	}
	hostAddrInfo := peer.AddrInfo{
		ID:    host.ID(),
		Addrs: host.Addrs(),
	}
	hostAddrs, err := peer.AddrInfoToP2pAddrs(&hostAddrInfo)
	if err != nil {
		return err
	}
	for _, a := range hostAddrs {
		logger.Infof("Listening for p2p on... %v", a)
	}

	if peers != "" {
		peersArr := strings.Split(peers, ",")
		for _, p := range peersArr {
			p := p
			go func() {
				logger := logger.WithField("peer", p)
				logger.Info("Connecting to peer...")
				peerAddrInfo, err := peer.AddrInfoFromString(p)
				if err != nil {
					logger.Errorf("Error parsing peer address: %v", err)
					return
				}
				err = host.Connect(context.Background(), *peerAddrInfo)
				if err != nil {
					logger.Errorf("Error connecting to peer: %v", err)
					return
				}
				logger.Info("Connected to peer")
			}()
		}
	}

	logger.Info("Starting mdns service...")
	mdnsService := mdns.NewMdnsService(host, "gravitybeam", &mdnsNotifee{Host: host, Logger: logger})
	err = mdnsService.Start()
	if err != nil {
		return err
	}

	pubSub, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		return err
	}
	logger.Info("Listening for p2p transactions...")
	topic, err := pubSub.Join("txs")
	if err != nil {
		return err
	}
	collector := TransactionCollector{
		NetworkPassphrase: networkDetails.NetworkPassphrase,
		Logger:            logger,
		DB:                db,
		HorizonClient:     horizonClient,
		Topic:             topic,
	}
	go collector.Collect()

	r := supporthttp.NewAPIMux(logger)
	r.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{Logger: logger, NoColor: false}))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})

	r.MethodFunc("GET", "/", func(w http.ResponseWriter, r *http.Request) {
		render.PlainText(w, r, `
░██████╗░██████╗░░█████╗░██╗░░░██╗██╗████████╗██╗░░░██╗░░░██████╗░███████╗░█████╗░███╗░░░███╗
██╔════╝░██╔══██╗██╔══██╗██║░░░██║██║╚══██╔══╝╚██╗░██╔╝░░░██╔══██╗██╔════╝██╔══██╗████╗░████║
██║░░██╗░██████╔╝███████║╚██╗░██╔╝██║░░░██║░░░░╚████╔╝░░░░██████╦╝█████╗░░███████║██╔████╔██║
██║░░╚██╗██╔══██╗██╔══██║░╚████╔╝░██║░░░██║░░░░░╚██╔╝░░░░░██╔══██╗██╔══╝░░██╔══██║██║╚██╔╝██║
╚██████╔╝██║░░██║██║░░██║░░╚██╔╝░░██║░░░██║░░░░░░██║░░░░░░██████╦╝███████╗██║░░██║██║░╚═╝░██║
░╚═════╝░╚═╝░░╚═╝╚═╝░░╚═╝░░░╚═╝░░░╚═╝░░░╚═╝░░░░░░╚═╝░░░░░░╚═════╝░╚══════╝╚═╝░░╚═╝╚═╝░░░░░╚═╝
`)
	})

	r.Method("POST", "/tx", &TransactionHandler{
		NetworkPassphrase: networkDetails.NetworkPassphrase,
		HorizonClient:     horizonClient,
		Logger:            logger,
		DB:                db,
		Topic:             topic,
	})

	listener, err := net.Listen("tcp", ":"+portHTTP)
	if err != nil {
		return err
	}

	server := http.Server{
		Handler:     r,
		ReadTimeout: 10 * time.Second,
	}
	logger.Infof("Listening for http on %s...", listener.Addr())
	err = server.Serve(listener)
	if err != nil {
		return err
	}

	return nil
}

type mdnsNotifee struct {
	Host   host.Host
	Logger *supportlog.Entry
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.Host.ID() {
		return
	}
	n.Logger.Infof("Connecting to peer discovered via mdns: %s", pi.ID.Pretty())
	err := n.Host.Connect(context.Background(), pi)
	if err != nil {
		n.Logger.WithStack(err).Error(fmt.Errorf("Error connecting to peer %s: %w", pi.ID.Pretty(), err))
	}
}
