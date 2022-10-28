package kafka

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Shopify/sarama"
	"github.com/rcrowley/go-metrics"
)

type commonError struct {
	operation string
	err       error
}

type topicError struct {
	operation string
	topic     string
	err       error
}

type topicPartError struct {
	operation string
	topic     string
	partition int32
	err       error
}

type topicGroupError struct {
	operation string
	topic     string
	group     string
	err       error
}

type groupError struct {
	operation string
	group     string
	err       error
}

type topicGroupPartError struct {
	operation string
	topic     string
	group     string
	partition int32
	err       error
}

func (e *commonError) Error() string {
	return fmt.Sprintf("[%s] common %v", e.operation, e.err)
}

func (e *topicError) Error() string {
	return fmt.Sprintf("[%s](topic: %s) %v", e.operation, e.topic, e.err)
}

func (e *topicPartError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, partition: %d) %v", e.operation, e.topic, e.partition, e.err)
}

func (e *topicGroupError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, group: %s) %v", e.operation, e.topic, e.group, e.err)
}

func (e *groupError) Error() string {
	return fmt.Sprintf("[%s](group: %s) %v", e.operation, e.group, e.err)
}

func (e *topicGroupPartError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, group: %s, partition: %d) %v", e.operation, e.topic, e.group, e.partition, e.err)
}

type ClusterMetric struct {
	BrokerCount int
	CtrlID      int32
}

type BrokerMetric struct {
	Address  string
	BrokerID int32
	IsCtrl   int
	RackID   string
}

type LogDirMetric struct {
	Path string
	Size int64
}

type TopicMetric struct {
	Leader          int32
	LeaderNP        int
	Replicas        int
	InSyncReplicas  int
	UnderReplicated int
	Newest          int64
	Oldest          int64
	MsgNumber       int64
	NewestTime      time.Time
	OldestTime      time.Duration
}

type GroupMetric struct {
	Current   int64
	OffsetLag int64
	TimeLag   time.Duration
}

var Debug = false

func newKafkaConfig() *sarama.Config {
	cfg := sarama.NewConfig()
	cfg.ClientID = "yakle"
	cfg.Version = sarama.V2_1_0_0
	metrics.UseNilMetrics = true

	if Debug {
		sarama.Logger = log.New(os.Stdout, "Debug: ", log.Ltime)
	}

	return cfg
}

func AdminConnect(brokers string) (sarama.ClusterAdmin, error) {
	admin, err := sarama.NewClusterAdmin(strings.Split(brokers, ","), newKafkaConfig())
	if err != nil {
		return nil, &commonError{"AdminConnect() new-cluster-admin", err}
	}

	return admin, nil
}

func ClientConnect(brokers string) (sarama.Client, error) {
	client, err := sarama.NewClient(strings.Split(brokers, ","), newKafkaConfig())
	if err != nil {
		return nil, &commonError{"ClientConnect() new-client", err}
	}

	return client, nil
}

func GetGroupOffset(broker *sarama.Broker, topic string, partition int32, group string) (int64, error) {
	request := &sarama.OffsetFetchRequest{Version: 4, ConsumerGroup: group}
	request.AddPartition(topic, partition)

	fr, err := broker.FetchOffset(request)
	if err != nil {
		return 0, &commonError{"GetGroupOffset() fetch-offset", err}
	}

	block := fr.GetBlock(topic, partition)
	if block == nil {
		return 0, &commonError{"GetGroupOffset() get-block", err}
	}

	return block.Offset, nil
}

func GetTimestamp(broker *sarama.Broker, topic string, partition int32, offset int64) (time.Time, error) {
	request := &sarama.FetchRequest{Version: 4}
	request.AddBlock(topic, partition, offset, 1)

	fr, err := broker.Fetch(request)
	if err != nil {
		return time.Time{}, &commonError{"GetGroupOffset() fetch-offset", err}
	}

	block := fr.GetBlock(topic, partition)
	if block == nil || block.Records == nil || block.Records.RecordBatch == nil {
		return time.Time{}, &commonError{"GetGroupOffset() get-block", err}
	}

	return block.Records.RecordBatch.MaxTimestamp, nil
}

func GetTopics(admin sarama.ClusterAdmin) (map[string]sarama.TopicDetail, error) {
	topics, err := admin.ListTopics()
	if err != nil {
		return nil, &commonError{"GetTopics() list-topics", err}
	}

	return topics, nil
}

func GetGroups(admin sarama.ClusterAdmin) ([]*sarama.GroupDescription, error) {
	glist, err := admin.ListConsumerGroups()
	if err != nil {
		return nil, &commonError{"GetGroups() list-consumer-groups", err}
	}

	groups := make([]string, 0, len(glist))
	for group := range glist {
		groups = append(groups, group)
	}

	gds, err := admin.DescribeConsumerGroups(groups)
	if err != nil {
		return nil, &commonError{"GetGroups() list-consumer-groups", err}
	}

	return gds, nil
}

