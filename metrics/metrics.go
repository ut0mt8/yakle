package metrics

import (
	"fmt"
	"github.com/Shopify/sarama"
	"strings"
	"time"
)

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

type topicGroupPartError struct {
	operation string
	topic     string
	group     string
	partition int32
	err       error
}

func (e *topicError) Error() string {
	return fmt.Sprintf("[%s](topic: %s): %v", e.operation, e.topic, e.err)
}

func (e *topicPartError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, partition: %d): %v", e.operation, e.topic, e.partition, e.err)
}

func (e *topicGroupError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, group: %s): %v", e.operation, e.topic, e.group, e.err)
}

func (e *topicGroupPartError) Error() string {
	return fmt.Sprintf("[%s](topic: %s, group: %s, partition: %d): %v", e.operation, e.topic, e.group, e.partition, e.err)
}

type TopicMetrics struct {
	Leader         int32
	Replicas       int
	InSyncReplicas int
	Newest         int64
	Oldest         int64
	MsgNumber      int64
	NewestTime     time.Time
	OldestTime     time.Duration
}

type GroupMetrics struct {
	Current   int64
	OffsetLag int64
	TimeLag   time.Duration
}

var nullTime time.Time = time.Time{}

func GetGroupOffset(broker *sarama.Broker, topic string, partition int32, group string) (int64, error) {
	request := &sarama.OffsetFetchRequest{Version: 4, ConsumerGroup: group}
	request.AddPartition(topic, partition)

	fr, err := broker.FetchOffset(request)
	if err != nil {
		return 0, fmt.Errorf("cannot fetch offset request: %v", err)
	}

	block := fr.GetBlock(topic, partition)
	if block == nil {
		return 0, fmt.Errorf("cannot get block")
	}

	return block.Offset, nil
}

func GetTimestamp(broker *sarama.Broker, topic string, partition int32, offset int64) (time.Time, error) {
	request := &sarama.FetchRequest{Version: 4}
	request.AddBlock(topic, partition, offset, 1)

	fr, err := broker.Fetch(request)
	if err != nil {
		return nullTime, fmt.Errorf("cannot fetch request: %v", err)
	}

	block := fr.GetBlock(topic, partition)
	if block == nil || block.Records == nil {
		return nullTime, fmt.Errorf("cannot get block")
	}

	return block.Records.RecordBatch.MaxTimestamp, nil
}

func GetTopics(brokers string) (map[string]sarama.TopicDetail, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0
	bks := strings.Split(brokers, ",")

	cadmin, err := sarama.NewClusterAdmin(bks, cfg)
	if err != nil {
		return nil, fmt.Errorf("admin-connect")
	}
	defer cadmin.Close()

	topics, err := cadmin.ListTopics()
	if err != nil {
		return nil, fmt.Errorf("get-topics")
	}

	return topics, nil
}

func GetGroups(brokers string) (map[string]string, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0
	bks := strings.Split(brokers, ",")

	cadmin, err := sarama.NewClusterAdmin(bks, cfg)
	if err != nil {
		return nil, fmt.Errorf("admin-connect")
	}
	defer cadmin.Close()

	groups, err := cadmin.ListConsumerGroups()
	if err != nil {
		return nil, fmt.Errorf("get-groups")
	}

	return groups, nil
}

func GetTopicConsummed(brokers string, topic string, group string) (bool, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0
	bks := strings.Split(brokers, ",")

	client, err := sarama.NewClient(bks, cfg)
	if err != nil {
		return false, &topicGroupError{"GetTopicConsummed() client-connect", topic, group, err}
	}
	defer client.Close()

	parts, err := client.Partitions(topic)
	if err != nil {
		return false, &topicGroupError{"GetTopicConsummed() list-partitions", topic, group, err}
	}

	request := &sarama.OffsetFetchRequest{Version: 4, ConsumerGroup: group}
	for _, part := range parts {
		request.AddPartition(topic, part)
	}

	coordinator, err := client.Coordinator(group)
	if err != nil {
		return false, &topicGroupError{"GetTopicConsummed() get-coordinator", topic, group, err}
	}
	if ok, _ := coordinator.Connected(); !ok {
		coordinator.Open(client.Config())
	}

	fr, err := coordinator.FetchOffset(request)
	if err != nil {
		return false, &topicGroupError{"GetTopicConsummed() fetch-offset", topic, group, err}
	}

	for _, parts := range fr.Blocks {
		for _, block := range parts {
			if block.Offset != -1 {
				return true, nil
			}
		}
	}

	return false, nil
}

