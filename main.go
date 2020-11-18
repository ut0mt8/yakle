package main

import (
	"fmt"
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/ut0mt8/yakle/metrics"
	"net/http"
	"net/http/pprof"
	"regexp"
	"strconv"
	"time"
)

type config struct {
	brokers  string
	laddr    string
	mpath    string
	filter   string
	interval int
	debug    bool
}

var (
	version      string
	build        string
	leaderMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_leader",
			Help: "Leader Broker ID of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	replicasMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_replicas",
			Help: "Number of replicas of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	replicasInsyncMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_isr",
			Help: "Number of in-sync replicas of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	oldestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_oldest_offset",
			Help: "Oldest available offset of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	newestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_newest_offset",
			Help: "Last committed offset of a given topic/partition",
		},
		[]string{"topic", "partition"},
	)
	oldestTimeMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_oldest_time",
			Help: "Time of the oldest available offset of a given topic/partition",
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

func main() {

	var conf config
	flag.StringVar(&conf.brokers, "brokers", "localhost:9092", "brokers to connect on")
	flag.StringVar(&conf.laddr, "listen-address", ":8080", "host:port to listen on")
	flag.StringVar(&conf.mpath, "metric-path", "/metrics", "path exposing metrics")
	flag.StringVar(&conf.filter, "filter", "^__.*", "regex for filtering topics")
	flag.IntVar(&conf.interval, "interval", 10, "interval of lag refresh")
	flag.BoolVar(&conf.debug, "debug", false, "enable debug logging")
	flag.Parse()

	if conf.debug {
		log.SetLevel(log.DebugLevel)
		metrics.Debug = true
	}

	prometheus.MustRegister(leaderMetric)
	prometheus.MustRegister(replicasMetric)
	prometheus.MustRegister(replicasInsyncMetric)
	prometheus.MustRegister(oldestOffsetMetric)
	prometheus.MustRegister(newestOffsetMetric)
	prometheus.MustRegister(oldestTimeMetric)
	prometheus.MustRegister(currentGroupOffsetMetric)
	prometheus.MustRegister(offsetGroupLagMetric)
	prometheus.MustRegister(timeGroupLagMetric)

	tfilter := regexp.MustCompile(conf.filter)

	go func() {
		ticker := time.NewTicker(time.Duration(conf.interval) * time.Second)
		for range ticker.C {
			topics, err := metrics.GetTopics(conf.brokers)
			if err != nil {
				log.Errorf("getTopics() failed: %v", err)
				continue
			}

			groups, err := metrics.GetGroups(conf.brokers)
			if err != nil {
				log.Errorf("getGroups() failed: %v", err)
				continue
			}

			for topic := range topics {
				if tfilter.MatchString(topic) {
					log.Debugf("skip topic: %s", topic)
					continue
				}

				log.Infof("getTopicMetrics() started for topic: %s", topic)
				tms, err := metrics.GetTopicMetrics(conf.brokers, topic)
				if err != nil {
					log.Errorf("getTopicMetrics() failed: %v", err)
					continue
				}
				log.Infof("getTopicMetrics() ended for topic: %s", topic)
				for p, tm := range tms {
					log.Debugf("getTopicMetrics() topic: %s, part: %d, leader: %d, replicas: %d, isr: %d, oldest: %d, newest: %d",
						topic, p, tm.Leader, tm.Replicas, tm.InSyncReplicas, tm.Oldest, tm.Newest)
					leaderMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.Leader))
					replicasMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.Replicas))
					replicasInsyncMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.InSyncReplicas))
					oldestOffsetMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.Oldest))
					newestOffsetMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.Newest))
					oldestTimeMetric.WithLabelValues(topic, strconv.Itoa(int(p))).Set(float64(tm.OldestTime / time.Millisecond))
				}

				for group := range groups {
					consummed, err := metrics.GetTopicConsummed(conf.brokers, topic, group)
					if !consummed || err != nil {
						log.Debugf("skip topic: %s, group: %s", topic, group)
						continue
					}
					log.Infof("getGroupMetrics() started for topic: %s, group: %s", topic, group)
					gms, err := metrics.GetGroupMetrics(conf.brokers, topic, group, tms)
					if err != nil {
						log.Errorf("getGroupMetrics() failed: %v", err)
						continue
					}
					log.Infof("getGroupMetrics() ended for topic: %s, group: %s", topic, group)
					for p, gm := range gms {
						log.Debugf("getGroupMetrics() topic: %s, group: %s, part: %d, current: %d, olag: %d",
							topic, group, p, gm.Current, gm.OffsetLag)
						currentGroupOffsetMetric.WithLabelValues(group, topic, strconv.Itoa(int(p))).Set(float64(gm.Current))
						offsetGroupLagMetric.WithLabelValues(group, topic, strconv.Itoa(int(p))).Set(float64(gm.OffsetLag))
						timeGroupLagMetric.WithLabelValues(group, topic, strconv.Itoa(int(p))).Set(float64(gm.TimeLag / time.Millisecond))
					}
				}
			}
		}
	}()

	http.Handle(conf.mpath, promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "OK")
	})
	http.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	log.Infof("yalke version=%s build=%s starting", version, build)
	log.Infof("beginning to serve on %s, metrics on %s", conf.laddr, conf.mpath)
	log.Fatal(http.ListenAndServe(conf.laddr, nil))
}
