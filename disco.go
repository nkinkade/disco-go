package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/m-lab/go/rtx"
	"github.com/nkinkade/disco-go/config"
	"github.com/nkinkade/disco-go/metrics"
	"github.com/nkinkade/disco-go/snmp"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/soniah/gosnmp"
)

var (
	community           = os.Getenv("DISCO_COMMUNITY")
	fListenAddress      = flag.String("listen-address", ":8888", "Address to listen on for telemetry.")
	fMetricsFile        = flag.String("metrics", "", "Path to YAML file defining metrics to scrape.")
	fWriteInterval      = flag.Uint64("write-interval", 300, "Interval in seconds to write out JSON files.")
	fTarget             = flag.String("target", "", "Switch FQDN to scrape metrics from.")
	logFatal            = log.Fatal
	mainCtx, mainCancel = context.WithCancel(context.Background())
)

func main() {
	flag.Parse()

	if len(community) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_COMMUNITY")
	}

	goSNMP := &gosnmp.GoSNMP{
		Target:    *fTarget,
		Port:      uint16(161),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(2) * time.Second,
		Retries:   1,
	}
	err := goSNMP.Connect()
	rtx.Must(err, "Failed to connect to the SNMP server")

	config, err := config.New(*fMetricsFile)
	rtx.Must("Could not create new metrics configuration")
	client := snmp.Client(goSNMP)
	metrics := metrics.New(client, config, *fTarget)

	// Start scraping on a clean 10s boundary within a minute.
	for time.Now().Second()%10 != 0 {
		time.Sleep(1 * time.Second)
	}

	cronCollectMetrics := gocron.NewScheduler(time.UTC)
	cronCollectMetrics.Every(10).Seconds().StartImmediately().Do(metrics.Collect, client, config)
	cronCollectMetrics.StartAsync()

	cronWriteMetrics := gocron.NewScheduler(time.UTC)
	cronWriteMetrics.Every(*fWriteInterval).Seconds().Do(metrics.Write, *fWriteInterval)
	cronWriteMetrics.StartAsync()

	http.Handle("/metrics", promhttp.Handler())

	srv := http.Server{
		Addr:    *fListenAddress,
		Handler: http.DefaultServeMux,
	}

	fmt.Printf("Listening on port %v.\n", *fListenAddress)

	// When the context is canceled, stop serving.
	go func() {
		<-mainCtx.Done()
		goSNMP.Conn.Close()
		srv.Close()
	}()

	// Listen forever, or until the context is closed.
	logFatal(srv.ListenAndServe())
}
