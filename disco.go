package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/soniah/gosnmp"
)

var (
	fListenAddress      = flag.String("web.listen-address", ":8888", "Address to listen on for telemetry.")
	ifAliasOID          = "1.3.6.1.2.1.31.1.1.1.18"
	hostname, _         = os.Hostname()
	machine             = hostname[:5]
	target              = os.Getenv("DISCO_TARGET")
	community           = os.Getenv("DISCO_COMMUNITY")
	mainCtx, mainCancel = context.WithCancel(context.Background())
	logFatal            = log.Fatal
	ifDescOidStub       = "1.3.6.1.2.1.2.2.1.2.IFACE"
	metricOidStubs      = map[string]string{
		"ifHCInOctets":             "1.3.6.1.2.1.31.1.1.1.6.IFACE",
		"ifHCOutOctets":            "1.3.6.1.2.1.31.1.1.1.10.IFACE",
		"ifHCInUcastPkts":          "1.3.6.1.2.1.31.1.1.1.7.IFACE",
		"ifHCOutUcastPkts":         "1.3.6.1.2.1.31.1.1.1.11.IFACE",
		"ifInErrors":               "1.3.6.1.2.1.2.2.1.14.IFACE",
		"ifOutErrors":              "1.3.6.1.2.1.2.2.1.20.IFACE",
		"jnxCosQstatTotalDropPkts": "1.3.6.1.4.1.2636.3.15.4.1.53.IFACE.0",
	}

	// Stores the last read values for each machine iface metric
	metricMachinePrevValues = map[string]uint64{
		"ifHCInOctets":             0,
		"ifHCOutOctets":            0,
		"ifHCInUcastPkts":          0,
		"ifHCOutUcastPkts":         0,
		"ifInErrors":               0,
		"ifOutErrors":              0,
		"jnxCosQstatTotalDropPkts": 0,
	}

	// Stores the last read values for each uplink iface metric
	metricUplinkPrevValues = map[string]uint64{
		"ifHCInOctets":             0,
		"ifHCOutOctets":            0,
		"ifHCInUcastPkts":          0,
		"ifHCOutUcastPkts":         0,
		"ifInErrors":               0,
		"ifOutErrors":              0,
		"jnxCosQstatTotalDropPkts": 0,
	}

	promMetrics = map[string]*prometheus.CounterVec{
		"ifHCInOctets": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCInOctets",
				Help: "Ingress octets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCOutOctets": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCOutOctets",
				Help: "Egress octets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCInUcastPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCInUcastPkts",
				Help: "Ingress unicast packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifHCOutUcastPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifHCOutUcastPkts",
				Help: "Egress unicast packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifInErrors": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifInErrors",
				Help: "Ingress errors.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"ifOutErrors": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ifOutErrors",
				Help: "Egress errors.",
			},
			[]string{
				"node",
				"interface",
			},
		),
		"jnxCosQstatTotalDropPkts": promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jnxCosQstatTotalDropPkts",
				Help: "Dropped packets.",
			},
			[]string{
				"node",
				"interface",
			},
		),
	}
)

func collectMetrics() {
	fmt.Println("collectMetrics() called.")
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
		log.Fatalf("Connect() err: %v", err)
	}
	defer snmp.Conn.Close()

	snmpPDUs, err := snmp.BulkWalkAll(ifAliasOID)
	if err != nil {
		log.Fatalf("Walk Error: %v\n", err)
	}

	mIface, uIface := getIfaces(snmpPDUs)

	fmt.Printf("miface: %v, uIface: %v\n", mIface, uIface)

	for metric, oid := range metricOidStubs {
		// Machine interface metrics
		mOid := strings.Replace(oid, "IFACE", mIface, 1)
		mVal := getOidUint64(snmp, mOid)
		mIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", mIface, 1))
		mIncrease := mVal - metricMachinePrevValues[metric]
		fmt.Printf("Machine metric %v increase for %v: %v\n", metric, mIfDesc, mIncrease)
		promMetrics[metric].WithLabelValues(hostname, mIfDesc).Add(float64(mIncrease))
		metricMachinePrevValues[metric] = mVal

		// Uplink interface metrics
		uOid := strings.Replace(oid, "IFACE", uIface, 1)
		uVal := getOidUint64(snmp, uOid)
		uIfDesc := getOidString(snmp, strings.Replace(ifDescOidStub, "IFACE", uIface, 1))
		uIncrease := uVal - metricUplinkPrevValues[metric]
		fmt.Printf("Uplink metric %v increase for %v: %v\n", metric, uIfDesc, uIncrease)
		promMetrics[metric].WithLabelValues(hostname, uIfDesc).Add(float64(uIncrease))
		metricUplinkPrevValues[metric] = uVal
	}

}

func getOidString(snmp *gosnmp.GoSNMP, oid string) string {
	result, _ := snmp.Get([]string{oid})
	pdu := result.Variables[0]
	b := pdu.Value.([]byte)
	return string(b)
}

func getOidUint64(snmp *gosnmp.GoSNMP, oid string) uint64 {
	result, _ := snmp.Get([]string{oid})
	pdu := result.Variables[0]
	return gosnmp.ToBigInt(pdu.Value).Uint64()
}

func getIfaces(pdus []gosnmp.SnmpPDU) (string, string) {
	var machineIface string
	var uplinkIface string

	for _, pdu := range pdus {
		oidParts := strings.Split(pdu.Name, ".")
		iface := oidParts[len(oidParts)-1]

		b := pdu.Value.([]byte)
		val := strings.TrimSpace(string(b))
		if val == machine {
			machineIface = iface
		}
		if strings.HasPrefix(val, "uplink") {
			uplinkIface = iface
		}
	}
	return machineIface, uplinkIface
}

func main() {
	flag.Parse()

	if len(target) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_TARGET")
	}
	if len(community) <= 0 {
		log.Fatalf("Environment variable not set: DISCO_COMMUNITY")
	}

	cron := gocron.NewScheduler(time.UTC)
	cron.Every(10).Seconds().Do(collectMetrics)
	cron.StartAsync()

	http.Handle("/metrics", promhttp.Handler())

	fmt.Println("Set up Gocron job.")

	srv := http.Server{
		Addr:    *fListenAddress,
		Handler: http.DefaultServeMux,
	}

	fmt.Printf("Listening on port %v.\n", *fListenAddress)

	// When the context is canceled, stop serving.
	go func() {
		<-mainCtx.Done()
		srv.Close()
	}()

	// Listen forever, or until the context is closed.
	logFatal(srv.ListenAndServe())
}