func GetTopicMetrics(brokers string, topic string) (map[int32]TopicMetrics, error) {
	metrics := make(map[int32]TopicMetrics)
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0

	bks := strings.Split(brokers, ",")

	client, err := sarama.NewClient(bks, cfg)
	if err != nil {
		return nil, &topicError{"GetTopicMetrics() client-connect", topic, err}
	}
	defer client.Close()

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, &topicError{"GetTopicMetrics() list-partitions", topic, err}
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

		leader, err := client.Leader(topic, part)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-leader", topic, part, err}
		}
		if ok, _ := leader.Connected(); !ok {
			leader.Open(client.Config())
		}

		var newest, oldest, msgnumber int64 = -1, -1, 0

		newest, err = client.GetOffset(topic, part, sarama.OffsetNewest)
		if err != nil {
			return nil, &topicPartError{"GetTopicMetrics() get-topic-newest-offset", topic, part, err}
		}

		/* newest is the next offset on the partition, if at 0 the partition had never be filled,
		so there are never been data, and no oldest offest */
		if newest > 0 {
			oldest, err = client.GetOffset(topic, part, sarama.OffsetOldest)
			if err != nil {
				return nil, &topicPartError{"GetTopicMetrics() get-topic-oldest-offset", topic, part, err}
			}
		}

		/* oldest is the last offset availabble on the partition, so the number of message is the
		difference between newest and oldest if any */
		if newest > 0 && oldest >= 0 {
			msgnumber = newest - oldest
		}

		var newestTime, oldestTime time.Time
		var oldestInterval time.Duration

		/* if there are no message, we cannot get timestamps */
		if msgnumber > 0 {
			/* newest is the next offset so getting the previous one */
			newestTime, err = GetTimestamp(leader, topic, part, newest-1)
			if err != nil {
				return nil, &topicPartError{"GetTopicMetrics() get-timestamp-topic-offset-latest", topic, part, err}
			}

			oldestTime, err = GetTimestamp(leader, topic, part, oldest)
			if err != nil {
				return nil, &topicPartError{"GetTopicMetrics() get-timestamp-topic-offset-oldest", topic, part, err}
			}
			oldestInterval = time.Now().Sub(oldestTime)
		}

		metrics[part] = TopicMetrics{
			Leader:         leader.ID(),
			Replicas:       len(replicas),
			InSyncReplicas: len(isr),
			Newest:         newest,
			Oldest:         oldest,
			MsgNumber:      msgnumber,
			NewestTime:     newestTime,
			OldestTime:     oldestInterval,
		}
	}
	return metrics, nil
}

func GetGroupMetrics(brokers string, topic string, group string, tm map[int32]TopicMetrics) (map[int32]GroupMetrics, error) {
	metrics := make(map[int32]GroupMetrics)
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_1_0_0

	bks := strings.Split(brokers, ",")

	client, err := sarama.NewClient(bks, cfg)
	if err != nil {
		return nil, &topicGroupError{"GetGroupMetrics() client-connect", topic, group, err}
	}
	defer client.Close()

	parts, err := client.Partitions(topic)
	if err != nil {
		return nil, &topicGroupError{"GetGroupMetrics() list-partitions", topic, group, err}
	}

	for _, part := range parts {
		newest := tm[part].Newest
		oldest := tm[part].Oldest
		msgnumber := tm[part].MsgNumber
		newestTime := tm[part].NewestTime

		leader, err := client.Leader(topic, part)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-leader", topic, group, part, err}
		}
		if ok, _ := leader.Connected(); !ok {
			leader.Open(client.Config())
		}

		var current, olag int64 = -1, 0
		var tlag time.Duration

		/* for group offset we query the coordinator of the group */
		coordinator, err := client.Coordinator(group)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-group-coordinator", topic, group, part, err}
		}
		if ok, _ := coordinator.Connected(); !ok {
			coordinator.Open(client.Config())
		}

		current, err = GetGroupOffset(coordinator, topic, part, group)
		if err != nil {
			return nil, &topicGroupPartError{"GetGroupMetrics() get-group-offset", topic, group, part, err}
		}

		/* offset lag is only defined if there are message */
		if newest >= 0 && current >= 0 {
			olag = newest - current
		}

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

		metrics[part] = GroupMetrics{
			Current:   current,
			OffsetLag: olag,
			TimeLag:   tlag,
		}
	}
	return metrics, nil
}