func GetClusterMetric(admin sarama.ClusterAdmin) (ClusterMetric, error) {
	brokers, ctrlID, err := admin.DescribeCluster()
	if err != nil {
		return ClusterMetric{}, &commonError{"GetClusterMetric() describe-cluster", err}
	}

	metric := ClusterMetric{
		BrokerCount: len(brokers),
		CtrlID:      ctrlID,
	}

	return metric, nil
}

func GetBrokerMetrics(admin sarama.ClusterAdmin, ctrlID int32) (map[int]BrokerMetric, error) {
	metrics := make(map[int]BrokerMetric)

	brokers, _, err := admin.DescribeCluster()
	if err != nil {
		return nil, &commonError{"GetBrokerMetrics() describe-cluster", err}
	}

	for brkc, broker := range brokers {
		isCtrl := 0
		if ctrlID == broker.ID() {
			isCtrl = 1
		}

		metrics[brkc] = BrokerMetric{
			Address:  broker.Addr(),
			BrokerID: broker.ID(),
			IsCtrl:   isCtrl,
			RackID:   broker.Rack(),
		}
	}

	return metrics, nil
}

func GetLogDirMetrics(admin sarama.ClusterAdmin) (map[int32]map[string]LogDirMetric, error) {
	metrics := make(map[int32]map[string]LogDirMetric)

	blist, _, err := admin.DescribeCluster()
	if err != nil {
		return nil, &commonError{"GetLogDirMetrics() describe-cluster", err}
	}

	brokers := make([]int32, 0, len(blist))
	for _, broker := range blist {
		brokers = append(brokers, broker.ID())
	}

	dldrs, err := admin.DescribeLogDirs(brokers)
	if err != nil {
		return nil, &commonError{"GetLogDirMetrics() describe-logdirs", err}
	}

	for brkid, brkr := range dldrs {
		if len(brkr) > 0 {
			logdir := brkr[0]
			metrics[brkid] = map[string]LogDirMetric{}

			for _, topic := range logdir.Topics {
				tsize := int64(0)
				for _, part := range topic.Partitions {
					tsize += part.Size
				}

				metrics[brkid][topic.Topic] = LogDirMetric{Path: logdir.Path, Size: tsize}
			}
		}
	}

	return metrics, nil
}

func GetTopicsConsummed(client sarama.Client,
	topics map[string]sarama.TopicDetail, group string) (map[string]bool, error) {

	ctopics := make(map[string]bool)
	request := &sarama.OffsetFetchRequest{Version: 4, ConsumerGroup: group}

	for topic := range topics {
		parts, err := client.Partitions(topic)
		if err != nil {
			return nil, &groupError{"GetTopicsConsummed() get-partitions", group, err}
		}

		for _, part := range parts {
			request.AddPartition(topic, part)
		}
	}

	coordinator, err := client.Coordinator(group)
	if err != nil {
		return nil, &groupError{"GetTopicConsummed() get-coordinator", group, err}
	}

	if ok, _ := coordinator.Connected(); !ok {
		err = coordinator.Open(client.Config())
		if err != nil {
			return nil, &groupError{"GetTopicConsummed() open-coordinator", group, err}
		}
	}

	fr, err := coordinator.FetchOffset(request)
	if err != nil {
		return nil, &groupError{"GetTopicConsummed() fetch-offset", group, err}
	}

	for t, parts := range fr.Blocks {
		for _, block := range parts {
			if block.Offset != -1 {
				ctopics[t] = true
			}
		}
	}

	return ctopics, nil
}

