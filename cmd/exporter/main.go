package main

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/procutil"
	"github.com/VictoriaMetrics/cybera-exporter/pkg/scraper"
	"github.com/VictoriaMetrics/cybera-exporter/pkg/sender"
)

var (
	httpListenAddr = flag.String("httpListenAddr", ":8436", "TCP address to listen for http connections.")
)

func main() {
	flag.Parse()
	buildinfo.Init()
	logger.Infof("starting exporter")
	ctx, cancel := context.WithCancel(context.Background())

	cyberaURL := *scraper.URL
	if cyberaURL == "" {
		logger.Fatalf("cyberaURL flag cannot be empty, define it with flag -cybera.url=http://localhost:8013")
	}
	vmURL := *sender.URL
	if vmURL == "" {
		logger.Fatalf("victoria-metrics url cannot be empty, define it with flag -vm.url=http://localhost:8428")
	}
	var wg sync.WaitGroup
	syncChan := make(chan []string, 50)
	vmClient := sender.NewVMClient(vmURL, &wg)
	cyberaClient, err := scraper.NewCyberaClient(cyberaURL, &wg)
	if err != nil {
		logger.Fatalf("cannot create cybera client: %v", err)
	}
	wg.Add(1)
	if err := cyberaClient.StartScraper(ctx, syncChan); err != nil {
		logger.Fatalf("unexpected error recevied from cybera: %v", err)
	}
	wg.Add(1)
	if err := vmClient.StartSender(ctx, nil, syncChan); err != nil {
		logger.Fatalf("unexpected error received from victoria-metrics: %v", err)
	}

	go httpserver.Serve(*httpListenAddr, func(w http.ResponseWriter, r *http.Request) bool {
		return requestHandler(w, r, vmClient)
	})

	procutil.WaitForSigterm()
	t := time.Now()
	cancel()
	logger.Infof("waiting for clients to stop")
	wg.Wait()
	logger.Infof("sucessufully stoped exporter after: %.3f seconds", time.Since(t).Seconds())
}

func requestHandler(w http.ResponseWriter, r *http.Request, vmClient *sender.VMClient) bool {
	path := strings.Replace(r.URL.Path, "//", "/", -1)
	switch path {
	case "/api/metrics":
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "text/plain")
		vmClient.WriteBufferedItems(w)
		return true
	}
	return false
}
