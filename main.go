package main

import (
	"fmt"
	"github.com/Shopify/sarama"
	"github.com/namsral/flag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	brokers  string
	topic    string
	group    string
	interval int
}

type OffsetStatus struct {
	current int64
	end     int64
	lag     int64
}

var (
	currentOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_current_offset",
			Help: "Current offset of a partition for a consumer group",
		},
		[]string{"topic", "partition", "group"},
	)
	endOffsetMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_end_offset",
			Help: "Last offset of a partition",
		},
		[]string{"topic", "partition", "group"},
	)
	lagMetric = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yakle_lag",
			Help: "Lag of consumer group on a partition",
		},
		[]string{"topic", "partition", "group"},
	)
)

var config Config
var log = logrus.New()

func getLag(brokers string, topic string, group string) (map[int32]OffsetStatus, error) {

	ofs := make(map[int32]OffsetStatus)
	kcfg := sarama.NewConfig()
	kcfg.Consumer.Return.Errors = true
	kcfg.Version = sarama.V2_1_0_0

	bks := strings.Split(brokers, ",")

	client, err := sarama.NewClient(bks, kcfg)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to broker : %v", err)
	}

	topics, err := client.Topics()
	if err != nil {
		return nil, fmt.Errorf("cannot list topics : %v", err)
	}

	found := false
	for _, t := range topics {
		if t == topic {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("topic %s doesn't exist", topic)
	}

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, fmt.Errorf("cannot get partitions : %v", err)
	}

	mng, err := sarama.NewOffsetManagerFromClient(group, client)
	if err != nil {
		return nil, fmt.Errorf("cannot create offset manager : %v", err)
	}

	for _, part := range parts {
		end, err := client.GetOffset(topic, part, sarama.OffsetNewest)
		if err != nil {
			log.Errorf("cannot get partition offset : %v", err)
		}
		pmng, err := mng.ManagePartition(topic, part)
		if err != nil {
			log.Errorf("cannot create partition manager : %v", err)
		}
		cur, _ := pmng.NextOffset()
		if err != nil {
			log.Errorf("cannot get group offset : %v", err)
		}
		if cur == -1 {
			cur = 0
		}
		ofs[part] = OffsetStatus{
			current: cur,
			end:     end,
			lag:     end - cur,
		}
	}
	mng.Close()
	client.Close()
	return ofs, nil
}

func init() {
	flag.StringVar(&config.brokers, "brokers", "localhost:9092", "brokers to connect on")
	flag.StringVar(&config.topic, "topic", "", "topic to check")
	flag.StringVar(&config.group, "group", "", "group to check")
	flag.IntVar(&config.interval, "interval", 10, "interval of lag refresh")
	log.Formatter = new(logrus.TextFormatter)
	log.Level = logrus.InfoLevel
	prometheus.MustRegister(currentOffsetMetric)
	prometheus.MustRegister(endOffsetMetric)
	prometheus.MustRegister(lagMetric)
}

func main() {

	flag.Parse()
	if config.topic == "" || config.group == "" {
		log.Fatalf("-topic and -group options are required")
	}

	go func() {
		ticker := time.NewTicker(time.Duration(config.interval) * time.Second)
		for range ticker.C {
			log.Infof("running getLag")
			ofs, err := getLag(config.brokers, config.topic, config.group)
			if err != nil {
				log.Errorf("getLag failed : %v", err)
				continue
			}
			for p, of := range ofs {
				log.Printf("- %d %d %d %d\n", p, of.end, of.current, of.lag)
                                currentOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.current))
                                endOffsetMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.end))
                                lagMetric.WithLabelValues(config.topic, strconv.Itoa(int(p)), config.group).Set(float64(of.lag))
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Info("beginning to serve on port 8080")
	http.ListenAndServe(":8080", nil)
}
