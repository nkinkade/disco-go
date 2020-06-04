package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/nkinkade/disco-go/config"
	"github.com/soniah/gosnmp"
)

var (
	fListenAddress      = flag.String("web.listen-address", ":8888", "Address to listen on for telemetry.")
	fMetricsFile        = flag.String("metrics", "", "Path to YAML file defining metrics to scrape.")
	fSeriesDuration     = flag.Uint64("series-duration", 60, "Interval in seconds to write out JSON files.")
	target              = os.Getenv("DISCO_TARGET") // This can become a flag.
	community           = os.Getenv("DISCO_COMMUNITY")
	mainCtx, mainCancel = context.WithCancel(context.Background())
	logFatal            = log.Fatal
	seriesStartTime     = time.Now()
)

func main() {
	flag.Parse()

	if len(target) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_TARGET")
	}
	if len(community) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_COMMUNITY")
	}

	// This should probably be moved to main() and the connnection kept open for the life of the daemon.
	snmp := &gosnmp.GoSNMP{
		Target:    target,
		Port:      uint16(161),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Duration(2) * time.Second,
		Retries:   1,
	}
	err := snmp.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to the SNMP server: %v\n", err)
	}

	config := config.New(fMetricsFile)
	metrics := metrics.New(*snmp, *config)

	// Start scraping on a clean 10s boundary within a minute.
	for time.Now().Second()%10 != 0 {
		time.Sleep(1 * time.Second)
	}

	cronCollectMetrics := gocron.NewScheduler(time.UTC)
	cronCollectMetrics.Every(10).Seconds().StartImmediately().Do(metrics.Collect, *snmp, *config)
	cronCollectMetrics.StartAsync()

	cronWriteMetrics := gocron.NewScheduler(time.UTC)
	cronWriteMetrics.Every(fSeriesDuration).Seconds().Do(metrics.Write)
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
		snmp.Conn.Close()
		srv.Close()
	}()

	// Listen forever, or until the context is closed.
	logFatal(srv.ListenAndServe())
}