func GetTopicMetrics(client sarama.Client, topic string, timestamp bool) (map[int32]TopicMetric, error) {
	metrics := make(map[int32]TopicMetric)

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, &topicError{"GetTopicMetrics() get-partitions", topic, err}
	}

	for _, part := range parts {
		replicas, err := client.Replicas(topic, part)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-replicas", topic, part, err}
		}

		isr, err := client.InSyncReplicas(topic, part)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-insync-replicas", topic, part, err}
		}

		underRep := 0
		if len(isr) < len(replicas) {
			underRep = 1
		}

		leader, err := client.Leader(topic, part)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-leader", topic, part, err}
		}

		if ok, _ := leader.Connected(); !ok {
			err = leader.Open(client.Config())
			if err != nil && err.Error() != "kafka: broker connection already initiated" {
				return nil, &topicPartError{"GetTopicMetrics() open-leader", topic, part, err}
			}
		}

		leaderNP := 1
		if len(replicas) != 0 && replicas[0] == leader.ID() {
			leaderNP = 0
		}

		var oldest, msgnumber int64 = 0, 0

		newest, err := client.GetOffset(topic, part, sarama.OffsetNewest)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-topic-newest-offset", topic, part, err}
		}

		/* newest(high watermark) is the next offset on the partition, if at 0 the partition had never be filled,
		so there are never been data, and no oldest offest */
		if newest > 0 {
			oldest, err = client.GetOffset(topic, part, sarama.OffsetOldest)
			if err != nil {
				return nil, &topicPartError{"GetTopicMetrics() get-topic-oldest-offset", topic, part, err}
			}
		}

		/* oldest(low watermark) is the last offset availabble on the partition, so the number of message is the
		difference between newest and oldest if any */
		if newest > 0 && oldest >= 0 {
			msgnumber = newest - oldest
		}

		var (
			newestTime, oldestTime time.Time
			oldestInterval         time.Duration
		)

		if timestamp {
			/* if there are no message, we cannot get timestamps */
			if msgnumber > 0 {
				// newest is the next offset so getting the previous one
				newestTime, err = GetTimestamp(leader, topic, part, newest-1)
				if err != nil {
					return nil, &topicPartError{"GetTopicMetrics() get-timestamp-topic-offset-newest", topic, part, err}
				}

				oldestTime, err = GetTimestamp(leader, topic, part, oldest)
				if err != nil {
					return nil, &topicPartError{"GetTopicMetrics() get-timestamp-topic-offset-oldest", topic, part, err}
				}

				oldestInterval = time.Since(oldestTime)
			}
		}

		metrics[part] = TopicMetric{
			Replicas:        len(replicas),
			InSyncReplicas:  len(isr),
			Leader:          leader.ID(),
			LeaderNP:        leaderNP,
			UnderReplicated: underRep,
			Newest:          newest,
			Oldest:          oldest,
			MsgNumber:       msgnumber,
			NewestTime:      newestTime,
			OldestTime:      oldestInterval,
		}
	}

	return metrics, nil
}

func GetGroupMetrics(client sarama.Client, topic string, group string,
	topicMetrics map[int32]TopicMetric, timestamp bool) (map[int32]GroupMetric, error) {

	metrics := make(map[int32]GroupMetric)

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, &topicGroupError{"GetGroupMetrics() get-partitions", topic, group, err}
	}

	for _, part := range parts {
		oldest := topicMetrics[part].Oldest
		msgnumber := topicMetrics[part].MsgNumber
		newestTime := topicMetrics[part].NewestTime

		leader, err := client.Leader(topic, part)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-leader", topic, group, part, err}
		}

		if ok, _ := leader.Connected(); !ok {
			err = leader.Open(client.Config())
			if err != nil {
				return nil, &topicGroupPartError{"GetGroupMetrics() open-leader", topic, group, part, err}
			}
		}

		var (
			olag int64 = 0
			tlag time.Duration
		)

		/* for group offset we query the coordinator of the group */
		coordinator, err := client.Coordinator(group)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-group-coordinator", topic, group, part, err}
		}

		if ok, _ := coordinator.Connected(); !ok {
			err = coordinator.Open(client.Config())
			if err != nil {
				return nil, &topicGroupPartError{"GetGroupMetrics() open-group-coordinator", topic, group, part, err}
			}
		}

		current, err := GetGroupOffset(coordinator, topic, part, group)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-group-current-offset", topic, group, part, err}
		}

		/* reget newest offset to be more accurate */
		newest, err := client.GetOffset(topic, part, sarama.OffsetNewest)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-topic-newest-offset", topic, group, part, err}
		}

		/* offset lag is only defined if there are message */
		if newest >= 0 && current >= 0 {
			olag = newest - current
		}

		/* since we are not synchronous lag can be negative if consummer is fast enough */
		if olag < 0 {
			olag = 0
		}

		nullTime := time.Time{}

		if timestamp {
			/* we need at least two messages available */
			if msgnumber > 1 && olag > 0 && current > oldest {
				currentTime, err := GetTimestamp(leader, topic, part, current-1)
				if err != nil {
					return nil, &topicGroupPartError{"GetGroupMetrics() get-timestamp-group-offset", topic, group, part, err}
				}

				if currentTime != nullTime && newestTime != nullTime && newestTime.After(currentTime) {
					tlag = newestTime.Sub(currentTime)
				} else {
					tlag = 0
				}
			}
		}

		metrics[part] = GroupMetric{
			Current:   current,
			OffsetLag: olag,
			TimeLag:   tlag,
		}
	}

	return metrics, nil
}
