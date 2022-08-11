package main

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"regexp"
	"strconv"
	"time"

	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"

	"yakle/metrics"
)

type config struct {
	brokers  string
	laddr    string
	mpath    string
	clabel   string
	tfilter  string
	interval int
	debug    bool
}

var (
	version      string
	build        string
	leaderMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_topic_partition_leader",
			Help: "Leader Broker ID for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	leaderIsPreferredMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_leader_is_preferred",
			Help: "Boolean indicating if the leader use its preferred broker for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	replicasMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_replicas",
			Help: "Number of replicas for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	replicasInsyncMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_isr",
			Help: "Number of in-sync replicas for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	underReplicatedMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_under_replicated",
			Help: "Boolean indicating if all replicas are in sync for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	oldestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_oldest_offset",
			Help: "Oldest available offset for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	newestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_newest_offset",
			Help: "Last committed offset for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	oldestTimeMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_oldest_time",
			Help: "Time of the oldest available offset for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	currentGroupOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_group_topic_partition_current_offset",
			Help: "Current offset for a given group/topic/partition",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)
	offsetGroupLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_group_topic_partition_offset_lag",
			Help: "Offset lag for a given group/topic/partition",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)
	timeGroupLagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_group_topic_partition_time_lag",
			Help: "Time lag for a given group/topic/partition",
		},
		[]string{"cluster", "group", "topic", "partition"},
	)
)

func main() {
	var conf config
	flag.StringVar(&conf.brokers, "kafka.brokers", "localhost:9092", "address list (host:port) of kafka brokers to connect to")
	flag.StringVar(&conf.laddr, "web.listen-address", ":8080", "address (host:port) to listen on for telemetry")
	flag.StringVar(&conf.mpath, "web.telemetry-path", "/metrics", "path under which to expose metrics")
	flag.StringVar(&conf.clabel, "kafka.label", "kafka-cluster", "kafka cluster name for labeling metrics")
	flag.StringVar(&conf.tfilter, "topic.filter", "^__.*", "regex for excluding topics")
	flag.IntVar(&conf.interval, "refresh.interval", 30, "interval for refreshing metrics")
	flag.BoolVar(&conf.debug, "log.enable-sarama", false, "enable sarama debug logging")
	flag.Parse()

	if conf.debug {
		log.SetLevel(log.DebugLevel)
		metrics.Debug = true
	}

	prometheus.MustRegister(leaderMetric)
	prometheus.MustRegister(leaderIsPreferredMetric)
	prometheus.MustRegister(replicasMetric)
	prometheus.MustRegister(replicasInsyncMetric)
	prometheus.MustRegister(oldestOffsetMetric)
	prometheus.MustRegister(newestOffsetMetric)
	prometheus.MustRegister(oldestTimeMetric)
	prometheus.MustRegister(currentGroupOffsetMetric)
	prometheus.MustRegister(offsetGroupLagMetric)
	prometheus.MustRegister(timeGroupLagMetric)

	clabel := conf.clabel
	tfilter := regexp.MustCompile(conf.tfilter)

	go func() {
		ticker := time.NewTicker(time.Duration(conf.interval) * time.Second)
		for range ticker.C {
			log.Infof("getMetrics fired")

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
					log.Debugf("getTopicMetrics() topic: %s, part: %d, leader: %d, isp: %d, replicas: %d, isr: %d, oldest: %d, newest: %d",
						topic, p, tm.Leader, tm.LeaderISP, tm.Replicas, tm.InSyncReplicas, tm.Oldest, tm.Newest)
					leaderMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.Leader))
					leaderIsPreferredMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.LeaderISP))
					replicasMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.Replicas))
					replicasInsyncMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.InSyncReplicas))
					oldestOffsetMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.Oldest))
					newestOffsetMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.Newest))
					oldestTimeMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(p))).Set(float64(tm.OldestTime / time.Millisecond))
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
						currentGroupOffsetMetric.WithLabelValues(clabel, group, topic, strconv.Itoa(int(p))).Set(float64(gm.Current))
						offsetGroupLagMetric.WithLabelValues(clabel, group, topic, strconv.Itoa(int(p))).Set(float64(gm.OffsetLag))
						timeGroupLagMetric.WithLabelValues(clabel, group, topic, strconv.Itoa(int(p))).Set(float64(gm.TimeLag / time.Millisecond))
					}
				}
			}
			log.Infof("getMetrics ended")
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
