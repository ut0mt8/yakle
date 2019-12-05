package main

import (
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/ut0mt8/kalag/lag"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"
)

type Config struct {
	brokers  string
	topic    string
	group    string
	interval int
}

var (
	currentOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_current_offset",
			Help: "Current offset of a specific consumergroup/partition",
		},
		[]string{"topic", "partition", "group"},
	)
	endOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_end_offset",
			Help: "Last committed offset of a specific consumergroup/partition",
		},
		[]string{"topic", "partition", "group"},
	)
	offsetLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_offset_lag",
			Help: "Offset lag of a specific consumergroup/partition",
		},
		[]string{"topic", "partition", "group"},
	)
	timeLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_time_lag",
			Help: "Time lag of a specific consumergroup/partition",
		},
		[]string{"topic", "partition", "group"},
	)
)

var config Config
var log = logrus.New()

func init() {
	flag.StringVar(&config.brokers, "brokers", "localhost:9092", "brokers to connect on")
	flag.StringVar(&config.topic, "topic", "", "topic to check")
	flag.StringVar(&config.group, "group", "", "group to check")
	flag.IntVar(&config.interval, "interval", 30, "interval of lag refresh")
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.InfoLevel
	prometheus.MustRegister(currentOffsetMetric)
	prometheus.MustRegister(endOffsetMetric)
	prometheus.MustRegister(offsetLagMetric)
	prometheus.MustRegister(timeLagMetric)
}

func main() {

	flag.Parse()
	if config.topic == "" || config.group == "" {
		log.Fatalf("-topic and -group options are required")
	}

	go func() {
		ticker := time.NewTicker(time.Duration(config.interval) * time.Second)
		for range ticker.C {
			ofs, err := lag.GetLag(config.brokers, config.topic, config.group)
			if err != nil {
				log.Errorf("getLag failed : %v", err)
				continue
			}
			for p, of := range ofs {
				log.Infof("get: %d %d %d %d %v\n", p, of.End, of.Current, of.OffsetLag, of.TimeLag)
				currentOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.Current))
				endOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.End))
				offsetLagMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.OffsetLag))
				timeLagMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.TimeLag/time.Millisecond))
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	log.Info("beginning to serve on port 8080")
	http.ListenAndServe(":8080", nil)
}
