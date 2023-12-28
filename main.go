package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/namsral/flag"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type config struct {
	brokers  string
	laddr    string
	mpath    string
	clabel   string
	tfilter  string
	gfilter  string
	interval int
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
	flag.BoolVar(&conf.ts, "kafka-fetch-timestamp", false, "enable timestamps calculation")
	flag.BoolVar(&conf.debug, "log-debug", false, "enable debug logging")
	flag.Parse()

	if conf.debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	ctx := context.Background()
	//timestamp := conf.ts
	clabel := conf.clabel
	tfilter := regexp.MustCompile(conf.tfilter)
	//gfilter := regexp.MustCompile(conf.gfilter)

	go func() {
		ticker := time.NewTicker(time.Duration(conf.interval) * time.Second)
		for range ticker.C {
			start := time.Now()
			log.Info().Msg("getMetrics fired")

			brokers := strings.Split(conf.brokers, ",")
			kclient, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
			if err != nil {
				log.Error().Err(err).Msg("NewClient() failed")
				continue
			}
			defer kclient.Close()

			kadmin := kadm.NewClient(kclient)

			brkm, err := kadmin.BrokerMetadata(ctx)
			if err != nil {
				log.Error().Err(err).Msg("BrokerMetadata() failed")
				continue
			}

			// kafka_cluster_info{"cluster", "broker_count", "controller_id", "topic_count", "group_count"}
			clusterInfoMetric := fmt.Sprintf(`kafka_cluster_info{cluster="%s", broker_count="%d", controller_id="%d", topic_count="%d", group_count="%d"}`,
				clabel, len(brkm.Brokers), brkm.Controller, -1, -1) // TODO topic /group count
			metrics.GetOrCreateGauge(clusterInfoMetric, nil).Set(1)

			// kafka_broker_info{"cluster", "broker_id", "address", "is_controller", "rack_id"}
			for _, brkd := range brkm.Brokers {
				isCtrl := 0
				if brkd.NodeID == brkm.Controller {
					isCtrl = 1
				}
				brokerInfoMetric := fmt.Sprintf(`kafka_broker_info{cluster="%s", broker_id="%d", address="%s", is_controller="%d", rack_id="%s"}`,
					clabel, brkd.NodeID, brkd.Host, isCtrl, *brkd.Rack)
				metrics.GetOrCreateGauge(brokerInfoMetric, nil).Set(1)
			}

			/*
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
			*/

			topics, err := kadmin.ListTopics(ctx)
			if err != nil {
				log.Error().Err(err).Msg("ListTopics() failed")
				continue
			}

			for tname, topic := range topics {
				if tfilter.MatchString(tname) {
					continue
				}

				// kafka_topic_info{"cluster", "topic", "partition_count", "replication_factor"}
				// topicInfoMetric := fmt.Sprintf(`kafka_topic_info{cluster="%s", topic="%s", partition_count="%d", replication_factor="%d"}`,
				//	clabel, tname, topic.NumPartitions, topic.ReplicationFactor)
				// metrics.GetOrCreateGauge(topicInfoMetric, nil).Set(1)

				for _, p := range topic.Partitions.Sorted() {

					// kafka_topic_partition_info{"cluster", "topic", "partition", "leader", "replicas", "insync_replicas"}
					topicPartitionInfoMetric := fmt.Sprintf(`kafka_topic_partition_info{cluster="%s", topic="%s", partition="%d", leader="%d", replicas="%d", insync_replicas="%d"}`,
						clabel, p.Topic, p.Partition, p.Leader, len(p.Replicas), len(p.ISR))
					metrics.GetOrCreateGauge(topicPartitionInfoMetric, nil).Set(1)

					// kafka_topic_partition_not_preferred{"cluster", "topic", "partition"}
					leaderNP := 0
					if p.Replicas[0] != p.Leader {
						leaderNP = 1
					}

					notPreferredMetric := fmt.Sprintf(`kafka_topic_partition_not_preferred{cluster="%s", topic="%s", partition="%d"}`,
						clabel, p.Topic, p.Partition)
					metrics.GetOrCreateGauge(notPreferredMetric, nil).Set(float64(leaderNP))

					// kafka_topic_partition_under_replicated{"cluster", "topic", "partition"}
					underRepl := 0
					if len(p.ISR) < len(p.Replicas) {
						underRepl = 1
					}

					underReplicatedMetric := fmt.Sprintf(`kafka_topic_partition_under_replicated{cluster="%s", topic="%s", partition="%d"}`,
						clabel, p.Topic, p.Partition)
					metrics.GetOrCreateGauge(underReplicatedMetric, nil).Set(float64(underRepl))
				}
			}

			// kafka_topic_partition_oldest_offset{"cluster", "topic", "partition"}
			// oldestOffsetMetric := fmt.Sprintf(`kafka_topic_partition_oldest_offset{cluster="%s", topic="%s", partition="%d"}`,
			// 	clabel, topic, part)
			// metrics.GetOrCreateGauge(oldestOffsetMetric, nil).Set(float64(tpm.Oldest))

			// kafka_topic_partition_newest_offset{"cluster", "topic", "partition"}
			// newestOffsetMetric := fmt.Sprintf(`kafka_topic_partition_newest_offset{cluster="%s", topic="%s", partition="%d"}`,
			//	clabel, topic, part)
			// metrics.GetOrCreateGauge(newestOffsetMetric, nil).Set(float64(tpm.Newest))

			// kafka_topic_partition_oldest_time{"cluster", "topic", "partition"}
			// oldestTimeMetric := fmt.Sprintf(`kafka_topic_partition_oldest_time{cluster="%s", topic="%s", partition="%d"}`,
			//	clabel, topic, part)
			// metrics.GetOrCreateGauge(oldestTimeMetric, nil).Set(float64(tpm.OldestTime / time.Millisecond))

			/*

				for _, group := range groups {
					if gfilter.MatchString(group.GroupId) {
						log.Debug().Msgf("skip group: %s", group.GroupId)
						continue
					}

					// kafka_group_info{"cluster", "group", "state", "member_count"}
					groupInfoMetric := fmt.Sprintf(`kafka_group_info{cluster="%s", group="%s", state="%s", member_count="%d"}`,
						clabel, group.GroupId, group.State, len(group.Members))
					//metrics.GetOrCreateGauge(groupInfoMetric, nil).Set(1)
					metrics.Set(groupInfoMetric, 1)

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
							// timeGroupLagMetric := fmt.Sprintf(`kafka_group_topic_partition_time_lag{cluster="%s", group="%s", topic="%s", partition="%d"}`,
							//	clabel, group, ctopic, part)
							//metrics.GetOrCreateGauge(timeGroupLagMetric, nil).Set(float64(gpm.TimeLag / time.Millisecond))
						}
					}
				}
			*/
			elapsed := time.Since(start)
			log.Info().Str("elapsed", elapsed.String()).Msg("getMetrics ended")
		}
	}()

	http.HandleFunc(conf.mpath, func(w http.ResponseWriter, _ *http.Request) {
		metrics.WritePrometheus(w, true)
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "OK")
	})

	log.Info().Msgf("yalke version=%s build=%s starting", version, build)
	log.Info().Msgf("beginning to serve on %s, metrics on %s", conf.laddr, conf.mpath)

	if err := http.ListenAndServe(conf.laddr, nil); err != nil {
		log.Fatal().Err(err).Msg("http startup failed")
	}
}

/*

		}
	}

	var los kadm.ListedOffsets

		los, err = cadmin.ListStartOffsets(context.Background())

	for _, lo := range los {
		for _, p := range lo {
			fmt.Printf("%s %d %d\n", p.Topic, p.Partition, p.Offset)
		}
	}

		los, err = cadmin.ListEndOffsets(context.Background())


	for _, lo := range los {
		for _, p := range lo {
			fmt.Printf("%s %d %d\n", p.Topic, p.Partition, p.Offset)
		}
	}


	lgs, err := cadmin.ListGroups(context.Background())
	if err != nil {
		panic(err)
	}

	var groups []string
	for g, _ := range lgs{
		groups = append(groups, g)
	}

	lags, err :=	cadmin.Lag(context.Background(), groups...)
	if err != nil {
		panic(err)
	}

	for g, lag := range lags {
		fmt.Printf("\t%s %s \n", g, lag.State)
		for t, gl := range lag.Lag {
			for p, gml := range gl {
				fmt.Printf("\t\t%s %d %d \n", t, p, gml.Lag)
			}
		}
	}

	cadmin.Close()
}
*/
