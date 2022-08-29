package main

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	"github.com/zenthangplus/goccm"
	"yakle/internal/kafka"
)

type config struct {
	brokers  string
	laddr    string
	mpath    string
	clabel   string
	tfilter  string
	gfilter  string
	interval int
	workers  int
	ts       bool
	debug    bool
}

var (
	version           string
	build             string
	clusterInfoMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_cluster_info",
			Help: "Informations for the cluster",
		},
		[]string{"cluster", "broker_count", "controller_id", "topic_count", "group_count"},
	)
	brokerInfoMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_broker_info",
			Help: "Informations for a given broker",
		},
		[]string{"cluster", "broker_id", "address", "is_controller", "rack_id"},
	)
	topicInfoMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_info",
			Help: "Informations for a given topic",
		},
		[]string{"cluster", "topic", "partition_count", "replication_factor"},
	)
	logDirMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_broker_logdir_size",
			Help: "Logdir size for a given topic/broker",
		},
		[]string{"cluster", "topic", "broker", "path"},
	)
	topicPartitionInfoMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_info",
			Help: "Informations for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition", "leader", "replicas", "insync_replicas"},
	)
	notPreferredMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_not_preferred",
			Help: "Boolean indicating if the leader don't use its preferred broker for a given topic/partition",
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
			Help: "Oldest available offset (low watermark) for a given topic/partition",
		},
		[]string{"cluster", "topic", "partition"},
	)
	newestOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_topic_partition_newest_offset",
			Help: "Last committed offset (high watermark) for a given topic/partition",
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
	groupInfoMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_group_info",
			Help: "Informations for a given group",
		},
		[]string{"cluster", "group", "state", "member_count"},
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

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	flag.StringVar(&conf.brokers, "kafka-brokers", "localhost:9092", "address list of kafka brokers to connect to")
	flag.StringVar(&conf.laddr, "web-listen-address", ":8080", "address (host:port) to listen on for telemetry")
	flag.StringVar(&conf.mpath, "web-telemetry-path", "/metrics", "path under which to expose metrics")
	flag.StringVar(&conf.clabel, "kafka-label", "kafka-cluster", "kafka cluster name for labeling metrics")
	flag.StringVar(&conf.tfilter, "topic-filter", "^__.*", "regex for excluding topics, default to internal topics")
	flag.StringVar(&conf.gfilter, "group-filter", "^__.*", "regex for excluding groups, default to internal groups")
	flag.IntVar(&conf.interval, "refresh-interval", 30, "interval for refreshing metrics")
	flag.IntVar(&conf.workers, "kafka-workers", 10, "number of parallel workers for fetching metrics")
	flag.BoolVar(&conf.ts, "kafka-fetch-timestamp", false, "enable timestamps calculation")
	flag.BoolVar(&conf.debug, "log-debug", false, "enable debug and sarama logging")
	flag.Parse()

	if conf.debug {
		kafka.Debug = true
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	prometheus.MustRegister(clusterInfoMetric)
	prometheus.MustRegister(brokerInfoMetric)
	prometheus.MustRegister(topicInfoMetric)
	prometheus.MustRegister(logDirMetric)
	prometheus.MustRegister(topicPartitionInfoMetric)
	prometheus.MustRegister(notPreferredMetric)
	prometheus.MustRegister(underReplicatedMetric)
	prometheus.MustRegister(oldestOffsetMetric)
	prometheus.MustRegister(newestOffsetMetric)
	prometheus.MustRegister(oldestTimeMetric)
	prometheus.MustRegister(groupInfoMetric)
	prometheus.MustRegister(currentGroupOffsetMetric)
	prometheus.MustRegister(offsetGroupLagMetric)
	prometheus.MustRegister(timeGroupLagMetric)

	timestamp := conf.ts
	clabel := conf.clabel
	tfilter := regexp.MustCompile(conf.tfilter)
	gfilter := regexp.MustCompile(conf.gfilter)

	go func() {
		ticker := time.NewTicker(time.Duration(conf.interval) * time.Second)
		for range ticker.C {
			log.Info().Msg("getMetrics fired")

			admin, err := kafka.AdminConnect(conf.brokers)
			if err != nil {
				log.Error().Err(err).Msg("AdminConnect failed")
				continue
			}

			topics, err := kafka.GetTopics(admin)
			if err != nil {
				log.Error().Err(err).Msg("getTopics() failed")
				continue
			}

			groups, err := kafka.GetGroups(admin)
			if err != nil {
				log.Error().Err(err).Msg("getGroups() failed")
				continue
			}

			cmetric, err := kafka.GetClusterMetric(admin)
			if err != nil {
				log.Error().Err(err).Msg("getClusterMetrics() failed")
				continue
			}

			clusterInfoMetric.WithLabelValues(clabel, strconv.Itoa(cmetric.BrokerCount), strconv.Itoa(int(cmetric.CtrlID)),
				strconv.Itoa(len(topics)), strconv.Itoa(len(groups))).Set(1)

			bmetrics, err := kafka.GetBrokerMetrics(admin, cmetric.CtrlID)
			if err != nil {
				log.Error().Err(err).Msg("getBrokerMetrics() failed")
				continue
			}

			for _, bmetric := range bmetrics {
				brokerInfoMetric.WithLabelValues(clabel, strconv.Itoa(int(bmetric.BrokerID)), bmetric.Address,
					strconv.Itoa(bmetric.IsCtrl), bmetric.RackID).Set(1)
			}

			lgmetrics, err := kafka.GetLogDirMetrics(admin)
			if err != nil {
				log.Error().Err(err).Msg("getLogDirMetrics() failed")
				continue
			}

			for brkid, lgbms := range lgmetrics {
				for topic, lgm := range lgbms {
					logDirMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(brkid)), lgm.Path).Set(float64(lgm.Size))
				}
			}

			admin.Close()

			client, err := kafka.ClientConnect(conf.brokers)
			if err != nil {
				log.Error().Err(err).Msg("ClientConnect failed")
				continue
			}

			atms := make(map[string]map[int32]kafka.TopicMetric)
			mutex := &sync.Mutex{}

			tworkers := conf.workers
			if len(topics) < conf.workers {
				tworkers = len(topics)
			}

			tcwrk := goccm.New(tworkers)

			for tname, topic := range topics {
				if tfilter.MatchString(tname) {
					log.Debug().Msgf("skip topic: %s", tname)
					continue
				}

				topicInfoMetric.WithLabelValues(clabel, tname, strconv.Itoa(int(topic.NumPartitions)),
					strconv.Itoa(int(topic.ReplicationFactor))).Set(1)

				tcwrk.Wait()

				go func(topic string) {
					defer tcwrk.Done()
					log.Debug().Msgf("getTopicMetrics() started for topic: %s", topic)

					tpms, err := kafka.GetTopicMetrics(client, topic, timestamp)
					if err != nil {
						log.Error().Err(err).Msg("getTopicMetrics() failed")
						return
					}

					log.Debug().Msgf("getTopicMetrics() ended for topic: %s", topic)

					for part, tpm := range tpms {
						log.Debug().Msgf(
							"getTopicMetrics() topic: %s, part: %d, leader: %d, np: %d, replicas: %d, isr: %d, oldest: %d, newest: %d",
							topic, part, tpm.Leader, tpm.LeaderNP, tpm.Replicas, tpm.InSyncReplicas, tpm.Oldest, tpm.Newest)

						topicPartitionInfoMetric.WithLabelValues(clabel, topic, strconv.Itoa(int(part)),
							strconv.Itoa(int(tpm.Leader)), strconv.Itoa(tpm.Replicas), strconv.Itoa(tpm.InSyncReplicas)).Set(1)
						notPreferredMetric.WithLabelValues(clabel, topic,
							strconv.Itoa(int(part))).Set(float64(tpm.LeaderNP))
						underReplicatedMetric.WithLabelValues(clabel, topic,
							strconv.Itoa(int(part))).Set(float64(tpm.UnderReplicated))
						oldestOffsetMetric.WithLabelValues(clabel, topic,
							strconv.Itoa(int(part))).Set(float64(tpm.Oldest))
						newestOffsetMetric.WithLabelValues(clabel, topic,
							strconv.Itoa(int(part))).Set(float64(tpm.Newest))
						oldestTimeMetric.WithLabelValues(clabel, topic,
							strconv.Itoa(int(part))).Set(float64(tpm.OldestTime / time.Millisecond))
					}

					mutex.Lock()
					atms[topic] = tpms
					mutex.Unlock()
				}(tname)
			}

			tcwrk.WaitAllDone()

			gworkers := conf.workers
			if len(groups) < conf.workers {
				gworkers = len(topics)
			}

			gcwrk := goccm.New(gworkers)

			for _, group := range groups {
				if gfilter.MatchString(group.GroupId) {
					log.Debug().Msgf("skip group: %s", group.GroupId)
					continue
				}

				groupInfoMetric.WithLabelValues(clabel, group.GroupId, group.State, strconv.Itoa(len(group.Members))).Set(1)

				gcwrk.Wait()

				go func(group string) {
					defer gcwrk.Done()

					ctopics, err := kafka.GetTopicsConsummed(client, topics, group)
					if err != nil {
						log.Error().Err(err).Msg("getTopicsConsummed() failed")
						return
					}

					for ctopic := range ctopics {
						log.Debug().Msgf("getGroupMetrics() started for group %s, topic: %s", group, ctopic)

						gpms, err := kafka.GetGroupMetrics(client, ctopic, group, atms[ctopic], timestamp)
						if err != nil {
							log.Error().Err(err).Msg("getGroupMetrics() failed")
							continue
						}

						log.Debug().Msgf("getGroupMetrics() ended for topic: %s, group: %s", ctopic, group)

						for part, gpm := range gpms {
							log.Debug().Msgf(
								"getGroupMetrics() topic: %s, group: %s, part: %d, current: %d, olag: %d",
								ctopic, group, part, gpm.Current, gpm.OffsetLag)

							currentGroupOffsetMetric.WithLabelValues(clabel, group, ctopic,
								strconv.Itoa(int(part))).Set(float64(gpm.Current))
							offsetGroupLagMetric.WithLabelValues(clabel, group, ctopic,
								strconv.Itoa(int(part))).Set(float64(gpm.OffsetLag))
							timeGroupLagMetric.WithLabelValues(clabel, group, ctopic,
								strconv.Itoa(int(part))).Set(float64(gpm.TimeLag / time.Millisecond))
						}
					}
				}(group.GroupId)
			}

			gcwrk.WaitAllDone()
			client.Close()
			log.Info().Msg("getMetrics ended")
		}
	}()

	http.Handle(conf.mpath, promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "OK")
	})
	http.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))

	log.Info().Msgf("yalke version=%s build=%s starting", version, build)
	log.Info().Msgf("beginning to serve on %s, metrics on %s", conf.laddr, conf.mpath)

	if err := http.ListenAndServe(conf.laddr, nil); err != nil {
		log.Fatal().Err(err).Msg("http startup failed")
	}
}
