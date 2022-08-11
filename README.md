# YAKLE (Yet Another Kafka Lag Exporter)

Kafka lag exporter are either broken, slow or only send metrics to influx.
This is my attempt to write my own. This is inspired by burrowx, but simplified with the more simple and robust logic I could think of.
Yakle basically export the same sets of metrics as danielqsj/kafka_exporter but the logic behind is very different. 

Yakle is fast cause it use only low level kafka api. The main feature compared to other exporters is that yakle reports not only offset lag but also time lag (real time lag, not interpolated).
Yakle is "production" tested and worked since months in our environment (dozen of kafka clusters, hundred of brokers/topics with many partitions)

## Usage

```
Usage of ./yakle:
  -brokers="localhost:9092": brokers to connect on
  -debug=false: enable debug logging
  -filter="^__.*": regex for filtering topics
  -interval=10: interval of lag refresh
  -listen-address=":8080": host:port to listen on
  -metric-path="/metrics": path exposing metrics
```

Flags can be passed as environment variables. 

Docker image exist at dockerhub `ut0mt8/yakle:latest`

## Exposed metrics

### Labels

**`topic`**: Topic name

**`partition`**: Partition ID 

**`group`**: Consumer group name


### Metrics

#### Topic / Partition metrics

| Metric | Description |
| --- | --- |
| `kafka_topic_partition_leader{cluster, topic, partition}` | Leader Broker ID for a given topic/partition |
| `kafka_topic_partition_leader_is_preferred{cluster, topic, partition}` | Boolean indicating if the leader use its preferred broker for a given topic/partition |
| `kafka_topic_partition_replicas{cluster, topic, partition}` | Number of replicas for a given topic/partition |
| `kafka_topic_partition_isr{cluster, topic, partition}` | Number of in-sync replicas for a given topic/partition |
| `kafka_topic_partition_under_replicated{cluster, topic, partition}` | Boolean indicating if all replicas are in sync for a given topic/partition |
| `kafka_topic_partition_newest_offset{cluster, topic, partition}` | Latest commited offset for a given topic/partition |
| `kafka_topic_partition_oldest_offset{cluster, topic, partition}` | Oldest offset available for a given topic/partition |
| `kafka_topic_partition_oldest_time{cluster, topic, partition}` | Timestamp in ms of the oldest offset available for a given topic/partition |


#### Consumer group metrics

| Metric | Description |
| --- | --- |
| `kafka_group_topic_partition_current_offset{cluster, group, topic, partition}` | Current offset for a given group/topic/partition |
| `kafka_group_topic_partition_offset_lag{cluster, group, topic, partition}` | Offset lag for a given group/topic/partition |
| `kafka_group_topic_partition_time_lag{cluster, group, topic, partition}` | Time lag (in ms) for a given group/topic/partition |



## Todo

 - Add unit tests (but mocking kafka brokers is not easy)

