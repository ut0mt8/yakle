package main

import (
	"fmt"
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/ut0mt8/kalag/lag"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"
)

type Config struct {
	laddr    string
	mpath    string
	brokers  string
	topic    string
	group    string
	interval int
}

var (
	oldestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_oldest_offset",
			Help: "Oldest avalaible offset of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	newestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_newest_offset",
			Help: "Last commited offset of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	currentGroupOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_group_topic_partition_current_offset",
			Help: "Current offset of a given group/topic/partition",
		},
		[]string{"group", "topic", "partition"},
	)
	offsetGroupLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_group_topic_partition_offset_lag",
			Help: "Offset lag of a given group/topic/partition",
		},
		[]string{"group", "topic", "partition"},
	)
	timeGroupLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_group_topic_partition_time_lag",
			Help: "Time lag of a given group/topic/partition",
		},
		[]string{"group", "topic", "partition"},
	)
)

var config Config
var log = logrus.New()

func init() {
	flag.StringVar(&config.laddr, "listen-address", ":8080", "host:port to listen on")
	flag.StringVar(&config.mpath, "metric-path", "/metrics", "path exposing metrics")
	flag.StringVar(&config.brokers, "brokers", "localhost:9092", "brokers to connect on")
	flag.StringVar(&config.topic, "topic", "", "topic to check")
	flag.StringVar(&config.group, "group", "", "group to check")
	flag.IntVar(&config.interval, "interval", 30, "interval of lag refresh")
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.InfoLevel
	prometheus.MustRegister(oldestOffsetMetric)
	prometheus.MustRegister(newestOffsetMetric)
	prometheus.MustRegister(currentGroupOffsetMetric)
	prometheus.MustRegister(offsetGroupLagMetric)
	prometheus.MustRegister(timeGroupLagMetric)
}

func main() {

	flag.Parse()
	if config.topic == "" || config.group == "" {
		fmt.Println("-topic and -group options are required")
		os.Exit(2)
	}

	go func() {
		ticker := time.NewTicker(time.Duration(config.interval) * time.Second)
		for range ticker.C {
			log.Infof("getLag started for topic: %s, group: %s\n", config.topic, config.group)
			ofs, err := lag.GetLag(config.brokers, config.topic, config.group)
			if err != nil {
				log.Errorf("getLag failed : %v", err)
				continue
			}
			log.Infof("getLag ended for topic: %s, group: %s\n", config.topic, config.group)
			for p, of := range ofs {
				log.Debugf("getLag partition: %d, newest: %d, current: %d, offsetlag: %d, timelag: %v\n", p, of.Newest, of.Current, of.OffsetLag, of.TimeLag)
				oldestOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p))).Set(float64(of.Oldest))
				newestOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p))).Set(float64(of.Newest))
				currentGroupOffsetMetric.WithLabelValues(config.group, config.topic, strconv.Itoa(int(p))).Set(float64(of.Current))
				offsetGroupLagMetric.WithLabelValues(config.group, config.topic, strconv.Itoa(int(p))).Set(float64(of.OffsetLag))
				timeGroupLagMetric.WithLabelValues(config.group, config.topic, strconv.Itoa(int(p))).Set(float64(of.TimeLag / time.Millisecond))
			}
		}
	}()

	http.Handle(config.mpath, promhttp.Handler())
	http.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	log.Infof("beginning to serve on %s, metrics on %s", config.laddr, config.mpath)
	http.ListenAndServe(config.laddr, nil)
}
