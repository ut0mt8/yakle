package main

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"regexp"
	"sync"
	"time"

	"github.com/namsral/flag"

	"github.com/VictoriaMetrics/metrics"
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
	version string
	build   string
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

			// kafka_cluster_info{"cluster", "broker_count", "controller_id", "topic_count", "group_count"}
			clusterInfoMetric := fmt.Sprintf(`kafka_cluster_info{cluster="%s", broker_count="%d", controller_id="%d", topic_count="%d", group_count="%d"}`,
				clabel, cmetric.BrokerCount, cmetric.CtrlID, len(topics), len(groups))
			metrics.GetOrCreateGauge(clusterInfoMetric, nil).Set(1)

			bmetrics, err := kafka.GetBrokerMetrics(admin, cmetric.CtrlID)
			if err != nil {
				log.Error().Err(err).Msg("getBrokerMetrics() failed")
				continue
			}

			// kafka_broker_info{"cluster", "broker_id", "address", "is_controller", "rack_id"}
			for _, bmetric := range bmetrics {
				brokerInfoMetric := fmt.Sprintf(`kafka_broker_info{cluster="%s", broker_id="%d", address="%s", is_controller="%d", rack_id="%s"}`,
					clabel, bmetric.BrokerID, bmetric.Address, bmetric.IsCtrl, bmetric.RackID)
				metrics.GetOrCreateGauge(brokerInfoMetric, nil).Set(1)
			}

			lgmetrics, err := kafka.GetLogDirMetrics(admin)
			if err != nil {
				log.Error().Err(err).Msg("getLogDirMetrics() failed")
				continue
			}

			// kafka_topic_broker_logdir_size{"cluster", "topic", "broker", "path"}
			for brkid, lgbms := range lgmetrics {
				for topic, lgm := range lgbms {
					logDirMetric := fmt.Sprintf(`kafka_topic_broker_logdir_size{cluster="%s", topic="%s", broker="%d", path="%s"}`,
						clabel, topic, brkid, lgm.Path)
					metrics.GetOrCreateGauge(logDirMetric, nil).Set(float64(lgm.Size))
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

				// kafka_topic_info{"cluster", "topic", "partition_count", "replication_factor"}
				topicInfoMetric := fmt.Sprintf(`kafka_topic_info{cluster="%s", topic="%s", partition_count="%d", replication_factor="%d"}`,
					clabel, tname, topic.NumPartitions, topic.ReplicationFactor)
				metrics.GetOrCreateGauge(topicInfoMetric, nil).Set(1)

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

						// kafka_topic_partition_info{"cluster", "topic", "partition", "leader", "replicas", "insync_replicas"}
						topicPartitionInfoMetric := fmt.Sprintf(`kafka_topic_partition_info{cluster="%s", topic="%s", partition="%d", leader="%d", replicas="%d", insync_replicas="%d"}`,
							clabel, topic, part, tpm.Leader, tpm.Replicas, tpm.InSyncReplicas)
						metrics.GetOrCreateGauge(topicPartitionInfoMetric, nil).Set(1)

						// kafka_topic_partition_not_preferred{"cluster", "topic", "partition"}
						notPreferredMetric := fmt.Sprintf(`kafka_topic_partition_not_preferred{cluster="%s", topic="%s", partition="%d"}`,
							clabel, topic, part)
						metrics.GetOrCreateGauge(notPreferredMetric, nil).Set(float64(tpm.LeaderNP))

						// kafka_topic_partition_under_replicated{"cluster", "topic", "partition"}
						underReplicatedMetric := fmt.Sprintf(`kafka_topic_partition_under_replicated{cluster="%s", topic="%s", partition="%d"}`,
							clabel, topic, part)
						metrics.GetOrCreateGauge(underReplicatedMetric, nil).Set(float64(tpm.UnderReplicated))

						// kafka_topic_partition_oldest_offset{"cluster", "topic", "partition"}
						oldestOffsetMetric := fmt.Sprintf(`kafka_topic_partition_oldest_offset{cluster="%s", topic="%s", partition="%d"}`,
							clabel, topic, part)
						metrics.GetOrCreateGauge(oldestOffsetMetric, nil).Set(float64(tpm.Oldest))

						// kafka_topic_partition_newest_offset{"cluster", "topic", "partition"}
						newestOffsetMetric := fmt.Sprintf(`kafka_topic_partition_newest_offset{cluster="%s", topic="%s", partition="%d"}`,
							clabel, topic, part)
						metrics.GetOrCreateGauge(newestOffsetMetric, nil).Set(float64(tpm.Newest))

						// kafka_topic_partition_oldest_time{"cluster", "topic", "partition"}
						oldestTimeMetric := fmt.Sprintf(`kafka_topic_partition_oldest_time{cluster="%s", topic="%s", partition="%d"}`,
							clabel, topic, part)
						metrics.GetOrCreateGauge(oldestTimeMetric, nil).Set(float64(tpm.OldestTime / time.Millisecond))
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

				// kafka_group_info{"cluster", "group", "state", "member_count"}
				groupInfoMetric := fmt.Sprintf(`kafka_group_info{cluster="%s", group="%s", state="%s", member_count="%d"}`,
					clabel, group.GroupId, group.State, len(group.Members))
				metrics.GetOrCreateGauge(groupInfoMetric, nil).Set(1)

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

							// kafka_group_topic_partition_current_offset{"cluster", "group", "topic", "partition"}
							currentGroupOffsetMetric := fmt.Sprintf(`kafka_group_topic_partition_current_offset{cluster="%s", group="%s", topic="%s", partition="%d"}`,
								clabel, group, ctopic, part)
							metrics.GetOrCreateGauge(currentGroupOffsetMetric, nil).Set(float64(gpm.Current))

							// kafka_group_topic_partition_offset_lag{"cluster", "group", "topic", "partition"}
							offsetGroupLagMetric := fmt.Sprintf(`kafka_group_topic_partition_offset_lag{cluster="%s", group="%s", topic="%s", partition="%d"}`,
								clabel, group, ctopic, part)
							metrics.GetOrCreateGauge(offsetGroupLagMetric, nil).Set(float64(gpm.OffsetLag))

							// kafka_group_topic_partition_time_lag{"cluster", "group", "topic", "partition"}
							timeGroupLagMetric := fmt.Sprintf(`kafka_group_topic_partition_time_lag{cluster="%s", group="%s", topic="%s", partition="%d"}`,
								clabel, group, ctopic, part)
							metrics.GetOrCreateGauge(timeGroupLagMetric, nil).Set(float64(gpm.TimeLag / time.Millisecond))
						}
					}
				}(group.GroupId)
			}

			gcwrk.WaitAllDone()

			client.Close()
			log.Info().Msg("getMetrics ended")
		}
	}()

	//http.Handle(conf.mpath, promhttp.Handler())
	http.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		metrics.WritePrometheus(w, true)
	})
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
