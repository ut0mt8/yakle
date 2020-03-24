# YAKLE (Yet Another Kafka Lag Exporter)

Kafka lag exporter are either broken, slow or only send metrics to influx. Why not writing my own ? 

This is inspired by burrowx, but simplified with the more simple and robust logic I could think of.
Yakle is fast cause it use only low level api. The big feature compared to other exporter is that yakle reports not only offset lag but also the time lag, and not interpolated one.

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
| `yakle_topic_partition_leader{topic, partition}` | Leader Broker ID of a given topic/partition. |
| `yakle_topic_partition_replicas{topic, partition}` | Number of replicas of a given topic/partition. |
| `yakle_topic_partition_isr{topic, partition}` | Number of in-sync replicas of a given topic/partition. |
| `yakle_topic_partition_newest_offset{topic, partition}` | Latest commited offset of a given topic/partition. |
| `yakle_topic_partition_oldest_offset{topic, partition}` | Oldest offset available of a given topic/partition. |


#### Consumer group metrics

| Metric | Description |
| --- | --- |
| `yakle_group_topic_partition_current_offset{group, topic, partition}` | Current offset of a given group/topic/partition. |
| `yakle_group_topic_partition_offset_lag{group, topic, partition}` | Offset lag of a given group/topic/partition. |
| `yakle_group_topic_partition_time_lag{group, topic, partition}` | Time lag (in ms) of a given group/topic/partition. |





